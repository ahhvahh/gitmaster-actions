package gitmasteractions

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var actionSecrets = regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]+|gh[pousr]_[A-Za-z0-9_]+|Bearer\s+\S+)`)

func redactActionText(s string) string { return actionSecrets.ReplaceAllString(s, "[redacted]") }

// forbiddenSnapshotPath rejects common credential and private-key locations.
// This is defense in depth; callers must also screen their own repository secrets.
func forbiddenSnapshotPath(name string) bool {
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		switch part {
		case ".git", ".ssh", ".aws", ".gnupg", ".kube", ".netrc", ".npmrc", ".pypirc",
			".git-credentials", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
			return true
		}
		if part == ".env" || (strings.HasPrefix(part, ".env.") &&
			part != ".env.example" && part != ".env.sample" && part != ".env.template") {
			return true
		}
		if strings.HasSuffix(part, ".key") || strings.HasSuffix(part, ".p12") ||
			strings.HasSuffix(part, ".pfx") || strings.HasSuffix(part, ".kdbx") {
			return true
		}
	}
	return false
}

func safeSnapshotArchive(file string) error {
	fi, err := os.Lstat(file)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() || fi.Size() == 0 || fi.Size() > 100<<20 {
		return errors.New("archive must be regular TAR.GZ (<=100 MiB)")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("invalid gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := 0
	entries := 0
	var total int64
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		entries++
		if entries > 20000 {
			return errors.New("archive exceeds entry limit")
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || path.IsAbs(name) || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) || path.Clean(name) != name || strings.HasPrefix(name, "../") || name == ".." || seen[name] {
			return fmt.Errorf("unsafe or duplicate archive entry %q", name)
		}
		if forbiddenSnapshotPath(name) {
			return fmt.Errorf("archive contains sensitive path %q", name)
		}
		seen[name] = true
		if h.Typeflag == tar.TypeDir {
			if h.Size != 0 {
				return fmt.Errorf("directory has nonzero size %q", name)
			}
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return fmt.Errorf("unsupported archive entry %q", name)
		}
		if h.Size < 0 {
			return errors.New("negative archive entry size")
		}
		files++
		total += h.Size
		if files > 15000 || total > 256<<20 {
			return errors.New("archive exceeds file/count limits")
		}
		if _, err := io.CopyN(io.Discard, tr, h.Size); err != nil {
			return err
		}
	}
	if files == 0 {
		return errors.New("archive has no regular files")
	}
	return nil
}
func (e *Engine) stageSnapshot(ctx context.Context, origin string) (string, string, error) {
	if origin == "" {
		return "", "", nil
	}
	if !filepath.IsAbs(origin) || !strings.HasSuffix(strings.ToLower(origin), ".tar.gz") {
		return "", "", errors.New("ArchivePath must be absolute .tar.gz")
	}
	fi, err := os.Lstat(origin)
	if err != nil {
		return "", "", err
	}
	if !fi.Mode().IsRegular() || fi.Size() == 0 || fi.Size() > 100<<20 {
		return "", "", errors.New("archive must be a regular file <=100 MiB")
	}
	dir := filepath.Join(filepath.Dir(e.options.DatabasePath), "actions-material")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	dest, err := os.CreateTemp(dir, "snapshot-*.tar.gz")
	if err != nil {
		return "", "", err
	}
	name := dest.Name()
	ok := false
	defer func() {
		_ = dest.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	src, err := os.Open(origin)
	if err != nil {
		return "", "", err
	}
	defer src.Close()
	n, err := io.Copy(dest, io.LimitReader(src, (100<<20)+1))
	if err != nil {
		return "", "", err
	}
	if n > 100<<20 {
		return "", "", errors.New("archive size changed or exceeds 100 MiB")
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if err := dest.Sync(); err != nil {
		return "", "", err
	}
	if err := dest.Close(); err != nil {
		return "", "", err
	}
	if err := safeSnapshotArchive(name); err != nil {
		return "", "", err
	}
	f, err := os.Open(name)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", "", err
	}
	ok = true
	return name, hex.EncodeToString(h.Sum(nil)), nil
}
func (e *Engine) SubmitYAML(ctx context.Context, req SubmitRequest) (*Action, error) {
	if err := e.assertOpen(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	plan, err := parseSubmission(req)
	if err != nil {
		return nil, err
	}
	// Validate the real profile and guest configuration before accepting the Action.
	runtime, err := e.runtimeForActions()
	if err != nil {
		return nil, err
	}
	if err = runtime.Profile(plan.Profile); err != nil {
		return nil, fmt.Errorf("VM profile: %w", err)
	}
	workspace := runtime.Workspace()
	if !strings.HasPrefix(workspace, "/") || workspace == "/" || strings.ContainsAny(workspace, "\x00\r\n\\") {
		return nil, errors.New("unsafe InsulaVM guest.destination")
	}
	id, err := uuidv7()
	if err != nil {
		return nil, err
	}
	material, checksum, err := e.stageSnapshot(ctx, plan.ArchivePath)
	if err != nil {
		return nil, err
	}
	keepMaterial := false
	defer func() {
		if !keepMaterial && material != "" {
			_ = os.Remove(material)
		}
	}()
	origin := req.Origin
	if origin.WorkflowName == "" {
		origin.WorkflowName = plan.Name
	}
	// Fully qualified origin keys are unique across clone/SHA/workflow/event.
	if origin.CloneID != "" {
		var previous string
		err = e.db.QueryRowContext(ctx, `SELECT id FROM actions WHERE clone_id=? AND commit_sha=? AND workflow_path=? AND event_type=?`, origin.CloneID, origin.CommitSHA, origin.WorkflowPath, origin.EventType).Scan(&previous)
		if err == nil {
			return e.GetAction(ctx, previous)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO actions(id,name,status,created_at,clone_id,commit_sha,event_id,event_type,branch,workflow_name,workflow_path,yaml_format,vm_profile,material_sha256,material_path,cleanup_status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, plan.Name, "pending", timestamp(), origin.CloneID, origin.CommitSHA, origin.EventID, origin.EventType, origin.Branch, origin.WorkflowName, origin.WorkflowPath, string(req.Format), plan.Profile, checksum, material, "not_required")
	if err != nil {
		// Release the only SQLite connection before checking a concurrent winner.
		_ = tx.Rollback()
		if origin.CloneID != "" {
			var previous string
			if lookup := e.db.QueryRowContext(ctx, `SELECT id FROM actions WHERE clone_id=? AND commit_sha=? AND workflow_path=? AND event_type=?`, origin.CloneID, origin.CommitSHA, origin.WorkflowPath, origin.EventType).Scan(&previous); lookup == nil {
				return e.GetAction(ctx, previous)
			}
		}
		return nil, err
	}
	for position, j := range plan.Jobs {
		jid, err := uuidv7()
		if err != nil {
			return nil, err
		}
		needs, err := json.Marshal(j.Needs)
		if err != nil {
			return nil, err
		}
		state := "pending"
		if j.SkipReason != "" {
			state = "skipped"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,action_id,name,position,allow_failure,status,needs_json,skip_reason,timeout_seconds) VALUES(?,?,?,?,?,?,?,?,?)`, jid, id, j.Name, position, j.AllowFailure, state, string(needs), j.SkipReason, j.TimeoutSeconds)
		if err != nil {
			return nil, err
		}
		for n, c := range j.Commands {
			cid, err := uuidv7()
			if err != nil {
				return nil, err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO commands(id,job_id,position,command_text,status,step_name,working_directory) VALUES(?,?,?,?,?,?,?)`, cid, jid, n, c.Run, state, c.Name, c.Directory)
			if err != nil {
				return nil, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO runtime_events(action_id,stage,message,recorded_at) VALUES(?,'queued','action accepted and persisted',?)`, id, timestamp()); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	keepMaterial = true
	runCtx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		cancel()
		_ = e.finishUnstarted(context.Background(), id, "cancelled")
		_ = e.finishQueued(context.Background(), id, "failed", "Engine closed after enqueue")
		return e.GetAction(context.Background(), id)
	}
	e.active[id] = cancel
	e.mu.Unlock()
	go e.processAction(runCtx, id, plan, material, runtime, cancel)
	return e.GetAction(ctx, id)
}

func (e *Engine) processAction(ctx context.Context, id string, plan executionPlan, material string, rt Runtime, cancel context.CancelFunc) {
	defer cancel()
	defer func() {
		e.mu.Lock()
		delete(e.active, id)
		e.mu.Unlock()
		if material != "" {
			_ = os.Remove(material)
		}
	}()
	// Bound VM concurrency; waiting actions remain durably queued.
	select {
	case e.asyncSlots <- struct{}{}:
		defer func() { <-e.asyncSlots }()
	case <-ctx.Done():
		e.completeAction(id, ctx.Err())
		return
	}
	err := e.runSubmittedAction(ctx, id, plan, material, rt)
	e.completeAction(id, err)
}
func (e *Engine) completeAction(id string, runErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status := "succeeded"
	message := ""
	if runErr != nil {
		status = "failed"
		message = redactActionText(runErr.Error())
	}
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		status = "cancelled"
	}
	// An interrupted or canceled job cannot remain pending or running.
	if status != "succeeded" {
		_ = e.finishUnstarted(ctx, id, "cancelled")
	}
	if err := e.finishQueued(ctx, id, status, message); err != nil {
		return
	}
	_ = e.addEvent(ctx, id, "finished", status+": "+message)
}
func (e *Engine) runSubmittedAction(ctx context.Context, id string, plan executionPlan, material string, rt Runtime) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.setStatus(ctx, "actions", id, "starting", ""); err != nil {
		return err
	}
	if err := e.addEvent(ctx, id, "starting", "starting isolated VM"); err != nil {
		return err
	}
	vmID, err := rt.Start(ctx, plan.Profile, func(stage, message string) {
		eventCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.addEvent(eventCtx, id, "vm:"+stage, message)
	})
	if err != nil {
		return fmt.Errorf("InsulaVM start: %w", err)
	}
	if vmID == "" {
		return errors.New("InsulaVM returned empty VM ID")
	}
	// Persist ownership before any transfer or command, enabling safe recovery.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cleanupErr := rt.Stop(cleanupCtx, vmID)
		status := "succeeded"
		message := ""
		if cleanupErr != nil {
			status = "failed"
			message = redactActionText(cleanupErr.Error())
		}
		persistCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		retErr = errors.Join(retErr, cleanupErr, e.setCleanup(persistCtx, id, status, message), e.addEvent(persistCtx, id, "cleanup", status+": "+message))
	}()
	persistCtx, persistCancel := context.WithTimeout(context.Background(), 10*time.Second)
	persistErr := e.setVM(persistCtx, id, vmID)
	persistCancel()
	if persistErr != nil {
		return persistErr
	}
	if err := e.addEvent(ctx, id, "vm_started", vmID); err != nil {
		return err
	}
	if err := e.setStatus(ctx, "actions", id, "running", ""); err != nil {
		return err
	}
	if material != "" {
		// InsulaVM performs extraction and transfer; neither shell nor SSH runs here.
		script, err := os.CreateTemp("", "actions-post-import-*.sh")
		if err != nil {
			return err
		}
		scriptPath := script.Name()
		defer os.Remove(scriptPath)
		if _, err = script.WriteString("#!/bin/bash\nset -euo pipefail\ntest -d .\n"); err != nil {
			_ = script.Close()
			return err
		}
		if err = script.Close(); err != nil {
			return err
		}
		if err = rt.Deploy(ctx, vmID, material, scriptPath, nil); err != nil {
			return fmt.Errorf("InsulaVM transfer: %w", err)
		}
		if err = e.addEvent(ctx, id, "material_deployed", "verified archive SHA256 and transferred to guest"); err != nil {
			return err
		}
	}
	jobs, err := e.ListJobs(ctx, id)
	if err != nil {
		return err
	}
	results := map[string]string{}
	anyFailed := false
	for i, j := range plan.Jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		jobID := jobs[i].ID
		if j.SkipReason != "" {
			results[j.Name] = "skipped"
			continue
		}
		blocked := false
		for _, need := range j.Needs {
			if results[need] != "succeeded" {
				blocked = true
				break
			}
		}
		if blocked {
			if err = e.setStatus(ctx, "jobs", jobID, "skipped", "dependency failed or skipped"); err != nil {
				return err
			}
			if err = e.skipCommands(ctx, jobID, "skipped"); err != nil {
				return err
			}
			results[j.Name] = "skipped"
			anyFailed = true
			continue
		}
		if err = e.setStatus(ctx, "jobs", jobID, "running", ""); err != nil {
			return err
		}
		cmds, err := e.ListCommands(ctx, jobID)
		if err != nil {
			return err
		}
		jobCtx := ctx
		jobCancel := func() {}
		if j.TimeoutSeconds > 0 {
			jobCtx, jobCancel = context.WithTimeout(ctx, time.Duration(j.TimeoutSeconds)*time.Second)
		}
		jobFailed := false
		infrastructure := false
		for k, c := range j.Commands {
			if err = jobCtx.Err(); err != nil {
				jobFailed = true
				infrastructure = true
				break
			}
			cid := cmds[k].ID
			if err = e.setStatus(jobCtx, "commands", cid, "running", ""); err != nil {
				jobFailed = true
				infrastructure = true
				break
			}
			dir := rt.Workspace()
			if c.Directory != "" && c.Directory != "." {
				dir = strings.TrimSuffix(dir, "/") + "/" + c.Directory
			}
			writer := NewOutputWriter(func(stream, line string) error {
				if stream == "system" {
					stream = "stdout"
				}
				// Preserve complete lines; redact known credential patterns.
				if len(line) > 4<<20 {
					return errors.New("command line exceeds 4 MiB capture limit")
				}
				return e.appendOutput(jobCtx, cid, stream, []byte(redactActionText(line)+"\n"))
			})
			exit, runErr := rt.Command(jobCtx, vmID, dir, c.Run, writer)
			runErr = errors.Join(runErr, writer.Flush())
			code := any(nil)
			if exit >= 0 {
				code = exit
			}
			failure := ""
			if runErr != nil || exit != 0 {
				failure = "remote_exit"
				if exit < 0 || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
					failure = "infrastructure"
				}
				if runErr == nil {
					runErr = fmt.Errorf("remote command exit code %d", exit)
				}
			}
			detail := ""
			if runErr != nil {
				detail = redactActionText(runErr.Error())
			}
			saveCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
			_, saveErr := e.db.ExecContext(saveCtx, `UPDATE commands SET exit_code=?,failure_kind=? WHERE id=?`, code, nullableString(failure), cid)
			if saveErr == nil {
				status := "succeeded"
				if failure != "" {
					status = "failed"
				}
				saveErr = e.setStatus(saveCtx, "commands", cid, status, detail)
			}
			if saveErr == nil {
				saveErr = e.addEvent(saveCtx, id, "command", fmt.Sprintf("job=%s step=%d status=%s exit=%d", j.Name, k, func() string {
					if failure != "" {
						return "failed"
					}
					return "succeeded"
				}(), exit))
			}
			done()
			if saveErr != nil {
				jobCancel()
				return fmt.Errorf("persist command result: %w", saveErr)
			}
			if failure != "" {
				if failure == "infrastructure" {
					infrastructure = true
				}
				if !j.AllowFailure || infrastructure {
					jobFailed = true
					break
				}
			}
		}
		if jobCtx.Err() != nil {
			jobFailed = true
			infrastructure = true
		}
		jobCancel()
		status := "succeeded"
		detail := ""
		if jobFailed {
			status = "failed"
			detail = "job command failed"
			anyFailed = true
		}
		saveCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		if err = e.setStatus(saveCtx, "jobs", jobID, status, detail); err == nil && jobFailed {
			err = e.skipCommands(saveCtx, jobID, "skipped")
		}
		done()
		if err != nil {
			return err
		}
		results[j.Name] = status
		if infrastructure {
			return fmt.Errorf("infrastructure failure in job %s", j.Name)
		}
	}
	if anyFailed {
		return errors.New("action has failed or skipped jobs")
	}
	return nil
}
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}