package legacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

)

// Action records remain durable across daemon restarts. Console output is never
// duplicated in these tables: log references are nullable foreign keys.
func ensureActionSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS action_runs (
			action_run_id TEXT PRIMARY KEY,
			clone_id TEXT NOT NULL REFERENCES clones(id) ON DELETE CASCADE,
			workflow_path TEXT NOT NULL,
			commit_sha TEXT NOT NULL,
			branch TEXT NOT NULL,
			vm_profile TEXT NOT NULL,
			insula_vm_id TEXT,
			event_id TEXT REFERENCES action_events(event_id) ON DELETE SET NULL,
			scope TEXT NOT NULL,
			owner_uid INTEGER NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('queued','running','succeeded','failed','cancelled','interrupted')),
			created_at TEXT NOT NULL,
			started_at TEXT,
			finished_at TEXT,
			UNIQUE(event_id, workflow_path)
		)`,
		`CREATE TABLE IF NOT EXISTS action_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			action_run_id TEXT NOT NULL REFERENCES action_runs(action_run_id) ON DELETE CASCADE,
			job_name TEXT NOT NULL,
			needs_json TEXT NOT NULL DEFAULT '[]',
			skip_reason TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK(status IN ('queued','running','succeeded','failed','skipped','cancelled','interrupted')),
			started_at TEXT,
			finished_at TEXT,
			exit_code INTEGER,
			UNIQUE(action_run_id,job_name)
		)`,
		`CREATE TABLE IF NOT EXISTS action_job_commands (
			command_id TEXT PRIMARY KEY,
			job_id INTEGER NOT NULL REFERENCES action_jobs(id) ON DELETE CASCADE,
			step_order INTEGER NOT NULL,
			step_name TEXT NOT NULL,
			working_directory TEXT NOT NULL DEFAULT '',
			command_text TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('queued','running','succeeded','failed','skipped','cancelled','interrupted')),
			exit_code INTEGER,
			started_at TEXT,
			finished_at TEXT,
			log_execution_id TEXT REFERENCES tool_executions(execution_id) ON DELETE SET NULL,
			UNIQUE(job_id,step_order)
		)`,
		`CREATE TABLE IF NOT EXISTS action_command_responses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			command_id TEXT NOT NULL REFERENCES action_job_commands(command_id) ON DELETE CASCADE,
			sequence INTEGER NOT NULL CHECK(sequence > 0),
			stream TEXT NOT NULL CHECK(stream IN ('stdout','stderr','system','error')),
			message TEXT NOT NULL,
			recorded_at TEXT NOT NULL,
			UNIQUE(command_id, sequence)
		)`,
		`CREATE TABLE IF NOT EXISTS action_runtime_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			action_run_id TEXT NOT NULL REFERENCES action_runs(action_run_id) ON DELETE CASCADE,
			stage TEXT NOT NULL,
			message TEXT NOT NULL,
			recorded_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_action_runtime_messages
			ON action_runtime_messages(action_run_id,id)`,
		`CREATE INDEX IF NOT EXISTS idx_action_command_responses_order
			ON action_command_responses(command_id, sequence)`,
		`CREATE INDEX IF NOT EXISTS idx_action_runs_status ON action_runs(status,created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_action_jobs_run ON action_jobs(action_run_id,id)`,
		`CREATE INDEX IF NOT EXISTS idx_action_commands_job ON action_job_commands(job_id,step_order)`,
	}
	tx, err := db.Begin()
	if err != nil { return err }
	defer tx.Rollback()
	for _, stmt := range stmts {
		if _, err = tx.Exec(stmt); err != nil { return err }
	}
	if err := tx.Commit(); err != nil { return err }
	// Databases created before job-level branch filters require a safe additive migration.
	rows, err := db.Query("PRAGMA table_info(action_jobs)")
	if err != nil { return err }
	hasSkipReason := false
	for rows.Next() {
		var cid, required, primary int
		var name, kind string
		var def sql.NullString
		if err = rows.Scan(&cid, &name, &kind, &required, &def, &primary); err != nil { break }
		if name == "skip_reason" { hasSkipReason = true }
	}
	if err == nil { err = rows.Err() }
	rows.Close()
	if err != nil { return err }
	if !hasSkipReason {
		_, err = db.Exec(`ALTER TABLE action_jobs ADD COLUMN skip_reason TEXT NOT NULL DEFAULT ''`)
		if err != nil { return err }
	}
	return nil
}

type ActionCommandPlan struct {
	Name             string `json:"name"`
	Run              string `json:"run"`
	WorkingDirectory string `json:"working_directory,omitempty"`
}
type ActionJobPlan struct {
	SkipReason string `json:"skip_reason,omitempty"`
	Name  string              `json:"name"`
	Needs []string            `json:"needs,omitempty"`
	Steps []ActionCommandPlan `json:"steps"`
}
type ActionPlan struct {
	CloneID      string          `json:"clone_id"`
	CommitSHA    string          `json:"commit_sha"`
	WorkflowPath string          `json:"workflow_path"`
	Branch       string          `json:"branch"`
	VMProfile    string          `json:"vm_profile"`
	EventID      string          `json:"event_id,omitempty"`
	Scope        string          `json:"scope"`
	OwnerUID     uint32          `json:"owner_uid"`
	Jobs         []ActionJobPlan `json:"jobs"`
}

type ActionCommandResult struct {
	CommandID      string  `json:"command_id"`
	StepOrder      int     `json:"step_order"`
	StepName       string  `json:"step_name"`
	WorkingDir     string  `json:"working_directory"`
	Status         string  `json:"status"`
	ExitCode       *int    `json:"exit_code"`
	StartedAt      *string `json:"started_at"`
	FinishedAt     *string `json:"finished_at"`
	LogExecutionID *string `json:"log_execution_id"`
}
type ActionJobResult struct {
	SkipReason string `json:"skip_reason,omitempty"`
	ID         int64                 `json:"-"`
	Name       string                `json:"job_name"`
	Needs      []string              `json:"needs"`
	Status     string                `json:"status"`
	ExitCode   *int                  `json:"exit_code"`
	StartedAt  *string               `json:"started_at"`
	FinishedAt *string               `json:"finished_at"`
	Commands   []ActionCommandResult `json:"commands"`
}
type ActionResult struct {
	ActionRunID  string            `json:"action_run_id"`
	CloneID      string            `json:"clone_id"`
	WorkflowPath string            `json:"workflow_path"`
	CommitSHA    string            `json:"commit_sha"`
	Branch       string            `json:"branch"`
	VMProfile    string            `json:"vm_profile"`
	InsulaVMID   *string           `json:"insula_vm_id,omitempty"`
	EventID      *string           `json:"event_id,omitempty"`
	EventType    *string           `json:"event_type,omitempty"`
	Scope        string            `json:"-"`
	OwnerUID     uint32            `json:"-"`
	Status       string            `json:"status"`
	CreatedAt    string            `json:"created_at"`
	StartedAt    *string           `json:"started_at"`
	FinishedAt   *string           `json:"finished_at"`
	Jobs         []ActionJobResult `json:"jobs"`
	RuntimeEvents []ActionRuntimeMessage `json:"runtime_events"`
}

// EnqueueAction persists the full workflow snapshot before any guest command
// can run. Repeated enqueue for the same clone/SHA/path is idempotent.
func (s *Store) EnqueueAction(ctx context.Context, plan ActionPlan) (string, error) {
	if plan.CloneID == "" || plan.CommitSHA == "" || plan.Branch == "" ||
		!strings.HasPrefix(plan.WorkflowPath, ".gitmaster/workflow/") ||
		plan.VMProfile == "" || plan.Scope == "" || len(plan.Jobs) == 0 {
		return "", errors.New("INVALID_ARGUMENT: incomplete action plan")
	}
	if len(plan.Jobs) > 100 { return "", errors.New("LIMIT_EXCEEDED: too many jobs") }
	seen := map[string]bool{}
	for _, j := range plan.Jobs {
		if j.Name == "" || seen[j.Name] || len(j.Steps) == 0 || len(j.Steps) > 100 {
			return "", errors.New("INVALID_ARGUMENT: duplicate/empty job or invalid steps")
		}
		seen[j.Name] = true
	}
	for _, j := range plan.Jobs {
		for _, dep := range j.Needs {
			if !seen[dep] || dep == j.Name { return "", errors.New("INVALID_ARGUMENT: invalid job dependency") }
		}
		for _, c := range j.Steps {
			if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Run) == "" {
				return "", errors.New("INVALID_ARGUMENT: empty command")
			}
		}
	}
	id, err := newUUIDv7()
	if err != nil { return "", err }
	tx, err := s.DB.BeginTx(ctx,nil)
	if err != nil { return "", err }
	defer tx.Rollback()
	var previous string
	if plan.EventID != "" {
		err = tx.QueryRowContext(ctx, `SELECT action_run_id FROM action_runs WHERE event_id=? AND workflow_path=?`,
			plan.EventID, plan.WorkflowPath).Scan(&previous)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT action_run_id FROM action_runs WHERE clone_id=? AND commit_sha=? AND workflow_path=? AND event_id IS NULL`,
			plan.CloneID, plan.CommitSHA, plan.WorkflowPath).Scan(&previous)
	}
	if err == nil { return previous, nil }
	if !errors.Is(err, sql.ErrNoRows) { return "", err }
	_, err = tx.ExecContext(ctx, `INSERT INTO action_runs
		(action_run_id,clone_id,workflow_path,commit_sha,branch,vm_profile,event_id,scope,owner_uid,status,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		id,plan.CloneID,plan.WorkflowPath,plan.CommitSHA,plan.Branch,plan.VMProfile,
		nullEventID(plan.EventID),plan.Scope,int64(plan.OwnerUID),"queued",now())
	if err != nil { return "", err }
	for _, j := range plan.Jobs {
		deps, err := json.Marshal(j.Needs)
		if err != nil { return "", err }
		status := "queued"
		if j.SkipReason != "" { status = "skipped" }
		res, err := tx.ExecContext(ctx,`INSERT INTO action_jobs(action_run_id,job_name,needs_json,skip_reason,status)
			VALUES(?,?,?,?,?)`, id, j.Name, string(deps), j.SkipReason, status)
		if err != nil { return "", err }
		jobID, err := res.LastInsertId()
		if err != nil { return "", err }
		for index, step := range j.Steps {
			commandID, err := newUUIDv7()
			if err != nil { return "", err }
			_, err = tx.ExecContext(ctx,`INSERT INTO action_job_commands
				(command_id,job_id,step_order,step_name,working_directory,command_text,status)
				VALUES(?,?,?,?,?,?,?)`,commandID,jobID,index,step.Name,step.WorkingDirectory,step.Run,status)
			if err != nil { return "", err }
		}
	}
	if err = tx.Commit(); err != nil { return "", err }
	return id,nil
}

// ReadAction is for the internal worker. MCP exposure uses scoped methods below.
func (s *Store) ReadAction(ctx context.Context, id string) (ActionResult,error) {
	var a ActionResult
	var uid int64
	err := s.DB.QueryRowContext(ctx,`SELECT action_run_id,clone_id,workflow_path,commit_sha,
		branch,vm_profile,insula_vm_id,event_id,(SELECT event_type FROM action_events WHERE event_id=action_runs.event_id),
		scope,owner_uid,status,created_at,started_at,finished_at
		FROM action_runs WHERE action_run_id=?`,id).
		Scan(&a.ActionRunID,&a.CloneID,&a.WorkflowPath,&a.CommitSHA,&a.Branch,&a.VMProfile,&a.InsulaVMID,&a.EventID,&a.EventType,
			&a.Scope,&uid,&a.Status,&a.CreatedAt,&a.StartedAt,&a.FinishedAt)
	if err != nil { return ActionResult{},err }
	a.OwnerUID=uint32(uid)
	rows,err := s.DB.QueryContext(ctx,`SELECT id,job_name,needs_json,skip_reason,status,exit_code,started_at,finished_at
		FROM action_jobs WHERE action_run_id=? ORDER BY id ASC`,id)
	if err != nil { return ActionResult{},err }
	a.Jobs=[]ActionJobResult{}
	for rows.Next() {
		var j ActionJobResult
		var deps string
		if err=rows.Scan(&j.ID,&j.Name,&deps,&j.SkipReason,&j.Status,&j.ExitCode,&j.StartedAt,&j.FinishedAt);err!=nil { break }
		err=json.Unmarshal([]byte(deps),&j.Needs)
		if err!=nil { break }
		a.Jobs=append(a.Jobs,j)
	}
	if err==nil {err=rows.Err()}
	rows.Close()
	if err!=nil {return ActionResult{},err}
	for i:=range a.Jobs {
		cmds,err:=s.DB.QueryContext(ctx,`SELECT command_id,step_order,step_name,working_directory,
			status,exit_code,started_at,finished_at,log_execution_id
			FROM action_job_commands WHERE job_id=? ORDER BY step_order ASC`,a.Jobs[i].ID)
		if err!=nil {return ActionResult{},err}
		a.Jobs[i].Commands=[]ActionCommandResult{}
		for cmds.Next(){
			var c ActionCommandResult
			if err=cmds.Scan(&c.CommandID,&c.StepOrder,&c.StepName,&c.WorkingDir,
				&c.Status,&c.ExitCode,&c.StartedAt,&c.FinishedAt,&c.LogExecutionID);err!=nil {break}
			a.Jobs[i].Commands=append(a.Jobs[i].Commands,c)
		}
		if err==nil {err=cmds.Err()}
		cmds.Close()
		if err!=nil {return ActionResult{},err}
	}
	messages,err:=s.ReadActionRuntimeMessages(ctx,id)
	if err!=nil{return ActionResult{},err}
	a.RuntimeEvents=messages
	return a,nil
}

// ActionCommandScript is available only within the worker, not as an MCP output.
func (s *Store) ActionCommandScript(ctx context.Context,id string) (string,error) {
	var script string
	err:=s.DB.QueryRowContext(ctx,"SELECT command_text FROM action_job_commands WHERE command_id=?",id).Scan(&script)
	return script,err
}

// ListCloneActionsFiltered applies predicates before LIMIT, so an older
// action_run_id is never hidden by the latest 100 actions in the clone.
func (s *Store) ListCloneActionsFiltered(
	ctx context.Context, cloneID, scope string, uid uint32,
	actionID, status string, limit int, admin bool,
) ([]ActionResult,error) {
	if cloneID == "" { return nil,errors.New("INVALID_ARGUMENT: clone_id required") }
	if limit == 0 { limit = 50 }
	if limit < 1 || limit > 100 { return nil,errors.New("INVALID_ARGUMENT: limit must be 1..100") }
	switch status {
	case "", "queued", "running", "succeeded", "failed", "cancelled", "interrupted":
	default: return nil,errors.New("INVALID_ARGUMENT: invalid action status")
	}
	query := `SELECT action_run_id FROM action_runs WHERE clone_id=?`
	args := []any{cloneID}
	if !admin {
		query += ` AND scope=? AND owner_uid=?`
		args = append(args,scope,int64(uid))
	}
	if actionID != "" {
		query += ` AND action_run_id=?`
		args = append(args,actionID)
	}
	if status != "" {
		query += ` AND status=?`
		args = append(args,status)
	}
	query += ` ORDER BY created_at DESC,action_run_id DESC LIMIT ?`
	args = append(args,limit)
	rows,err:=s.DB.QueryContext(ctx,query,args...)
	if err!=nil{return nil,err}
	ids:=[]string{}
	for rows.Next(){var id string;if err=rows.Scan(&id);err!=nil{break};ids=append(ids,id)}
	if err==nil{err=rows.Err()}
	rows.Close()
	if err!=nil{return nil,err}
	out:=make([]ActionResult,0,len(ids))
	for _,id:=range ids{
		a,err:=s.ReadAction(ctx,id)
		if err!=nil{return nil,err}
		out=append(out,a)
	}
	return out,nil
}

func (s *Store) ListCloneActions(ctx context.Context, cloneID,scope string,uid uint32) ([]ActionResult,error) {
	rows,err:=s.DB.QueryContext(ctx,`SELECT action_run_id FROM action_runs
		WHERE clone_id=? AND scope=? AND owner_uid=? ORDER BY created_at DESC,action_run_id DESC LIMIT 100`,
		cloneID,scope,int64(uid))
	if err!=nil{return nil,err}
	ids:=[]string{}
	for rows.Next(){var id string;if err=rows.Scan(&id);err!=nil{break};ids=append(ids,id)}
	if err==nil{err=rows.Err()}
	rows.Close()
	if err!=nil{return nil,err}
	out:=make([]ActionResult,0,len(ids))
	for _,id:=range ids{a,err:=s.ReadAction(ctx,id);if err!=nil{return nil,err};out=append(out,a)}
	return out,nil
}
func (s *Store) ListRunningActions(ctx context.Context) ([]ActionResult,error) {
	rows,err:=s.DB.QueryContext(ctx,`SELECT action_run_id FROM action_runs WHERE status='running'
		ORDER BY started_at ASC,action_run_id ASC LIMIT 100`)
	if err!=nil{return nil,err}
	ids:=[]string{}
	for rows.Next(){var id string;if err=rows.Scan(&id);err!=nil{break};ids=append(ids,id)}
	if err==nil{err=rows.Err()}
	rows.Close()
	if err!=nil{return nil,err}
	out:=make([]ActionResult,0,len(ids))
	for _,id:=range ids{a,err:=s.ReadAction(ctx,id);if err!=nil{return nil,err};out=append(out,a)}
	return out,nil
}

func (s *Store) StartAction(ctx context.Context,id string) error {
	r,err:=s.DB.ExecContext(ctx,`UPDATE action_runs SET status='running',started_at=?
		WHERE action_run_id=? AND status='queued'`,now(),id)
	return checkSingleRow(r,err)
}
func (s *Store) FinishAction(ctx context.Context,id,status string) error {
	if status!="succeeded" && status!="failed" && status!="cancelled" {return errors.New("invalid action status")}
	r,err:=s.DB.ExecContext(ctx,`UPDATE action_runs SET status=?,finished_at=?
		WHERE action_run_id=? AND status='running'`,status,now(),id)
	return checkSingleRow(r,err)
}
func (s *Store) StartJob(ctx context.Context,id int64) error {
	r,err:=s.DB.ExecContext(ctx,`UPDATE action_jobs SET status='running',started_at=?
		WHERE id=? AND status='queued'`,now(),id)
	return checkSingleRow(r,err)
}
func (s *Store) FinishJob(ctx context.Context,id int64,status string,code *int) error {
	if status!="succeeded" && status!="failed" && status!="skipped" && status!="cancelled" {return errors.New("invalid job status")}
	r,err:=s.DB.ExecContext(ctx,`UPDATE action_jobs SET status=?,exit_code=?,finished_at=?
		WHERE id=? AND status IN ('running','queued')`,status,code,now(),id)
	return checkSingleRow(r,err)
}
func (s *Store) StartCommand(ctx context.Context,id,logID string) error {
	r,err:=s.DB.ExecContext(ctx,`UPDATE action_job_commands SET status='running',started_at=?,log_execution_id=?
		WHERE command_id=? AND status='queued'`,now(),logID,id)
	return checkSingleRow(r,err)
}
// SkipQueuedCommands records that a failed dependency prevented later steps.
func (s *Store) SkipQueuedCommands(ctx context.Context, jobID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE action_job_commands
		SET status='skipped',finished_at=? WHERE job_id=? AND status='queued'`,now(),jobID)
	return err
}

func (s *Store) FinishCommand(ctx context.Context,id,status string,code *int) error {
	if status!="succeeded" && status!="failed" && status!="cancelled" {return errors.New("invalid command status")}
	r,err:=s.DB.ExecContext(ctx,`UPDATE action_job_commands SET status=?,exit_code=?,finished_at=?
		WHERE command_id=? AND status='running'`,status,code,now(),id)
	return checkSingleRow(r,err)
}

func checkSingleRow(result sql.Result,err error) error {
	if err!=nil{return err}
	n,err:=result.RowsAffected()
	if err!=nil{return err}
	if n!=1{return fmt.Errorf("CONFLICT: action state transition rejected")}
	return nil
}

// A restart removes log rows and marks unfinished work as interrupted.
// It must never claim a command succeeded without seeing its exit code.
func (s *Store) InterruptRunningActions(ctx context.Context) error {
	tx,err:=s.DB.BeginTx(ctx,nil)
	if err!=nil{return err}
	defer tx.Rollback()
	for _,q:=range []string{
		`UPDATE action_job_commands SET status='interrupted',finished_at=?
			WHERE status='queued' AND job_id IN (SELECT id FROM action_jobs WHERE action_run_id IN
			(SELECT action_run_id FROM action_runs WHERE status='running'))`,
		`UPDATE action_job_commands SET status='interrupted',finished_at=? WHERE status='running'`,
		`UPDATE action_jobs SET status='interrupted',finished_at=?
			WHERE status='queued' AND action_run_id IN (SELECT action_run_id FROM action_runs WHERE status='running')`,
		`UPDATE action_jobs SET status='interrupted',finished_at=? WHERE status='running'`,
		`UPDATE action_runs SET status='interrupted',finished_at=? WHERE status='running'`,
	}{
		if _,err=tx.ExecContext(ctx,q,now());err!=nil{return err}
	}
	return tx.Commit()
}

 // FailManualAction finalizes a manually scheduled run whose guest could not
 // boot, transfer files or finish. It never changes completed actions.
func (s *Store) FailManualAction(ctx context.Context, id string) error {
	tx,err:=s.DB.BeginTx(ctx,nil)
	if err!=nil{return err}
	defer tx.Rollback()
	_,err=tx.ExecContext(ctx,`UPDATE action_job_commands SET status='interrupted',finished_at=?
		WHERE status IN ('queued','running') AND job_id IN
		(SELECT id FROM action_jobs WHERE action_run_id=?)`,now(),id)
	if err!=nil{return err}
	_,err=tx.ExecContext(ctx,`UPDATE action_jobs SET status='interrupted',finished_at=?
		WHERE action_run_id=? AND status IN ('queued','running')`,now(),id)
	if err!=nil{return err}
	_,err=tx.ExecContext(ctx,`UPDATE action_runs SET status='failed',finished_at=?
		WHERE action_run_id=? AND status IN ('queued','running')`,now(),id)
	if err!=nil{return err}
	return tx.Commit()
}

func nullEventID(id string) any {
	if id == "" { return nil }
	return id
}
