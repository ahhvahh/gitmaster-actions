package legacy

import (
    "context"
    "errors"
    "fmt"
    "io"
    "log"
    "os"
    "path/filepath"
    "strings"
    "time"

    actions "github.com/ahhvahh/gitmaster-actions"
)

// ArchiveProvider is a Git-boundary callback. The consumer supplies a TAR.GZ
// snapshot of the immutable commit SHA; this package never reads Git refs.
// The cleanup callback owns the temporary archive and is always invoked.
type ArchiveProvider func(ctx context.Context, action ActionResult)(archivePath string,cleanup func(),err error)

type guestCommand struct{
    vmID string
    backend actions.Runtime
    console io.Writer
}
func(c guestCommand)Execute(ctx context.Context,dir,script string,emit func(string,string)error)(int,error){
    cmd:=actions.NewGuestCommand(c.backend,c.vmID,nil)
    return cmd.Execute(ctx,dir,script,func(stream,line string)error{
        if c.console!=nil{
            if _,err:=fmt.Fprintf(c.console,"[InsulaVM][%s] %s\n",stream,redactShort(line,""));err!=nil{return err}
        }
        return emit(stream,line)
    })
}

// RunActionViaInsula owns one VM from start to Stop, including failed imports,
// jobs, canceled contexts and incomplete cleanup. It preserves the original
// GitMaster SQLite event linkage until old records have been migrated.
func RunActionViaInsula(ctx context.Context,db *Store,actionID string,
    backend actions.Runtime,archive ArchiveProvider,output io.Writer)(retErr error){
    if backend==nil||archive==nil{return errors.New("InsulaVM runtime and archive provider required")}
    a,err:=db.ReadAction(ctx,actionID)
    if err!=nil{return err}
    if a.Status!="queued"{return errors.New("CONFLICT: action not queued")}
    if err=db.StartAction(ctx,actionID);err!=nil{return err}
    completed:=false
    stage:=func(kind,message string){
        clean:=redactShort(message,"")
        if err:=db.AddActionRuntimeMessage(ctx,actionID,kind,clean);err!=nil{
            log.Printf("level=ERROR event=action_runtime_log action=%s error=%q",actionID,err)
        }
        if output!=nil {_,_=fmt.Fprintf(output,"[GitMaster Actions][%s] %s\n",kind,clean)}
    }
    stage("queued","Action claimed; preparing immutable snapshot")
    defer func(){
        if !completed{
            cleanupCtx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
            defer cancel()
            if retErr!=nil{
                _=db.AddActionRuntimeMessage(cleanupCtx,actionID,"error",redactShort(retErr.Error(),""))
            }
            retErr=errors.Join(retErr,db.FailManualAction(cleanupCtx,actionID))
        }
    }()
    stage("profile","Validating InsulaVM profile "+a.VMProfile)
    if err=backend.Profile(a.VMProfile);err!=nil{return err}
    if !strings.HasPrefix(backend.Workspace(),"/")||backend.Workspace()=="/"{
        return errors.New("unsafe InsulaVM guest.destination")
    }
    archivePath,removeArchive,err:=archive(ctx,a)
    if err!=nil{return fmt.Errorf("prepare immutable archive: %w",err)}
    if removeArchive!=nil{defer removeArchive()}
    if archivePath==""||!strings.HasSuffix(strings.ToLower(archivePath),".tar.gz"){
        return errors.New("archive provider must return a .tar.gz path")
    }
    info,err:=os.Lstat(archivePath)
    if err!=nil{return err}
    if !info.Mode().IsRegular(){return errors.New("archive provider returned unsafe file")}
    stage("archive","TAR.GZ prepared from selected snapshot")
    scratch,err:=os.MkdirTemp("","gitmaster-actions-deploy-")
    if err!=nil{return err}
    defer os.RemoveAll(scratch)
    deployScript:=filepath.Join(scratch,"deploy.sh")
    const script="#!/bin/bash\nset -euo pipefail\numask 077\ntest -d .\nprintf 'GitMaster Actions: material imported\\n'\n"
    if err=os.WriteFile(deployScript,[]byte(script),0600);err!=nil{return err}

    stage("start","Starting VM with administrator-controlled bootstrap")
    vmID,err:=backend.Start(ctx,a.VMProfile,func(kind,message string){
        stage("startup_"+kind,message)
    })
    if err!=nil{return fmt.Errorf("InsulaVM start: %w",err)}
    if err=db.RecordInsulaVM(ctx,actionID,vmID);err!=nil{
        stopCtx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
        stopErr:=backend.Stop(stopCtx,vmID)
        cancel()
        return errors.Join(err,stopErr)
    }
    stage("start","VM started by InsulaVM: "+vmID)
    defer func(){
        stopCtx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
        defer cancel()
        if err:=backend.Stop(stopCtx,vmID);err!=nil{
            retErr=errors.Join(retErr,fmt.Errorf("InsulaVM cleanup: %w",err))
            _=db.AddActionRuntimeMessage(stopCtx,actionID,"error",redactShort(err.Error(),""))
            _=db.MarkActionCleanupFailed(stopCtx,actionID)
        }else{
            _=db.AddActionRuntimeMessage(stopCtx,actionID,"stop","VM discarded by InsulaVM: "+vmID)
        }
    }()
    stage("deploy","Uploading and extracting TAR.GZ through InsulaVM")
    out:=actions.NewOutputWriter(func(stream,line string)error{
        if err:=db.AddActionRuntimeMessage(ctx,actionID,"deploy_"+stream,redactShort(line,""));err!=nil{return err}
        if output!=nil{
            _,err:=fmt.Fprintf(output,"[InsulaVM][deploy:%s] %s\n",stream,redactShort(line,""))
            if err!=nil{return err}
        }
        return nil
    })
    err=backend.Deploy(ctx,vmID,archivePath,deployScript,out)
    err=errors.Join(err,out.Flush())
    if err!=nil{return fmt.Errorf("InsulaVM deploy: %w",err)}
    stage("jobs","Executing versioned workflow in the disposable VM")
    err=ExecuteClaimedActionAt(ctx,db,actionID,guestCommand{backend:backend,vmID:vmID,console:output},backend.Workspace())
    if err!=nil{return err}
    stage("jobs","Jobs finished; awaiting VM teardown")
    completed=true
    return nil
}

// StartInsulaWorker is controlled by the GitMaster daemon context. This Go
// library itself starts no independent daemon or network listener.
func StartInsulaWorker(ctx context.Context,db *Store,backend actions.Runtime,archive ArchiveProvider){
    if backend==nil||archive==nil{return}
    tick:=time.NewTicker(2*time.Second)
    defer tick.Stop()
    for {
        if ctx.Err()!=nil{return}
        ids,err:=db.ListQueuedTriggeredActions(ctx,1)
        if err!=nil{
            log.Printf("level=ERROR event=actions_queue error=%q",redactShort(err.Error(),""))
        }else{
            for _,id:=range ids{
                if err:=RunActionViaInsula(ctx,db,id,backend,archive,nil);err!=nil{
                    log.Printf("level=ERROR event=action_failed id=%s error=%q",id,redactShort(err.Error(),""))
                }
            }
        }
        select{case <-ctx.Done():return;case <-tick.C:}
    }
}
func ReconcileInsulaVMs(ctx context.Context,db *Store,backend actions.Runtime)error{
    orphans,err:=db.InterruptedInsulaVMs(ctx)
    if err!=nil{return err}
    for _,item:=range orphans {
        stopCtx,cancel:=context.WithTimeout(ctx,60*time.Second)
        err:=backend.Stop(stopCtx,item.VMID)
        if err==nil{
            _=db.AddActionRuntimeMessage(stopCtx,item.ActionID,"recovery",
                "InsulaVM confirmed cleanup of interrupted VM "+item.VMID)
        }else{
            _=db.AddActionRuntimeMessage(stopCtx,item.ActionID,"recovery",redactShort(err.Error(),""))
            log.Printf("level=ERROR event=action_recovery action=%s vm=%s error=%q",
                item.ActionID,item.VMID,redactShort(err.Error(),""))
        }
        cancel()
    }
    return nil
}
