package gitmasteractions

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"
)

// scheduledJobResult is transferred between workers and the scheduler without
// sharing mutable job maps. Only the scheduler changes dependency states.
type scheduledJobResult struct {
    name string
    status string
    infrastructure error
}

// executePlannedJobs starts independent jobs concurrently up to MaxParallelJobs.
// Each job keeps its own command sequence. One shared VM command token enforces
// InsulaVM's one-ExecVM-per-VM contract even when many Jobs are eligible.
func (e *Engine) executePlannedJobs(ctx context.Context, actionID string, plan executionPlan, rt Runtime, vmID string) error {
    persisted,err:=e.ListJobs(ctx,actionID)
    if err!=nil{return err}
    if len(persisted)!=len(plan.Jobs){return errors.New("persisted jobs differ from execution plan")}
    parallel:=plan.MaxParallelJobs
    if parallel<1{parallel=1}
    if parallel>16{return errors.New("max_parallel_jobs exceeds 16")}
    runCtx,cancel:=context.WithCancel(ctx)
    defer cancel()

    done:=make(chan scheduledJobResult,len(plan.Jobs))
    commandGate:=make(chan struct{},1)
    states:=make(map[string]string,len(plan.Jobs))
    jobByName:=make(map[string]string,len(plan.Jobs))
    for i,j:=range plan.Jobs {
        jobByName[j.Name]=persisted[i].ID
        if j.SkipReason!=""{states[j.Name]="skipped"}
    }
    remaining:=len(plan.Jobs)-len(states)
    running:=0
    failed:=false
    var fatal error

    for remaining>0 || running>0 {
        // Start all ready jobs up to the configured concurrency limit.
        // Dependency-skipped jobs are resolved immediately without using a slot.
        if fatal==nil && runCtx.Err()==nil {
            for i,j:=range plan.Jobs {
                if states[j.Name]!=""{continue}
                blocked:=""
                ready:=true
                for _,need:=range j.Needs {
                    state:=states[need]
                    if state==""||state=="running"{ready=false;break}
                    if state!="succeeded"&&blocked==""{blocked=need}
                }
                if !ready{continue}
                if blocked!=""{
                    reason:="dependency_not_succeeded:"+blocked
                    if err:=e.skipDependentJob(runCtx,jobByName[j.Name],reason);err!=nil{
                        fatal=fmt.Errorf("skip job %s: %w",j.Name,err)
                        cancel()
                        break
                    }
                    states[j.Name]="skipped"
                    remaining--
                    continue
                }
                if running>=parallel{continue}
                states[j.Name]="running"
                running++
                index:=i
                go func(){
                    name:=plan.Jobs[index].Name
                    status,err:=e.runPlannedJob(runCtx,actionID,persisted[index].ID,plan.Jobs[index],vmID,rt,commandGate)
                    done<-scheduledJobResult{name:name,status:status,infrastructure:err}
                }()
            }
        }
        if running==0{
            if fatal!=nil{return fatal}
            if ctx.Err()!=nil{return ctx.Err()}
            if remaining==0{break}
            return errors.New("job dependency scheduling could not make progress")
        }
        // Drain every started worker before returning: VM cleanup MUST NOT race
        // with still-running commands or persistent status writes.
        select{
        case result:=<-done:
            running--
            remaining--
            states[result.name]=result.status
            if result.status=="failed"{failed=true}
            if result.infrastructure!=nil&&fatal==nil{
                fatal=fmt.Errorf("job %s: %w",result.name,result.infrastructure)
                cancel()
            }
        case <-ctx.Done():
            cancel()
            for running>0{
                result:=<-done
                running--
                remaining--
                states[result.name]=result.status
            }
            return ctx.Err()
        }
    }
    if fatal!=nil{return fatal}
    if ctx.Err()!=nil{return ctx.Err()}
    if failed{return errors.New("action has failed jobs")}
    // Skips due to branch filters or unmet dependencies are not command failures.
    return nil
}

func (e *Engine) skipDependentJob(ctx context.Context, jobID, reason string) error {
    if _,err:=e.db.ExecContext(ctx,`UPDATE jobs SET skip_reason=? WHERE id=? AND status='pending'`,reason,jobID);err!=nil{return err}
    if err:=e.setStatus(ctx,"jobs",jobID,"skipped",reason);err!=nil{return err}
    return e.skipCommands(ctx,jobID,"skipped")
}

func (e *Engine) runPlannedJob(ctx context.Context, actionID, jobID string, j plannedJob,
    vmID string, rt Runtime, commandGate chan struct{}) (string,error) {
    if err:=ctx.Err();err!=nil{return "failed",err}
    if err:=e.setStatus(ctx,"jobs",jobID,"running","");err!=nil{return "failed",err}
    commands,err:=e.ListCommands(ctx,jobID)
    if err!=nil{return "failed",err}
    if len(commands)!=len(j.Commands){return "failed",errors.New("persisted commands differ from plan")}
    jobCtx:=ctx
    cancel:=func(){}
    if j.TimeoutSeconds>0{jobCtx,cancel=context.WithTimeout(ctx,time.Duration(j.TimeoutSeconds)*time.Second)}
    defer cancel()
    jobFailed:=false
    infrastructure:=false
    for k,c:=range j.Commands {
        if err:=jobCtx.Err();err!=nil{jobFailed=true;infrastructure=true;break}
        commandID:=commands[k].ID
        if err:=e.setStatus(jobCtx,"commands",commandID,"running","");err!=nil{return "failed",err}
        dir:=rt.Workspace()
        if c.Directory!=""&&c.Directory!="."{dir=strings.TrimSuffix(dir,"/")+"/"+c.Directory}
        writer:=NewOutputWriter(func(stream,line string)error{
            if stream=="system"{stream="stdout"}
            if len(line)>4<<20{return errors.New("command line exceeds 4 MiB capture limit")}
            persistCtx,done:=context.WithTimeout(context.Background(),10*time.Second)
            defer done()
            return e.appendOutput(persistCtx,commandID,stream,[]byte(redactActionText(line)+"\n"))
        })
        // The scheduler can run many Jobs, but InsulaVM serializes SSH ExecVM
        // on this specific VM. A VM outside this Action may still cause BUSY.
        var exit int
        var runErr error
        select{
        case commandGate<-struct{}{}:
            exit,runErr=runWithBusyRetry(jobCtx,func()(int,error){
                return rt.Command(jobCtx,vmID,dir,c.Run,writer)
            },func(attempt int,delay time.Duration){
                persistCtx,done:=context.WithTimeout(context.Background(),5*time.Second)
                defer done()
                _=e.addEvent(persistCtx,actionID,"vm_busy_retry",fmt.Sprintf("job=%s step=%d retry=%d delay_ms=%d",j.Name,k,attempt,delay.Milliseconds()))
            },randomBusyDelay)
            <-commandGate
        case <-jobCtx.Done():
            exit=-1
            runErr=jobCtx.Err()
        }
        runErr=errors.Join(runErr,writer.Flush())
        var exitValue any
        if exit>=0{exitValue=exit}
        failure:=""
        if runErr!=nil||exit!=0{
            failure="remote_exit"
            if exit<0||errors.Is(runErr,context.Canceled)||errors.Is(runErr,context.DeadlineExceeded){
                failure="infrastructure"
            }
            if runErr==nil{runErr=fmt.Errorf("remote command exit code %d",exit)}
        }
        detail:=""
        if runErr!=nil{detail=redactActionText(runErr.Error())}
        saveCtx,done:=context.WithTimeout(context.Background(),10*time.Second)
        _,saveErr:=e.db.ExecContext(saveCtx,`UPDATE commands SET exit_code=?,failure_kind=? WHERE id=?`,exitValue,nullableString(failure),commandID)
        if saveErr==nil {
            status:="succeeded"
            if failure!=""{status="failed"}
            saveErr=e.setStatus(saveCtx,"commands",commandID,status,detail)
        }
        if saveErr==nil {
            status:="succeeded"
            if failure!=""{status="failed"}
            saveErr=e.addEvent(saveCtx,actionID,"command",fmt.Sprintf("job=%s step=%d status=%s exit=%d",j.Name,k,status,exit))
        }
        done()
        if saveErr!=nil{return "failed",fmt.Errorf("persist command result: %w",saveErr)}
        if failure!=""{
            if failure=="infrastructure"{infrastructure=true}
            if !j.AllowFailure||infrastructure{jobFailed=true;break}
        }
    }
    if jobCtx.Err()!=nil{jobFailed=true;infrastructure=true}
    state:="succeeded"
    detail:=""
    if jobFailed{state="failed";detail="job command failed"}
    saveCtx,done:=context.WithTimeout(context.Background(),10*time.Second)
    err=e.setStatus(saveCtx,"jobs",jobID,state,detail)
    if err==nil&&jobFailed{err=e.skipCommands(saveCtx,jobID,"skipped")}
    done()
    if err!=nil{return "failed",err}
    if infrastructure{
        if ctx.Err()!=nil{return state,ctx.Err()}
        if jobCtx.Err()!=nil{return state,fmt.Errorf("job timeout: %v",jobCtx.Err())}
        return state,errors.New("VM command infrastructure failure")
    }
    return state,nil
}

