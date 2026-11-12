package gitmasteractions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Legacy records are copied verbatim to legacy_archive before normalization.
// New UUIDv7 identities prevent collisions between historical and new Actions.
func legacyText(row map[string]any, key string) string {
	v := row[key]
	if v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}
func legacyNumber(row map[string]any, key string) int64 {
	n, _ := strconv.ParseInt(legacyText(row, key), 10, 64)
	return n
}
func legacyNull(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func legacyDate(row map[string]any, key string) any { return legacyNull(legacyText(row, key)) }
func legacyStamp(row map[string]any, key string) string {
	if value := legacyText(row, key); value != "" {
		return value
	}
	return timestamp()
}
func legacyStatus(old, entity string) string {
	switch old {
	case "succeeded", "failed", "cancelled":
		return old
	case "skipped":
		if entity != "actions" {
			return "skipped"
		}
	case "queued", "pending", "running", "starting", "interrupted":
		if entity != "actions" {
			return "cancelled"
		}
	}
	if entity == "actions" {
		return "failed"
	}
	return "skipped"
}
func legacyLink(ctx context.Context, tx *sql.Tx, source, table, key string, create bool) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT mapped_id FROM legacy_links WHERE source=? AND table_name=? AND source_key=?`, source, table, key).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) || !create {
		return "", err
	}
	id, err = uuidv7()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO legacy_links(source,table_name,source_key,mapped_id) VALUES(?,?,?,?)`, source, table, key, id)
	return id, err
}

// normalizeLegacyRow translates the five historical tables in dependency order.
// Unknown fields and the original representation remain available in legacy_archive.
func normalizeLegacyRow(ctx context.Context, tx *sql.Tx, source, table, key string, row map[string]any) error {
	switch table {
	case "action_runs":
		id, err := legacyLink(ctx, tx, source, table, key, true)
		if err != nil {
			return err
		}
		state := legacyStatus(legacyText(row, "status"), "actions")
		finished := legacyDate(row, "finished_at")
		reason := ""
		if state == "failed" && legacyText(row, "status") != "failed" {
			reason = "legacy Action interrupted or in unknown state at import"
			if finished == nil {
				finished = timestamp()
			}
		}
		name := legacyText(row, "workflow_path")
		if name == "" {
			name = "legacy:" + key
		}
		// Never attempt to stop a historical VM without a current ownership lease.
		_, err = tx.ExecContext(ctx, `INSERT INTO actions
   (id,name,status,created_at,started_at,finished_at,error_message,clone_id,commit_sha,event_id,branch,workflow_name,workflow_path,yaml_format,vm_profile,vm_id,cleanup_status)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
   ON CONFLICT(id) DO UPDATE SET status=excluded.status,started_at=excluded.started_at,finished_at=excluded.finished_at,error_message=excluded.error_message`,
			id, name, state, legacyStamp(row, "created_at"), legacyDate(row, "started_at"), finished, legacyNull(reason), legacyText(row, "clone_id"), legacyText(row, "commit_sha"),
			legacyText(row, "event_id"), legacyText(row, "branch"), name, legacyText(row, "workflow_path"), "legacy-import", legacyText(row, "vm_profile"), legacyNull(legacyText(row, "insula_vm_id")), "historical")
		return err
	case "action_jobs":
		aid, err := legacyLink(ctx, tx, source, "action_runs", legacyText(row, "action_run_id"), false)
		if err != nil {
			return fmt.Errorf("legacy job %s parent: %w", key, err)
		}
		id, err := legacyLink(ctx, tx, source, table, key, true)
		if err != nil {
			return err
		}
		needs := legacyText(row, "needs_json")
		if needs == "" {
			needs = "[]"
		}
		var deps []string
		if json.Unmarshal([]byte(needs), &deps) != nil {
			needs = "[]"
		}
		name := legacyText(row, "job_name")
		if name == "" {
			name = "legacy-job-" + key
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO jobs (id,action_id,name,position,allow_failure,status,needs_json,skip_reason,started_at,finished_at)
   VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,started_at=excluded.started_at,finished_at=excluded.finished_at`,
			id, aid, name, legacyNumber(row, "id"), 0, legacyStatus(legacyText(row, "status"), "jobs"), needs, legacyText(row, "skip_reason"), legacyDate(row, "started_at"), legacyDate(row, "finished_at"))
		return err
	case "action_job_commands":
		jid, err := legacyLink(ctx, tx, source, "action_jobs", legacyText(row, "job_id"), false)
		if err != nil {
			return fmt.Errorf("legacy command %s parent: %w", key, err)
		}
		id, err := legacyLink(ctx, tx, source, table, key, true)
		if err != nil {
			return err
		}
		step := legacyText(row, "step_name")
		if step == "" {
			step = "legacy-step-" + key
		}
		var exit any
		if row["exit_code"] != nil {
			exit = legacyNumber(row, "exit_code")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO commands (id,job_id,position,command_text,status,exit_code,started_at,finished_at,step_name,working_directory)
   VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,exit_code=excluded.exit_code,started_at=excluded.started_at,finished_at=excluded.finished_at`,
			id, jid, legacyNumber(row, "step_order"), legacyText(row, "command_text"), legacyStatus(legacyText(row, "status"), "commands"), exit,
			legacyDate(row, "started_at"), legacyDate(row, "finished_at"), step, legacyText(row, "working_directory"))
		return err
	case "action_command_responses":
		cid, err := legacyLink(ctx, tx, source, "action_job_commands", legacyText(row, "command_id"), false)
		if err != nil {
			return fmt.Errorf("legacy response %s parent: %w", key, err)
		}
		stream := legacyText(row, "stream")
		content := legacyText(row, "message")
		switch stream {
		case "stdout", "stderr":
		case "system":
			stream = "stdout"
			content = "[legacy:system] " + content
		case "error":
			stream = "stderr"
			content = "[legacy:error] " + content
		default:
			stream = "stdout"
			content = "[legacy:unknown] " + content
		}
		sequence := legacyNumber(row, "sequence")
		if sequence <= 0 {
			sequence = legacyNumber(row, "id")
		}
		if sequence <= 0 {
			return fmt.Errorf("legacy response %s has invalid sequence", key)
		}
		stamp := legacyStamp(row, "recorded_at")
		_, err = tx.ExecContext(ctx, `INSERT INTO command_outputs(command_id,sequence_no,stream_type,payload,captured_at) VALUES(?,?,?,?,?)
   ON CONFLICT(command_id,sequence_no) DO UPDATE SET stream_type=excluded.stream_type,payload=excluded.payload,captured_at=excluded.captured_at`,
			cid, sequence, stream, []byte(content), stamp)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO command_log_lines(command_id,line_no,stream_type,payload,captured_at) VALUES(?,?,?,?,?)
   ON CONFLICT(command_id,line_no) DO UPDATE SET stream_type=excluded.stream_type,payload=excluded.payload,captured_at=excluded.captured_at`,
			cid, sequence, stream, []byte(content), stamp)
		return err
	case "action_runtime_messages":
		aid, err := legacyLink(ctx, tx, source, "action_runs", legacyText(row, "action_run_id"), false)
		if err != nil {
			return fmt.Errorf("legacy event %s parent: %w", key, err)
		}
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT mapped_id FROM legacy_links WHERE source=? AND table_name=? AND source_key=?`, source, table, key).Scan(&existing)
		if err == nil {
			index, parseErr := strconv.ParseInt(existing, 10, 64)
			if parseErr != nil {
				return parseErr
			}
			_, err = tx.ExecContext(ctx, `UPDATE runtime_events SET stage=?,message=?,recorded_at=? WHERE id=?`, "legacy:"+legacyText(row, "stage"), legacyText(row, "message"), legacyStamp(row, "recorded_at"), index)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO runtime_events(action_id,stage,message,recorded_at) VALUES(?,?,?,?)`, aid, "legacy:"+legacyText(row, "stage"), legacyText(row, "message"), legacyStamp(row, "recorded_at"))
		if err != nil {
			return err
		}
		index, err := result.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO legacy_links(source,table_name,source_key,mapped_id) VALUES(?,?,?,?)`, source, table, key, strconv.FormatInt(index, 10))
		return err
	default:
		return fmt.Errorf("unsupported legacy table %q", table)
	}
}

// GetLegacyActionID maps an old action_run_id to the new UUIDv7 for GetActionDetails.
func (e *Engine) GetLegacyActionID(ctx context.Context, source, oldID string) (string, error) {
	if err := e.assertOpen(); err != nil {
		return "", err
	}
	if source == "" || oldID == "" {
		return "", errors.New("legacy source and action ID required")
	}
	var id string
	err := e.db.QueryRowContext(ctx, `SELECT mapped_id FROM legacy_links WHERE source=? AND table_name='action_runs' AND source_key=?`, source, oldID).Scan(&id)
	return id, err
}