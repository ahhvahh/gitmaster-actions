package gitmasteractions

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const gitmasterWorkflow = `version: 1
name: ci
vm_profile: ci-go
triggers:
 - event: commit
   branches: [develop]
jobs:
 build:
   steps:
    - name: build
      working_directory: src/go
      run: go build ./...
 test:
   needs: [build]
   steps:
    - name: test
      run: go test ./...
`

type testRuntime struct {
	mu                      sync.Mutex
	starts, stops, commands int
	failCommand             bool
	block                   bool
}

func (m *testRuntime) Profile(n string) error {
	if n != "ci-go" {
		return errors.New("missing profile")
	}
	return nil
}
func (m *testRuntime) Workspace() string { return "/srv/guest" }
func (m *testRuntime) Start(ctx context.Context, n string, fn func(string, string)) (string, error) {
	m.mu.Lock()
	m.starts++
	m.mu.Unlock()
	return "vm-test", ctx.Err()
}
func (m *testRuntime) Deploy(context.Context, string, string, string, io.Writer) error { return nil }
func (m *testRuntime) Command(ctx context.Context, id, dir, script string, out io.Writer) (int, error) {
	m.mu.Lock()
	m.commands++
	fail, block := m.failCommand, m.block
	m.mu.Unlock()
	if block {
		<-ctx.Done()
		return -1, ctx.Err()
	}
	if _, err := io.WriteString(out, "[command] test output\n"); err != nil {
		return -1, err
	}
	if fail {
		return 13, errors.New("remote exit")
	}
	return 0, nil
}
func (m *testRuntime) Stop(context.Context, string) error {
	m.mu.Lock()
	m.stops++
	m.mu.Unlock()
	return nil
}
func newTestEngine(t *testing.T, r *testRuntime) (*Engine, string) {
	t.Helper()
	db := filepath.Join(t.TempDir(), "actions.db")
	e, err := New(Options{DatabasePath: db, RuntimeFactory: func() (Runtime, error) { return r, nil }, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	return e, db
}
func originTest() ActionOrigin {
	return ActionOrigin{CloneID: "0199c761-d56a-7000-8000-9bcc8a23ce47", CommitSHA: strings.Repeat("a", 40), WorkflowPath: ".gitmaster/workflow/ci.yaml", EventType: "commit", EventID: "event-1", Branch: "develop", WorkflowName: "ci"}
}
func TestExplicitAdapterAndDAG(t *testing.T) {
	o := originTest()
	if _, err := parseSubmission(SubmitRequest{YAML: []byte(gitmasterWorkflow), Origin: o}); err == nil {
		t.Fatal("format must be explicit")
	}
	p, err := parseSubmission(SubmitRequest{Format: FormatGitMasterV1, YAML: []byte(gitmasterWorkflow), Origin: o})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Jobs) != 2 || p.Jobs[0].Name != "build" || p.Jobs[1].Name != "test" {
		t.Fatalf("incorrect DAG: %#v", p)
	}
	bad := strings.Replace(gitmasterWorkflow, "needs: [build]", "needs: [test]", 1)
	if _, err := parseSubmission(SubmitRequest{Format: FormatGitMasterV1, YAML: []byte(bad), Origin: o}); err == nil {
		t.Fatal("cyclic graph was accepted")
	}
	bad = strings.Replace(gitmasterWorkflow, "working_directory: src/go", "working_directory: ../host", 1)
	if _, err := parseSubmission(SubmitRequest{Format: FormatGitMasterV1, YAML: []byte(bad), Origin: o}); err == nil {
		t.Fatal("unsafe guest directory was accepted")
	}
	o.EventType = "push"
	if _, err := parseSubmission(SubmitRequest{Format: FormatGitMasterV1, YAML: []byte(gitmasterWorkflow), Origin: o}); err == nil {
		t.Fatal("unmatched trigger was accepted")
	}
	if _, err := parseSubmission(SubmitRequest{Format: FormatActionV1, YAML: []byte(gitmasterWorkflow)}); err == nil {
		t.Fatal("workflow was silently converted")
	}
}
func TestSubmittedActionPersistedAndQueryable(t *testing.T) {
	ctx := context.Background()
	runtime := &testRuntime{}
	e, db := newTestEngine(t, runtime)
	req := SubmitRequest{Format: FormatGitMasterV1, YAML: []byte(gitmasterWorkflow), Origin: originTest()}
	a, err := e.SubmitYAML(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.SubmitYAML(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != again.ID {
		t.Fatalf("duplicate action: %s %s", a.ID, again.ID)
	}
	done, err := e.WaitAction(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "succeeded" {
		t.Fatalf("action status=%s err=%v", done.Status, done.ErrorMessage)
	}
	details, err := e.GetActionDetails(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Jobs) != 2 || len(details.Jobs[0].Commands) != 1 || details.VMID != "vm-test" || details.CleanupStatus != "succeeded" {
		t.Fatalf("incomplete details: %#v", details)
	}
	cmdID := details.Jobs[0].Commands[0].Command.ID
	logs, err := e.ListCommandLogs(ctx, cmdID, 0)
	if err != nil || len(logs) == 0 {
		t.Fatalf("missing logs: %v %#v", err, logs)
	}
	outputs, err := e.ListCommandOutputs(ctx, cmdID, OutputFilter{}, Page{})
	if err != nil || len(outputs) == 0 {
		t.Fatalf("missing outputs: %v %#v", err, outputs)
	}
	events, err := e.ListRuntimeEvents(ctx, a.ID, Page{})
	if err != nil || len(events) < 3 {
		t.Fatalf("missing events: %v %#v", err, events)
	}
	runtime.mu.Lock()
	starts, stops := runtime.starts, runtime.stops
	runtime.mu.Unlock()
	if starts != 1 || stops != 1 {
		t.Fatalf("unexpected VM lifecycle: start=%d stop=%d", starts, stops)
	}
	for i := 0; i < 50; i++ {
		err = e.Close()
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(Options{DatabasePath: db})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, err := reopened.GetActionDetails(ctx, a.ID)
	if err != nil || saved.Action.Status != "succeeded" {
		t.Fatalf("lost history on restart: %v %#v", err, saved)
	}
}
func TestCancellationAndCleanup(t *testing.T) {
	r := &testRuntime{block: true}
	e, _ := newTestEngine(t, r)
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, err := e.SubmitYAML(ctx, SubmitRequest{Format: FormatActionV1, YAML: []byte(validManifest)})
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the VM starts so that cancellation exercises cleanup.
	for i := 0; i < 100; i++ {
		r.mu.Lock()
		n := r.commands
		r.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := e.CancelAction(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	done, err := e.WaitAction(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "cancelled" {
		t.Fatalf("status %s", done.Status)
	}
	details, err := e.GetActionDetails(ctx, a.ID)
	if err != nil || details.CleanupStatus != "succeeded" {
		t.Fatalf("cleanup missing: %v %#v", err, details)
	}
}
func TestInfrastructureAndFailedCommandsRemainInHistory(t *testing.T) {
	r := &testRuntime{failCommand: true}
	e, _ := newTestEngine(t, r)
	defer e.Close()
	a, err := e.SubmitYAML(context.Background(), SubmitRequest{Format: FormatActionV1, YAML: []byte(validManifest)})
	if err != nil {
		t.Fatal(err)
	}
	done, err := e.WaitAction(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "failed" {
		t.Fatalf("expected failure, got %s", done.Status)
	}
	details, err := e.GetActionDetails(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	cmd := details.Jobs[0].Commands[0]
	if cmd.Command.ExitCode == nil || *cmd.Command.ExitCode != 13 || cmd.FailureKind != "remote_exit" {
		t.Fatalf("wrong exit: %#v", cmd)
	}
}
func TestRecoveryMarksPendingInterrupted(t *testing.T) {
	ctx := context.Background()
	e, db := newTestEngine(t, &testRuntime{})
	id, err := uuidv7()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.db.ExecContext(ctx, `INSERT INTO actions(id,name,status,created_at,vm_id) VALUES(?,?,?,?,?)`, id, "old", "running", timestamp(), "owned-vm"); err != nil {
		t.Fatal(err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(Options{DatabasePath: db})
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	d, err := recovered.GetActionDetails(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Action.Status != "failed" || d.CleanupStatus != "pending" {
		t.Fatalf("interrupted action lost: %#v", d)
	}
}
func TestUnsafeSnapshotRejected(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "unsafe.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err = tw.WriteHeader(&tar.Header{Name: "../../outside", Mode: 0600, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err = tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = safeSnapshotArchive(archive); err == nil {
		t.Fatal("accepted archive path traversal")
	}
}
func TestSnapshotCredentialPaths(t *testing.T) {
	for _, name := range []string{
		".env", "src/.env.production", ".ssh/id_rsa", "tools/id_ed25519",
		".aws/credentials", "secrets/client.key", "certs/private.p12",
	} {
		if !forbiddenSnapshotPath(name) {
			t.Fatalf("credential material accepted: %q", name)
		}
	}
	for _, name := range []string{
		".env.example", ".env.sample", ".env.template", "src/main.go", "docs/key-reference.md",
	} {
		if forbiddenSnapshotPath(name) {
			t.Fatalf("non-secret path rejected: %q", name)
		}
	}
}

func TestSnapshotArchiveRejectsSecretFile(t *testing.T) {
	name := filepath.Join(t.TempDir(), "secret.tar.gz")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tarWriter := tar.NewWriter(gz)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "src/.env.production", Mode: 0600, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := safeSnapshotArchive(name); err == nil {
		t.Fatal("accepted credential file inside TAR.GZ")
	}
}

func TestReconcileInterruptedDoesNotStopRunningAction(t *testing.T) {
	ctx := context.Background()
	rt := &testRuntime{block: true}
	e, _ := newTestEngine(t, rt)
	defer e.Close()
	action, err := e.SubmitYAML(ctx, SubmitRequest{Format: FormatActionV1, YAML: []byte(validManifest)})
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the command is blocked inside the VM so cleanup is pending.
	deadline := time.After(3 * time.Second)
	for {
		rt.mu.Lock()
		n := rt.commands
		rt.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("VM command did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := e.ReconcileInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	stops := rt.stops
	rt.mu.Unlock()
	if stops != 0 {
		t.Fatalf("reconciler stopped a running VM: %d stops", stops)
	}
	if err := e.CancelAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.WaitAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
}
