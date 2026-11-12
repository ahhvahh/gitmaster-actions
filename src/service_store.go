package gitmasteractions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RuntimeEvent is a durable lifecycle event belonging to an Action.
type RuntimeEvent struct {
	ID         int64  `json:"id"`
	ActionID   string `json:"action_id"`
	Stage      string `json:"stage"`
	Message    string `json:"message"`
	RecordedAt string `json:"recorded_at"`
}

type ActionDetails struct {
	Action         *Action        `json:"action"`
	Origin         ActionOrigin   `json:"origin"`
	Format         YAMLFormat     `json:"format"`
	VMProfile      string         `json:"vm_profile"`
	MaterialSHA256 string         `json:"material_sha256,omitempty"`
	VMID           string         `json:"vm_id,omitempty"`
	CleanupStatus  string         `json:"cleanup_status,omitempty"`
	CleanupError   string         `json:"cleanup_error,omitempty"`
	Jobs           []JobDetails   `json:"jobs"`
	Events         []RuntimeEvent `json:"runtime_events"`
}
type JobDetails struct {
	Job        *Job             `json:"job"`
	Needs      []string         `json:"needs"`
	SkipReason string           `json:"skip_reason,omitempty"`
	Commands   []CommandDetails `json:"commands"`
}
type CommandDetails struct {
	Command          *Command `json:"command"`
	Name             string   `json:"name"`
	WorkingDirectory string   `json:"working_directory"`
	FailureKind      string   `json:"failure_kind,omitempty"`
	ErrorMessage     string   `json:"error_message,omitempty"`
}

// Additive schema migration preserves pre-existing action, job and output rows.
func ensureServiceSchema(db *sql.DB) error {
	columns := map[string]map[string]string{
		"actions": {
			"clone_id": "TEXT", "commit_sha": "TEXT", "event_id": "TEXT", "event_type": "TEXT", "branch": "TEXT", "workflow_name": "TEXT", "workflow_path": "TEXT", "yaml_format": "TEXT", "vm_profile": "TEXT", "material_sha256": "TEXT", "material_path": "TEXT", "yaml_sha256": "TEXT", "vm_id": "TEXT", "cleanup_status": "TEXT", "cleanup_error": "TEXT",
		},
		"jobs":     {"needs_json": "TEXT NOT NULL DEFAULT '[]'", "skip_reason": "TEXT NOT NULL DEFAULT ''", "timeout_seconds": "INTEGER NOT NULL DEFAULT 0"},
		"commands": {"step_name": "TEXT NOT NULL DEFAULT ''", "working_directory": "TEXT NOT NULL DEFAULT ''"},
	}
	for table, cols := range columns {
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			return err
		}
		present := map[string]bool{}
		for rows.Next() {
			var n, required, pk int
			var name, typ string
			var defaultVal sql.NullString
			if err = rows.Scan(&n, &name, &typ, &required, &defaultVal, &pk); err != nil {
				break
			}
			present[name] = true
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		for name, kind := range cols {
			if present[name] {
				continue
			}
			// Column names are static literals from the map above.
			if _, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + name + " " + kind); err != nil {
				return fmt.Errorf("migrate %s.%s: %w", table, name, err)
			}
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS runtime_events(id INTEGER PRIMARY KEY AUTOINCREMENT,action_id TEXT NOT NULL REFERENCES actions(id),stage TEXT NOT NULL,message TEXT NOT NULL,recorded_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_runtime_events_action ON runtime_events(action_id,id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_actions_origin_event ON actions(clone_id,commit_sha,workflow_path,event_type) WHERE clone_id IS NOT NULL AND clone_id<>'' AND commit_sha IS NOT NULL AND workflow_path IS NOT NULL AND event_type IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_actions_clone ON actions(clone_id,created_at,id)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// findIdenticalSubmission returns a previous run only when its immutable
// identity, YAML and snapshot match. Legacy rows without hashes are not reusable.
func (e *Engine) findIdenticalSubmission(ctx context.Context, origin ActionOrigin, yamlHash, materialHash string, format YAMLFormat) (*Action, error) {
    var id, oldYAML, oldMaterial, oldEvent, oldBranch, oldFormat string
    err := e.db.QueryRowContext(ctx, `SELECT id,COALESCE(yaml_sha256,''),COALESCE(material_sha256,''),
        COALESCE(event_id,''),COALESCE(branch,''),COALESCE(yaml_format,'')
        FROM actions WHERE clone_id=? AND commit_sha=? AND workflow_path=? AND event_type=?`,
        origin.CloneID, origin.CommitSHA, origin.WorkflowPath, origin.EventType).
        Scan(&id,&oldYAML,&oldMaterial,&oldEvent,&oldBranch,&oldFormat)
    if errors.Is(err, sql.ErrNoRows) { return nil,nil }
    if err != nil { return nil,err }
    if oldYAML == "" || oldYAML != yamlHash || oldMaterial != materialHash ||
       oldEvent != origin.EventID || oldBranch != origin.Branch || oldFormat != string(format) {
        return nil,fmt.Errorf("CONFLICT: existing Action %s has different YAML, material or event metadata",id)
    }
    return e.GetAction(ctx,id)
}

func (e *Engine) addEvent(ctx context.Context, id, stage, message string) error {
	if stage == "" {
		return errors.New("runtime event requires stage")
	}
	_, err := e.db.ExecContext(ctx, `INSERT INTO runtime_events(action_id,stage,message,recorded_at) VALUES(?,?,?,?)`, id, stage, redactActionText(message), timestamp())
	return err
}
func (e *Engine) setVM(ctx context.Context, id, vmID string) error {
	_, err := e.db.ExecContext(ctx, `UPDATE actions SET vm_id=?,cleanup_status='pending' WHERE id=?`, vmID, id)
	return err
}
func (e *Engine) setCleanup(ctx context.Context, id, status, message string) error {
	_, err := e.db.ExecContext(ctx, `UPDATE actions SET cleanup_status=?,cleanup_error=? WHERE id=?`, status, message, id)
	return err
}
func (e *Engine) finishQueued(ctx context.Context, id, status, message string) error {
	if status != "succeeded" && status != "failed" && status != "cancelled" {
		return errors.New("invalid terminal action status")
	}
	_, err := e.db.ExecContext(ctx, `UPDATE actions SET status=?,finished_at=?,error_message=? WHERE id=?`, status, timestamp(), message, id)
	return err
}
func (e *Engine) finishUnstarted(ctx context.Context, actionID, status string) error {
    if status != "skipped" && status != "cancelled" && status != "failed" {
		return errors.New("invalid unstarted status")
	}
	_, err := e.db.ExecContext(ctx, `UPDATE jobs SET status=?,finished_at=? WHERE action_id=? AND status IN ('pending','running')`, status, timestamp(), actionID)
	if err != nil {
		return err
	}
	_, err = e.db.ExecContext(ctx, `UPDATE commands SET status=?,finished_at=? WHERE job_id IN (SELECT id FROM jobs WHERE action_id=?) AND status IN ('pending','running')`, status, timestamp(), actionID)
	return err
}
func (e *Engine) skipCommands(ctx context.Context, jobID, status string) error {
	_, err := e.db.ExecContext(ctx, `UPDATE commands SET status=?,finished_at=? WHERE job_id=? AND status='pending'`, status, timestamp(), jobID)
	return err
}
func normalizePage(p Page) (int, error) {
	n := p.Limit
	if n == 0 {
		n = 100
	}
	if n < 1 || n > 500 || p.AfterID < 0 {
		return 0, errors.New("invalid pagination: limit 1..500, cursor >= 0")
	}
	return n, nil
}
func (e *Engine) ListRuntimeEvents(ctx context.Context, actionID string, page Page) ([]RuntimeEvent, error) {
	if err := e.assertOpen(); err != nil {
		return nil, err
	}
	limit, err := normalizePage(page)
	if err != nil {
		return nil, err
	}
	rows, err := e.db.QueryContext(ctx, `SELECT id,action_id,stage,message,recorded_at FROM runtime_events WHERE action_id=? AND id>? ORDER BY id LIMIT ?`, actionID, page.AfterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuntimeEvent{}
	for rows.Next() {
		var r RuntimeEvent
		if err = rows.Scan(&r.ID, &r.ActionID, &r.Stage, &r.Message, &r.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (e *Engine) ListJobs(ctx context.Context, actionID string) ([]*Job, error) {
	return e.jobs(ctx, actionID)
}
func (e *Engine) ListCommands(ctx context.Context, jobID string) ([]*Command, error) {
	return e.commands(ctx, jobID)
}
func (e *Engine) ListCommandLogs(ctx context.Context, commandID string, cursor uint64) ([]CommandLog, error) {
	c, err := e.GetCommand(ctx, commandID)
	if err != nil {
		return nil, err
	}
	return c.Logs(ctx, cursor)
}
func (e *Engine) ListCommandOutputs(ctx context.Context, commandID string, filter OutputFilter, page Page) ([]CommandOutput, error) {
	c, err := e.GetCommand(ctx, commandID)
	if err != nil {
		return nil, err
	}
	return c.Outputs(ctx, filter, page)
}
func decodeJobNeeds(raw string, out *[]string) error { return json.Unmarshal([]byte(raw), out) }
func (e *Engine) GetActionDetails(ctx context.Context, id string) (ActionDetails, error) {
	a, err := e.GetAction(ctx, id)
	if err != nil {
		return ActionDetails{}, err
	}
	d := ActionDetails{Action: a, Jobs: []JobDetails{}, Events: []RuntimeEvent{}}
	err = e.db.QueryRowContext(ctx, `SELECT COALESCE(clone_id,''),COALESCE(commit_sha,''),COALESCE(event_id,''),COALESCE(event_type,''),COALESCE(branch,''),COALESCE(workflow_name,''),COALESCE(workflow_path,''),COALESCE(yaml_format,''),COALESCE(vm_profile,''),COALESCE(material_sha256,''),COALESCE(vm_id,''),COALESCE(cleanup_status,''),COALESCE(cleanup_error,'') FROM actions WHERE id=?`, id).Scan(
		&d.Origin.CloneID, &d.Origin.CommitSHA, &d.Origin.EventID, &d.Origin.EventType, &d.Origin.Branch, &d.Origin.WorkflowName, &d.Origin.WorkflowPath, &d.Format, &d.VMProfile, &d.MaterialSHA256, &d.VMID, &d.CleanupStatus, &d.CleanupError)
	if err != nil {
		return ActionDetails{}, err
	}
	jobs, err := e.ListJobs(ctx, id)
	if err != nil {
		return ActionDetails{}, err
	}
	for _, job := range jobs {
		jd := JobDetails{Job: job, Commands: []CommandDetails{}}
		var needs string
		if err = e.db.QueryRowContext(ctx, `SELECT needs_json,skip_reason FROM jobs WHERE id=?`, job.ID).Scan(&needs, &jd.SkipReason); err != nil {
			return ActionDetails{}, err
		}
		if err = decodeJobNeeds(needs, &jd.Needs); err != nil {
			return ActionDetails{}, err
		}
		commands, err := e.ListCommands(ctx, job.ID)
		if err != nil {
			return ActionDetails{}, err
		}
		for _, c := range commands {
			cd := CommandDetails{Command: c}
			if err = e.db.QueryRowContext(ctx, `SELECT step_name,working_directory,COALESCE(failure_kind,''),COALESCE(error_message,'') FROM commands WHERE id=?`, c.ID).Scan(&cd.Name, &cd.WorkingDirectory, &cd.FailureKind, &cd.ErrorMessage); err != nil {
				return ActionDetails{}, err
			}
			jd.Commands = append(jd.Commands, cd)
		}
		d.Jobs = append(d.Jobs, jd)
	}
	next := int64(0)
	for {
		events, err := e.ListRuntimeEvents(ctx, id, Page{Limit: 500, AfterID: next})
		if err != nil {
			return ActionDetails{}, err
		}
		d.Events = append(d.Events, events...)
		if len(events) < 500 {
			break
		}
		next = events[len(events)-1].ID
	}
	return d, nil
}
func (e *Engine) ListActionDetails(ctx context.Context, filter ActionFilter, page Page) ([]ActionDetails, error) {
	actions, err := e.ListActions(ctx, filter, page)
	if err != nil {
		return nil, err
	}
	out := make([]ActionDetails, 0, len(actions))
	for _, a := range actions {
		d, err := e.GetActionDetails(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// On startup, prior nonterminal actions are never falsely reported as successful.
// VM cleanup remains pending until ReconcileInterrupted is explicitly run.
func (e *Engine) recoverInterrupted(ctx context.Context) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,COALESCE(vm_id,'') FROM actions WHERE status IN ('pending','starting','running')`)
	if err != nil {
		return err
	}
	type item struct{ id, vmID string }
	items := []item{}
	for rows.Next() {
		var x item
		if err = rows.Scan(&x.id, &x.vmID); err != nil {
			break
		}
		items = append(items, x)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, a := range items {
		cleanup := "not_required"
		if a.vmID != "" {
			cleanup = "pending"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE actions SET status='failed',finished_at=?,error_message='interrupted by process restart',cleanup_status=? WHERE id=?`, timestamp(), cleanup, a.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status='cancelled',finished_at=? WHERE action_id=? AND status IN ('pending','running')`, timestamp(), a.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE commands SET status='cancelled',finished_at=?,failure_kind='infrastructure' WHERE job_id IN (SELECT id FROM jobs WHERE action_id=?) AND status IN ('pending','running')`, timestamp(), a.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO runtime_events(action_id,stage,message,recorded_at) VALUES(?,'interrupted','process stopped before action completion',?)`, a.id, timestamp()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (e *Engine) ReconcileInterrupted(ctx context.Context) error {
	if err := e.assertOpen(); err != nil {
		return err
	}
	rows, err := e.db.QueryContext(ctx, `SELECT id,vm_id FROM actions WHERE cleanup_status IN ('pending','failed') AND vm_id IS NOT NULL AND status IN ('succeeded','failed','cancelled')`)
	if err != nil {
		return err
	}
	type item struct{ id, vmID string }
	items := []item{}
	for rows.Next() {
		var x item
		if err = rows.Scan(&x.id, &x.vmID); err != nil {
			break
		}
		items = append(items, x)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	var all error
	for _, x := range items {
		rt, err := e.runtimeForActions()
		if err == nil {
			err = rt.Stop(ctx, x.vmID)
		}
		state := "succeeded"
		message := ""
		if err != nil {
			state = "failed"
			message = err.Error()
			all = errors.Join(all, fmt.Errorf("cleanup %s: %w", x.id, err))
		}
		persistCtx,done:=context.WithTimeout(context.Background(),10*time.Second)
        saveErr:=errors.Join(e.setCleanup(persistCtx,x.id,state,message),
            e.addEvent(persistCtx,x.id,"recovery_cleanup",state+": "+message))
        done()
        if saveErr!=nil{all=errors.Join(all,saveErr)}
	}
	return all
}
func (e *Engine) ListVMProfiles(ctx context.Context) ([]VMProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.assertOpen(); err != nil {
		return nil, err
	}
	return ListVMProfiles(e.options.InsulaConfigPath)
}
func (e *Engine) GetVMProfile(ctx context.Context, name string) (VMProfile, error) {
	profiles, err := e.ListVMProfiles(ctx)
	if err != nil {
		return VMProfile{}, err
	}
	for _, p := range profiles {
		if p.Name == name {
			return p, nil
		}
	}
	return VMProfile{}, sql.ErrNoRows
}
func (e *Engine) RegisterVMProfile(ctx context.Context, request ProfileRequest) (VMProfile, error) {
	if err := ctx.Err(); err != nil {
		return VMProfile{}, err
	}
	if err := e.assertOpen(); err != nil {
		return VMProfile{}, err
	}
	return RegisterVMProfile(e.options.InsulaConfigPath, e.options.ImagesDir, request)
}
func (e *Engine) ListVMImages(ctx context.Context) ([]VMImage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.assertOpen(); err != nil {
		return nil, err
	}
	return ListVMImages(e.options.ImagesDir)
}

// WaitAction waits for terminal persistence, including when called by another process.
func (e *Engine) WaitAction(ctx context.Context, id string) (*Action, error) {
	ticker := time.NewTicker(75 * time.Millisecond)
	defer ticker.Stop()
	for {
		action, err := e.GetAction(ctx, id)
		if err != nil {
			return nil, err
		}
		if action.Status == "succeeded" || action.Status == "failed" || action.Status == "cancelled" {
			return action, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// The engine shares the existing Close and CancelAction methods with RunYAML.
func (e *Engine) runtimeForActions() (Runtime, error) {
	if e.options.RuntimeFactory != nil {
		return e.options.RuntimeFactory()
	}
	if strings.TrimSpace(e.options.InsulaConfigPath) == "" {
		return nil, errors.New("InsulaConfigPath is required")
	}
	return NewRuntime(e.options.InsulaConfigPath, e.options.StartupScripts)
}