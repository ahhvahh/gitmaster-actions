package gitmasteractions

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyHistoryImportIsReadOnlyAndIdempotent(t *testing.T) {
	ctx := context.Background()
	r := &testRuntime{}
	e, _ := newTestEngine(t, r)
	defer e.Close()
	old := filepath.Join(t.TempDir(), "gitmaster.db")
	db, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`CREATE TABLE action_runs(action_run_id TEXT PRIMARY KEY,clone_id TEXT,commit_sha TEXT,status TEXT)`,
		`CREATE TABLE action_jobs(id INTEGER PRIMARY KEY,action_run_id TEXT,job_name TEXT)`,
		`CREATE TABLE action_job_commands(command_id TEXT PRIMARY KEY,job_id INTEGER,command_text TEXT)`,
		`CREATE TABLE action_command_responses(id INTEGER PRIMARY KEY,command_id TEXT,stream TEXT,message TEXT)`,
		`CREATE TABLE action_runtime_messages(id INTEGER PRIMARY KEY,action_run_id TEXT,stage TEXT,message TEXT)`,
		`INSERT INTO action_runs VALUES('legacy-1','clone-1','abc','succeeded')`,
		`INSERT INTO action_jobs VALUES(1,'legacy-1','build')`,
		`INSERT INTO action_job_commands VALUES('cmd-1',1,'go test ./...')`,
		`INSERT INTO action_command_responses VALUES(1,'cmd-1','stdout','ok')`,
		`INSERT INTO action_runtime_messages VALUES(1,'legacy-1','done','finished')`,
	} {
		if _, err = db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		count, err := e.ImportLegacyHistory(ctx, old)
		if err != nil || count != 5 {
			t.Fatalf("import %d: count %d err %v", n, count, err)
		}
	}
	records, err := e.ListLegacyRecords(ctx, "", "action_command_responses", Page{})
	if err != nil || len(records) != 1 {
		t.Fatalf("history %v %#v", err, records)
	}
	if len(records[0].Data) == 0 {
		t.Fatal("lost record body")
	}
	unchanged, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(unchanged) {
		t.Fatal("source SQLite modified")
	}
}