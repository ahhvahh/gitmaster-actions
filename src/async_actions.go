package gitmasteractions

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
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
	if strings.HasSuffix(strings.ToLower(file), ".zip") { return safeZipSnapshot(file) }
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
	if !filepath.IsAbs(origin) || !(strings.HasSuffix(strings.ToLower(origin), ".tar.gz") || strings.HasSuffix(strings.ToLower(origin), ".zip")) {
		return "", "", errors.New("ArchivePath must be absolute .zip or .tar.gz")
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
	suffix := ".tar.gz"
	if strings.HasSuffix(strings.ToLower(origin), ".zip") { suffix = ".zip" }
	dest, err := os.CreateTemp(dir, "snapshot-*" + suffix)
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
    if origin.WorkflowName != plan.Name {
        return nil, errors.New("origin workflow_name does not match the YAML name")
    }
    digest := sha256.Sum256(req.YAML)
    yamlHash := hex.EncodeToString(digest[:])
    // Replays may reuse only the identical immutable YAML and material.
    if origin.CloneID != "" {
        previous, lookupErr := e.findIdenticalSubmission(ctx, origin, yamlHash, checksum, req.Format)
        if lookupErr != nil { return nil, lookupErr }
        if previous != nil { return previous, nil }
    }
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO actions(id,name,status,created_at,clone_id,commit_sha,event_id,event_type,branch,workflow_name,workflow_path,yaml_format,vm_profile,material_sha256,material_path,cleanup_status,yaml_sha256) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, plan.Name, "pending", timestamp(), origin.CloneID, origin.CommitSHA, origin.EventID, origin.EventType, origin.Branch, origin.WorkflowName, origin.WorkflowPath, string(req.Format), plan.Profile, checksum, material, "not_required", yamlHash)
	if err != nil {
        // Release SQLite's single write connection before checking the winner.
        _ = tx.Rollback()
        if origin.CloneID != "" {
            previous, lookupErr := e.findIdenticalSubmission(ctx, origin, yamlHash, checksum, req.Format)
            if lookupErr != nil { return nil, lookupErr }
            if previous != nil { return previous, nil }
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
	// Close all unfinished children; no terminal Action may retain running Jobs.
    if status != "succeeded" {
        if errors.Is(runErr, context.Canceled) { _ = e.finishUnstarted(ctx, id, "cancelled") } else {
            _ = e.finishUnstarted(ctx, id, "failed")
        }
    }
    // If teardown failed, cancellation cannot conceal an unresolved VM.
    var cleanupStatus string
    if err := e.db.QueryRowContext(ctx, `SELECT COALESCE(cleanup_status,'') FROM actions WHERE id=?`, id).Scan(&cleanupStatus); err == nil && cleanupStatus == "failed" { status = "failed" }
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
	return e.executePlannedJobs(ctx, id, plan, rt, vmID)

}
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// safeZipSnapshot rejects zip-slip, symlinks, duplicate paths, credentials,
// encrypted entries and excessive decompression before the file reaches a VM.
func safeZipSnapshot(filename string) error {
    info,err:=os.Lstat(filename)
    if err!=nil{return err}
    if !info.Mode().IsRegular()||info.Size()==0||info.Size()>100<<20 {
        return errors.New("ZIP must be a regular archive <=100 MiB")
    }
    zr,err:=zip.OpenReader(filename)
    if err!=nil{return fmt.Errorf("invalid ZIP: %w",err)}
    defer zr.Close()
    if len(zr.File)==0||len(zr.File)>20000{return errors.New("ZIP entry count must be 1..20000")}
    seen:=make(map[string]bool,len(zr.File))
    count:=0
    var total uint64
    for _,file:=range zr.File {
        name:=strings.TrimSuffix(file.Name,"/")
        if name==""||path.IsAbs(name)||strings.Contains(name,"\\")||
           strings.ContainsRune(name,0)||path.Clean(name)!=name||
           strings.HasPrefix(name,"../")||name==".."||seen[name] {
            return fmt.Errorf("unsafe or duplicate ZIP path %q",name)
        }
        if forbiddenSnapshotPath(name){return fmt.Errorf("ZIP contains sensitive path %q",name)}
        seen[name]=true
        if file.Flags&0x1!=0{return fmt.Errorf("encrypted ZIP entry %q",name)}
        if file.FileInfo().IsDir(){continue}
        if !file.Mode().IsRegular(){return fmt.Errorf("ZIP entry is not a regular file %q",name)}
        count++
        total+=file.UncompressedSize64
        if count>15000||total>256<<20{return errors.New("ZIP exceeds file/count limits")}
        reader,err:=file.Open()
        if err!=nil{return err}
        n,copyErr:=io.Copy(io.Discard,io.LimitReader(reader,int64(file.UncompressedSize64)+1))
        closeErr:=reader.Close()
        if err:=errors.Join(copyErr,closeErr);err!=nil{return err}
        if uint64(n)!=file.UncompressedSize64{return fmt.Errorf("ZIP size mismatch for %q",name)}
    }
    if count==0{return errors.New("ZIP has no regular files")}
    return nil
}
