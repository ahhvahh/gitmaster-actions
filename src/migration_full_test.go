package gitmasteractions

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationKeepsRichLegacyHistoryAndNormalizedQueries(t *testing.T) {
	ctx := context.Background()
	engine, _ := newTestEngine(t, &testRuntime{})
	defer engine.Close()
	source := filepath.Join(t.TempDir(), "legacy-rich.db")
	old, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE action_runs(action_run_id TEXT PRIMARY KEY,clone_id TEXT,commit_sha TEXT,event_id TEXT,workflow_path TEXT,branch TEXT,vm_profile TEXT,insula_vm_id TEXT,status TEXT,created_at TEXT,started_at TEXT,finished_at TEXT,scope TEXT,owner_uid INTEGER)`,
		`CREATE TABLE action_jobs(id INTEGER PRIMARY KEY,action_run_id TEXT,job_name TEXT,needs_json TEXT,skip_reason TEXT,status TEXT,started_at TEXT,finished_at TEXT,exit_code INTEGER)`,
		`CREATE TABLE action_job_commands(command_id TEXT PRIMARY KEY,job_id INTEGER,step_order INTEGER,step_name TEXT,working_directory TEXT,command_text TEXT,status TEXT,exit_code INTEGER,started_at TEXT,finished_at TEXT)`,
		`CREATE TABLE action_command_responses(id INTEGER PRIMARY KEY,command_id TEXT,sequence INTEGER,stream TEXT,message TEXT,recorded_at TEXT)`,
		`CREATE TABLE action_runtime_messages(id INTEGER PRIMARY KEY,action_run_id TEXT,stage TEXT,message TEXT,recorded_at TEXT)`,
		`INSERT INTO action_runs VALUES('old-run','old-clone','abcdef','event','.gitmaster/workflow/test.yaml','develop','ci-go','old-vm','succeeded','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z','2026-01-01T00:00:02Z','scope',1000)`,
		`INSERT INTO action_jobs VALUES(7,'old-run','build','[]','','succeeded','2026-01-01T00:00:01Z','2026-01-01T00:00:02Z',0)`,
		`INSERT INTO action_job_commands VALUES('old-cmd',7,0,'check','src/go','go test ./...','succeeded',0,'2026-01-01T00:00:01Z','2026-01-01T00:00:02Z')`,
		`INSERT INTO action_command_responses VALUES(1,'old-cmd',1,'stdout','ok','2026-01-01T00:00:01Z')`,
		`INSERT INTO action_command_responses VALUES(2,'old-cmd',2,'system','complete','2026-01-01T00:00:02Z')`,
		`INSERT INTO action_runtime_messages VALUES(1,'old-run','finished','all jobs done','2026-01-01T00:00:02Z')`,
	}
	for _, stmt := range statements {
		if _, err = old.Exec(stmt); err != nil {
			t.Fatal(stmt, err)
		}
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		n, err := engine.ImportLegacyHistory(ctx, source)
		if err != nil || n != 6 {
			t.Fatalf("round=%d normalized %d records err=%v", round, n, err)
		}
	}
	id, err := engine.GetLegacyActionID(ctx, source, "old-run")
	if err != nil {
		t.Fatal(err)
	}
	d, err := engine.GetActionDetails(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Jobs) != 1 || len(d.Jobs[0].Commands) != 1 || len(d.Events) != 1 || d.VMID != "old-vm" || d.CleanupStatus != "historical" {
		t.Fatalf("historical fields missing: %#v", d)
	}
	if d.Origin.CloneID != "old-clone" || d.Origin.CommitSHA != "abcdef" || d.Origin.EventID != "event" {
		t.Fatalf("lost origin: %#v", d.Origin)
	}
	command := d.Jobs[0].Commands[0].Command
	if command.Text != "go test ./..." || command.ExitCode == nil || *command.ExitCode != 0 {
		t.Fatalf("lost command: %#v", command)
	}
	results, err := engine.ListCommandOutputs(ctx, command.ID, OutputFilter{}, Page{})
	if err != nil || len(results) != 2 || string(results[0].Payload) != "ok" || !strings.Contains(string(results[1].Payload), "legacy:system") {
		t.Fatalf("output stream not normalized: %v %#v", err, results)
	}
	logs, err := engine.ListCommandLogs(ctx, command.ID, 0)
	if err != nil || len(logs) != 2 {
		t.Fatalf("log projection missing: %v %#v", err, logs)
	}
	records, err := engine.ListLegacyRecords(ctx, source, "action_runs", Page{})
	if err != nil || len(records) != 1 || !strings.Contains(string(records[0].Data), "scope") {
		t.Fatalf("typed original not preserved: %v %#v", err, records)
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal("source removed:", err)
	}
}