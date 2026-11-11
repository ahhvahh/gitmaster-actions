package gitmasteractions

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeActionSnapshot(t *testing.T, dir, filename, body string) string {
	t.Helper()
	file := filepath.Join(dir, filename)
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	content := []byte(body)
	if err := tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}
func TestRunFileUsesDurableAsyncExecutor(t *testing.T) {
	r := &testRuntime{}
	e, _ := newTestEngine(t, r)
	defer e.Close()
	dir := t.TempDir()
	writeActionSnapshot(t, dir, "material.tar.gz", "material")
	yaml := `schema_version: 1
vm:
  profile: ci-go
action:
  name: legacy-local
  imports:
    - source: material.tar.gz
  jobs:
    - name: build
      commands: ["go version"]
`
	file := filepath.Join(dir, "action.yaml")
	if err := os.WriteFile(file, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	action, err := e.RunFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if action.Status != "succeeded" {
		t.Fatalf("status: %s", action.Status)
	}
	d, err := e.GetActionDetails(context.Background(), action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.VMID != "vm-test" || d.CleanupStatus != "succeeded" || d.MaterialSHA256 == "" || len(d.Events) < 3 {
		t.Fatalf("RunFile did not use unified executor: %#v", d)
	}
}
func TestIdempotentReplayRejectsChangedYAMLOrArchive(t *testing.T) {
	r := &testRuntime{}
	e, _ := newTestEngine(t, r)
	defer e.Close()
	dir := t.TempDir()
	a := writeActionSnapshot(t, dir, "first.tar.gz", "one")
	b := writeActionSnapshot(t, dir, "second.tar.gz", "two")
	request := SubmitRequest{Format: FormatGitMasterV1, Origin: originTest(), YAML: []byte(gitmasterWorkflow), ArchivePath: a}
	original, err := e.SubmitYAML(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	same, err := e.SubmitYAML(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if original.ID != same.ID {
		t.Fatal("duplicate action created")
	}
	changed := request
	changed.YAML = []byte(strings.Replace(gitmasterWorkflow, "go build ./...", "go build -v ./...", 1))
	if _, err = e.SubmitYAML(context.Background(), changed); err == nil {
		t.Fatal("changed YAML replay accepted")
	}
	changed = request
	changed.ArchivePath = b
	if _, err = e.SubmitYAML(context.Background(), changed); err == nil {
		t.Fatal("changed snapshot replay accepted")
	}
	if _, err = e.WaitAction(context.Background(), original.ID); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentActionsAndWorkerBound(t *testing.T) {
	runtime := &testRuntime{}
	e, _ := newTestEngine(t, runtime)
	defer e.Close()
	const count = 12
	var wg sync.WaitGroup
	errorsCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			a, err := e.SubmitYAML(ctx, SubmitRequest{Format: FormatActionV1, YAML: []byte(validManifest)})
			if err == nil {
				var done *Action
				done, err = e.WaitAction(ctx, a.ID)
				if err == nil && done.Status != "succeeded" {
					err = fmt.Errorf("state=%s", done.Status)
				}
			}
			if err != nil {
				errorsCh <- err
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
	runtime.mu.Lock()
	starts, stops := runtime.starts, runtime.stops
	runtime.mu.Unlock()
	if starts != count || stops != count {
		t.Fatalf("VM lifecycle: %d starts %d stops", starts, stops)
	}
}

type failingRuntime struct {
	*testRuntime
	startErr, stopErr error
}

func (r *failingRuntime) Start(ctx context.Context, profile string, fn func(string, string)) (string, error) {
	if r.startErr != nil {
		return "", r.startErr
	}
	return r.testRuntime.Start(ctx, profile, fn)
}
func (r *failingRuntime) Stop(ctx context.Context, id string) error {
	err := r.testRuntime.Stop(ctx, id)
	if err != nil {
		return err
	}
	return r.stopErr
}
func TestStartAndCleanupFailuresRemainQueryable(t *testing.T) {
	for _, stage := range []string{"start", "stop"} {
		t.Run(stage, func(t *testing.T) {
			runtime := &failingRuntime{testRuntime: &testRuntime{}}
			if stage == "start" {
				runtime.startErr = errors.New("mock VM bootstrap failure")
			} else {
				runtime.stopErr = errors.New("mock VM cleanup failure")
			}
			engine, err := New(Options{DatabasePath: filepath.Join(t.TempDir(), "actions.db"), RuntimeFactory: func() (Runtime, error) { return runtime, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			action, err := engine.SubmitYAML(context.Background(), SubmitRequest{Format: FormatActionV1, YAML: []byte(validManifest)})
			if err != nil {
				t.Fatal(err)
			}
			completed, err := engine.WaitAction(context.Background(), action.ID)
			if err != nil {
				t.Fatal(err)
			}
			if completed.Status != "failed" {
				t.Fatalf("status %s", completed.Status)
			}
			d, err := engine.GetActionDetails(context.Background(), action.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stage == "stop" && d.CleanupStatus != "failed" {
				t.Fatalf("missing cleanup error: %#v", d)
			}
			if len(d.Jobs) != 1 || len(d.Jobs[0].Commands) != 1 {
				t.Fatal("lost pending work graph")
			}
		})
	}
}
func TestJobTimeoutDoesNotLeaveRunningRecords(t *testing.T) {
	runtime := &testRuntime{block: true}
	e, _ := newTestEngine(t, runtime)
	defer e.Close()
	workflow := strings.Replace(gitmasterWorkflow, " build:\n   steps:", " build:\n   timeout_seconds: 1\n   steps:", 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	action, err := e.SubmitYAML(ctx, SubmitRequest{Format: FormatGitMasterV1, Origin: originTest(), YAML: []byte(workflow)})
	if err != nil {
		t.Fatal(err)
	}
	done, err := e.WaitAction(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "failed" {
		t.Fatalf("timed out job must fail, got %s", done.Status)
	}
	details, err := e.GetActionDetails(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range details.Jobs {
		if job.Job.Status == "running" || job.Job.Status == "pending" {
			t.Fatalf("stuck job %s", job.Job.Status)
		}
		for _, cmd := range job.Commands {
			if cmd.Command.Status == "running" || cmd.Command.Status == "pending" {
				t.Fatalf("stuck command %s", cmd.Command.Status)
			}
		}
	}
}
func TestExclusiveActionsDBOwnership(t *testing.T) {
	database := filepath.Join(t.TempDir(), "actions.db")
	first, err := New(Options{DatabasePath: database})
	if err != nil {
		t.Fatal(err)
	}
	if second, err := New(Options{DatabasePath: database}); err == nil {
		_ = second.Close()
		t.Fatal("second Engine opened same actions.db and could corrupt running recovery")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := New(Options{DatabasePath: database})
	if err != nil {
		t.Fatal("lock not released:", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestOutputWriterLimitsUnterminatedLines(t *testing.T) {
	writer := NewOutputWriter(func(stream, line string) error { return nil })
	_, err := writer.Write([]byte(strings.Repeat("X", (4<<20)+1)))
	if err == nil {
		t.Fatal("accepted unbounded line without newline")
	}
}

func TestRunYAMLContextCancellationStopsVM(t *testing.T) {
	runtime := &testRuntime{block: true}
	engine, _ := newTestEngine(t, runtime)
	defer engine.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := engine.RunYAML(ctx, []byte(validManifest))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	actions, err := engine.ListActions(context.Background(), ActionFilter{}, Page{})
	if err != nil || len(actions) != 1 || actions[0].Status != "cancelled" {
		t.Fatalf("canceled Action not durable: %#v %v", actions, err)
	}
	runtime.mu.Lock()
	stops := runtime.stops
	runtime.mu.Unlock()
	if stops != 1 {
		t.Fatalf("VM cleanup missing: %d", stops)
	}
}