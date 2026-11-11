package legacy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ActionCommandResponse stores one VM output line or lifecycle event in
// permanent, ordered history. Legacy MCP execution logs remain ephemeral.
type ActionCommandResponse struct {
	ID         int64  `json:"id"`
	CommandID  string `json:"command_id"`
	Sequence   int64  `json:"sequence"`
	Stream     string `json:"stream"`
	Message    string `json:"message"`
	RecordedAt string `json:"recorded_at"`
}

// AppendCommandResponse assigns sequence numbers transactionally. A command
// must be running: completed responses cannot be silently rewritten.
func (s *Store) AppendCommandResponse(ctx context.Context, commandID, stream, message string) error {
	if commandID == "" {
		return errors.New("INVALID_ARGUMENT: command_id required")
	}
	switch stream {
	case "stdout", "stderr", "system", "error":
	default:
		return errors.New("INVALID_ARGUMENT: response stream must be stdout/stderr/system/error")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM action_job_commands WHERE command_id=?`, commandID).Scan(&status); err != nil {
		return err
	}
	if status != "running" { return errors.New("CONFLICT: command is not running") }
	var sequence int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sequence),0)+1 FROM action_command_responses WHERE command_id=?`, commandID).
		Scan(&sequence); err != nil { return err }
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO action_command_responses(command_id,sequence,stream,message,recorded_at)
		VALUES(?,?,?,?,?)`, commandID, sequence, stream, message, now()); err != nil {
		return err
	}
	return tx.Commit()
}

// ListCommandResponses enforces the original operational socket and Linux UID
// ownership. A cursor limits the response size without dropping stored lines.
func (s *Store) ListCommandResponses(
	ctx context.Context, cloneID, commandID, scope string, ownerUID uint32,
	afterSequence int64, limit int,
) ([]ActionCommandResponse, int64, bool, error) {
	if cloneID == "" || commandID == "" || scope == "" || afterSequence < 0 {
		return nil, 0, false, errors.New("INVALID_ARGUMENT: clone_id, command_id, scope or cursor invalid")
	}
	if limit == 0 { limit = 100 }
	if limit < 1 || limit > 1000 {
		return nil, 0, false, errors.New("INVALID_ARGUMENT: limit must be 1..1000")
	}
	// The nested filter is crucial: possession of a command UUID is not an
	// authorization token, even for a valid clone_id from another user.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT r.id,r.command_id,r.sequence,r.stream,r.message,r.recorded_at
		  FROM action_command_responses r
		  JOIN action_job_commands c ON c.command_id=r.command_id
		  JOIN action_jobs j ON j.id=c.job_id
		  JOIN action_runs a ON a.action_run_id=j.action_run_id
		 WHERE a.clone_id=? AND a.scope=? AND a.owner_uid=?
		   AND r.command_id=? AND r.sequence>?
		 ORDER BY r.sequence ASC LIMIT ?`,
		cloneID, scope, int64(ownerUID), commandID, afterSequence, limit+1)
	if err != nil { return nil, 0, false, fmt.Errorf("list action responses: %w", err) }
	defer rows.Close()
	results := make([]ActionCommandResponse, 0, limit)
	more := false
	next := afterSequence
	for rows.Next() {
		var response ActionCommandResponse
		if err := rows.Scan(&response.ID, &response.CommandID, &response.Sequence,
			&response.Stream, &response.Message, &response.RecordedAt); err != nil {
			return nil, 0, false, err
		}
		if len(results) == limit { more = true; break }
		results = append(results, response)
		next = response.Sequence
	}
	if err := rows.Err(); err != nil { return nil, 0, false, err }
	return results, next, more, nil
}

// A diagnostic query for the internal worker, not exposed through MCP. Do not
// use this method in an agent-facing tool without access-control filtering.
func (s *Store) CommandResponseCount(ctx context.Context, commandID string) (int64, error) {
	var count int64
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM action_command_responses WHERE command_id=?`, commandID).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) { return 0, nil }
	return count, err
}
