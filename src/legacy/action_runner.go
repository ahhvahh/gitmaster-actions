package legacy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

)

// GuestCommandRunner executes a single versioned workflow step in an isolated
// VM, returning an exit code and streaming stdout/stderr to onOutput. It must
// NOT run untrusted workflow steps directly on the GitMaster host.
type GuestCommandRunner interface {
	Execute(ctx context.Context, workDir, script string, onOutput func(stream, line string) error) (int, error)
}

// ExecuteAction runs the persisted workflow snapshot in a VM that was already
// prepared and received the archive. One runner/VM is shared by all jobs of
// the action. Every step starts in /tmp/<clone_id>, optionally in a validated
// relative subdirectory. No new VM is created at job boundaries.
//
// VM provisioning, verified host keys and tar upload are handled by the
// separate VM orchestration layer; this function never starts a host shell.
func ExecuteAction(ctx context.Context, db *Store, runID string, guest GuestCommandRunner) error {
	a,err:=db.ReadAction(ctx,runID)
	if err!=nil{return err}
	return executeActionAt(ctx,db,runID,guest,"/tmp/"+a.CloneID,false)
}

// ExecuteClaimedActionAt continues an action already atomically claimed by the
// VM worker. WorkRoot is the absolute path prepared by InsulaVM DeployToVM.
func ExecuteClaimedActionAt(ctx context.Context,db *Store,runID string,
	guest GuestCommandRunner,workRoot string)error{
	return executeActionAt(ctx,db,runID,guest,workRoot,true)
}

func executeActionAt(ctx context.Context, db *Store, runID string, guest GuestCommandRunner,
	workRoot string, alreadyClaimed bool) error {
	if guest == nil { return errors.New("guest runner unavailable") }
	if !strings.HasPrefix(workRoot,"/") || workRoot=="/" ||
		strings.ContainsAny(workRoot,"\\\x00\r\n") || strings.Contains(workRoot,"/../") ||
		strings.HasSuffix(workRoot,"/..") {
		return errors.New("INVALID_ARGUMENT: unsafe InsulaVM guest workspace")
	}
	a, err := db.ReadAction(ctx, runID)
	if err != nil { return err }
	if alreadyClaimed {
		if a.Status!="running" {return errors.New("CONFLICT: action not claimed")}
	}else{
		if a.Status!="queued" {return errors.New("CONFLICT: action is not queued")}
		if err:=db.StartAction(ctx,runID);err!=nil{return err}
	}
	results := map[string]string{}
	waiting := map[string]ActionJobResult{}
	for _, job := range a.Jobs {
		if job.Status == "skipped" {
			results[job.Name] = "skipped"
			continue
		}
		waiting[job.Name] = job
	}
	anyFailure := false
	var firstError error
	for len(waiting) > 0 {
		progress := false
		for _, job := range a.Jobs {
			j, ok := waiting[job.Name]
			if !ok { continue }
			ready, blocked := true, false
			for _, dep := range j.Needs {
				state, ok := results[dep]
				if !ok { ready = false; break }
				if state != "succeeded" { blocked = true }
			}
			if !ready { continue }
			progress = true
			delete(waiting, j.Name)
			if blocked {
				if err := db.SkipQueuedCommands(ctx, j.ID); err != nil { return err }
				if err := db.FinishJob(ctx, j.ID, "skipped", nil); err != nil { return err }
				results[j.Name] = "skipped"
				anyFailure = true
				continue
			}
			if err := db.StartJob(ctx, j.ID); err != nil { return err }
			jobFailed := false
			jobExit := 0
			for _, step := range j.Commands {
				script, err := db.ActionCommandScript(ctx, step.CommandID)
				if err != nil { firstError = err; jobFailed = true; break }
				logID, err := newUUIDv7()
				if err != nil { firstError = err; jobFailed = true; break }
				if err = db.CreateExecutionWithType(ctx, logID, "gitm_action_cmd", a.Scope, a.OwnerUID, "action"); err != nil {
					firstError = err; jobFailed = true; break
				}
				if err = db.StartCommand(ctx, step.CommandID, logID); err != nil {
					firstError = err; jobFailed = true; break
				}
				// Every VM response also persists in action_command_responses,
				// independently from the temporary detalhes_execucao log.
				if err = db.AppendCommandResponse(ctx, step.CommandID, "system",
					"gitmaster: iniciando job="+j.Name+" etapa="+step.StepName); err != nil {
					firstError = err
					jobFailed = true
					jobExit = -1
					_ = completeCommand(db, logID, step.CommandID, -1, err)
					break
				}
				// An individual temporary log is retrievable with detalhes_execucao(logID).
				if err = db.AppendActionMessages(ctx, logID, []string{"gitmaster: iniciando job="+j.Name+" etapa="+step.StepName}); err != nil {
					firstError = err
					jobFailed = true
					jobExit = -1
					_ = completeCommand(db, logID, step.CommandID, -1, err)
					break
				}
				dir, err := preparedWorkingDirectory(workRoot, step.WorkingDir)
				if err != nil {
					firstError = err
					jobFailed = true
					jobExit = -1
					if e := completeCommand(db, logID, step.CommandID, -1, err); e != nil { firstError = e }
					break
				}
				cmdCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
				outputErr := error(nil)
				exit, execErr := guest.Execute(cmdCtx, dir, script, func(stream, line string) error {
					if outputErr != nil { return outputErr }
					clean := redact(line, "")
					if stream != "stdout" && stream != "stderr" { stream = "system" }
					// Persist every fragment, including empty lines and text longer
					// than the short diagnostic limit in tool_execution_messages.
					outputErr = db.AppendCommandResponse(cmdCtx, step.CommandID, stream, clean)
					if outputErr != nil { return outputErr }
					short := redactShort(clean, "")
					if short == "" { return nil }
					outputErr = db.AppendActionMessages(cmdCtx, logID, []string{stream+": "+short})
					return outputErr
				})
				cancel()
				if outputErr != nil && execErr == nil { execErr = outputErr }
				if execErr != nil || exit != 0 {
					if execErr == nil { execErr = fmt.Errorf("guest command exited with code %d", exit) }
					if firstError == nil { firstError = execErr }
					jobFailed = true
					jobExit = exit
				}
				if err := completeCommand(db, logID, step.CommandID, exit, execErr); err != nil {
					if firstError == nil { firstError = err }
					jobFailed = true
					if jobExit == 0 { jobExit = -1 }
				}
				if jobFailed { break }
			}
			jobStatus := "succeeded"
			if jobFailed {
				jobStatus = "failed"
				anyFailure = true
				if err := db.SkipQueuedCommands(ctx, j.ID); err != nil { return err }
			}
			if jobFailed && jobExit == 0 { jobExit = -1 }
			if err := db.FinishJob(ctx, j.ID, jobStatus, &jobExit); err != nil { return err }
			results[j.Name] = jobStatus
		}
		if !progress {
			anyFailure = true
			if firstError == nil { firstError = errors.New("INVALID_ARGUMENT: cyclic job dependencies") }
			for _, job := range waiting {
				_ = db.SkipQueuedCommands(ctx, job.ID)
				_ = db.FinishJob(ctx, job.ID, "skipped", nil)
			}
			break
		}
	}
	status := "succeeded"
	if anyFailure { status = "failed" }
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := db.FinishAction(persist, runID, status); err != nil { return err }
	if anyFailure && firstError == nil { return errors.New("COMMAND_FAILED: action finished with failed or skipped jobs") }
	return firstError
}

func completeCommand(db *Store, logID, commandID string, exit int, cmdErr error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, logStatus, code := "succeeded", "completed", ""
	if cmdErr != nil || exit != 0 {
		status, logStatus, code = "failed", "failed", "COMMAND_FAILED"
	}
	var outputErr error
	if cmdErr != nil {
		errorText := redact(cmdErr.Error(), "")
		outputErr = db.AppendCommandResponse(ctx, commandID, "error", errorText)
	}
	summary := fmt.Sprintf("gitmaster: exit_code=%d status=%s", exit, status)
	if outputErr == nil {
		outputErr = db.AppendCommandResponse(ctx, commandID, "system", summary)
	}
	messages := []string{summary}
	if cmdErr != nil { messages = append([]string{"error: " + redactShort(cmdErr.Error(), "")}, messages...) }
	appendErr := db.AppendActionMessages(ctx, logID, messages)
	finishErr := db.FinishExecution(ctx, logID, logStatus, code, nil)
	commandErr := db.FinishCommand(ctx, commandID, status, &exit)
	return errors.Join(outputErr, appendErr, finishErr, commandErr)
}

// preparedWorkingDirectory validates a step relative to the InsulaVM-managed
// workspace. The worker must have already created this folder in DeployToVM.
func preparedWorkingDirectory(root,relative string)(string,error){
	if !strings.HasPrefix(root,"/")||root=="/"||strings.ContainsAny(root,"\\\x00\r\n"){
		return "",errors.New("INVALID_ARGUMENT: unsafe root")
	}
	if relative==""||relative=="."{return root,nil}
	if strings.HasPrefix(relative,"/")||strings.ContainsAny(relative,"\\\x00\r\n"){
		return "",errors.New("INVALID_ARGUMENT: unsafe guest workdir")
	}
	for _,piece:=range strings.Split(relative,"/"){
		if piece==""||piece=="."||piece==".."{return "",errors.New("INVALID_ARGUMENT: unsafe guest workdir")}
	}
	return strings.TrimSuffix(root,"/")+"/"+relative,nil
}

// guestWorkingDirectory validates and normalizes the path on the guest. It
// must not accept absolute paths, empty components or parent traversal.
func guestWorkingDirectory(cloneID, relative string) (string, error) {
	if !isUUIDv7(cloneID) { return "", errors.New("INVALID_ARGUMENT: unsafe clone identifier") }
	root := "/tmp/" + cloneID
	if relative == "" || relative == "." { return root, nil }
	if strings.HasPrefix(relative, "/") || strings.ContainsAny(relative, "\\\x00\r\n") {
		return "", errors.New("INVALID_ARGUMENT: unsafe job workdir")
	}
	for _, part := range strings.Split(relative, "/") {
		if part == "." || part == ".." || part == "" { return "", errors.New("INVALID_ARGUMENT: unsafe job workdir") }
	}
	return root+"/"+relative, nil
}

func isUUIDv7(value string) bool {
	if len(value)!=36 || value[14]!='7' || value[8]!='-' || value[13]!='-' || value[18]!='-' || value[23]!='-' { return false }
	for i,ch := range value {
		if i==8 || i==13 || i==18 || i==23 {continue}
		if !(ch>='0' && ch<='9') && !(ch>='a'&&ch<='f') && !(ch>='A'&&ch<='F') {return false}
	}
	return true
}

// GuestWorkingDirectory is exported for consumers that validate untrusted
// workflow working-directory paths before dispatching guest commands.
func GuestWorkingDirectory(cloneID,relative string)(string,error){
    return guestWorkingDirectory(cloneID,relative)
}
