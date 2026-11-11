package gitmasteractions

import (
    "context"
    "crypto/rand"
    "database/sql"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
)

type Options struct {
    DatabasePath string
    InsulaConfigPath string
    StartupScripts map[string]string
}
type Engine struct {
    db *sql.DB
    options Options
    mu sync.Mutex
    active map[string]context.CancelFunc
    closed bool
}
func New(options Options)(*Engine,error){
    db,err:=openStore(options.DatabasePath)
    if err!=nil{return nil,err}
    return &Engine{db:db,options:options,active:map[string]context.CancelFunc{}},nil
}
func(e *Engine) Close()error{
    e.mu.Lock()
    defer e.mu.Unlock()
    if len(e.active)>0{return errors.New("cannot close Engine while actions are running")}
    if e.closed{return nil}
    e.closed=true
    return e.db.Close()
}
func(e *Engine) assertOpen()error{
    e.mu.Lock()
    defer e.mu.Unlock()
    if e.closed{return errors.New("GitMaster Actions Engine is closed")}
    return nil
}
func uuidv7()(string,error){
    var b [16]byte
    if _,err:=rand.Read(b[:]);err!=nil{return "",err}
    ms:=uint64(time.Now().UnixMilli())
    for i:=5;i>=0;i--{b[i]=byte(ms);ms>>=8}
    b[6]=(b[6]&0x0f)|0x70
    b[8]=(b[8]&0x3f)|0x80
    return fmt.Sprintf("%x-%x-%x-%x-%x",b[:4],b[4:6],b[6:8],b[8:10],b[10:]),nil
}
type Action struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Status string `json:"status"`
    CreatedAt string `json:"created_at"`
    StartedAt *string `json:"started_at,omitempty"`
    FinishedAt *string `json:"finished_at,omitempty"`
    ErrorMessage *string `json:"error_message,omitempty"`
    VMID string `json:"vm_id,omitempty"`
    engine *Engine
}
type Job struct {
    ID string `json:"id"`
    ActionID string `json:"action_id"`
    Name string `json:"name"`
    Position int `json:"position"`
    AllowFailure bool `json:"allow_failure"`
    Status string `json:"status"`
    StartedAt *string `json:"started_at,omitempty"`
    FinishedAt *string `json:"finished_at,omitempty"`
    engine *Engine
}
type Command struct {
    ID string `json:"id"`
    JobID string `json:"job_id"`
    Position int `json:"position"`
    Text string `json:"command_text"`
    Status string `json:"status"`
    ExitCode *int `json:"exit_code,omitempty"`
    StartedAt *string `json:"started_at,omitempty"`
    FinishedAt *string `json:"finished_at,omitempty"`
    engine *Engine
}
type CommandLog struct{
    LineNumber uint64 `json:"line_number"`
    StreamType string `json:"stream_type"`
    Payload []byte `json:"payload"`
    CapturedAt time.Time `json:"captured_at"`
}
type CommandOutput struct{
    ID int64 `json:"id"`
    SequenceNumber int64 `json:"sequence_no"`
    StreamType string `json:"stream_type"`
    Payload []byte `json:"payload"`
    CapturedAt time.Time `json:"captured_at"`
}
type ActionFilter struct{Status string}
type Page struct{Limit int; AfterID int64}
type OutputFilter struct{StreamType string}

func(e *Engine) RunFile(ctx context.Context,path string)(*Action,error){
    data,err:=os.ReadFile(path)
    if err!=nil{return nil,err}
    abs,err:=filepath.Abs(path)
    if err!=nil{return nil,err}
    return e.run(ctx,data,filepath.Dir(abs))
}
func(e *Engine) RunYAML(ctx context.Context,yaml []byte)(*Action,error){
    return e.run(ctx,yaml,"")
}
func(e *Engine) run(ctx context.Context,data []byte,baseDir string)(*Action,error){
    if err:=e.assertOpen();err!=nil{return nil,err}
    m,err:=parseManifest(data,baseDir)
    if err!=nil{return nil,err}
    if e.options.InsulaConfigPath==""{
        return nil,errors.New("InsulaConfigPath required to execute actions")
    }
    runtime,err:=NewRuntime(e.options.InsulaConfigPath,e.options.StartupScripts)
    if err!=nil{return nil,err}
    if err=runtime.Profile(m.VM.Profile);err!=nil{return nil,fmt.Errorf("InsulaVM profile: %w",err)}
    if path:=runtime.Workspace();!strings.HasPrefix(path,"/")||path=="/"{
        return nil,errors.New("InsulaVM guest.destination must be an absolute non-root directory")
    }
    id,err:=uuidv7()
    if err!=nil{return nil,err}
    tx,err:=e.db.BeginTx(ctx,nil)
    if err!=nil{return nil,err}
    defer tx.Rollback()
    if _,err=tx.ExecContext(ctx,`INSERT INTO actions(id,name,status,created_at)VALUES(?,?,?,?)`,
        id,m.Action.Name,"pending",timestamp());err!=nil{return nil,err}
    jobs:=make([]string,0,len(m.Action.Jobs))
    commands:=make([][]string,0,len(m.Action.Jobs))
    for j,job:=range m.Action.Jobs {
        jid,err:=uuidv7()
        if err!=nil{return nil,err}
        jobs=append(jobs,jid)
        if _,err=tx.ExecContext(ctx,`INSERT INTO jobs(id,action_id,name,position,allow_failure,status)
            VALUES(?,?,?,?,?,?)`,jid,id,job.Name,j,job.AllowFailure,"pending");err!=nil{return nil,err}
        cmdIDs:=make([]string,0,len(job.Commands))
        for k,cmd:=range job.Commands{
            cid,err:=uuidv7()
            if err!=nil{return nil,err}
            if _,err=tx.ExecContext(ctx,`INSERT INTO commands(id,job_id,position,command_text,status)
                VALUES(?,?,?,?,?)`,cid,jid,k,cmd,"pending");err!=nil{return nil,err}
            cmdIDs=append(cmdIDs,cid)
        }
        commands=append(commands,cmdIDs)
    }
    if err=tx.Commit();err!=nil{return nil,err}
    runCtx,cancel:=context.WithCancel(ctx)
    e.mu.Lock()
    if e.closed {e.mu.Unlock();cancel();return nil,errors.New("Engine closed while scheduling")}
    e.active[id]=cancel
    e.mu.Unlock()
    defer func(){
        cancel()
        e.mu.Lock()
        delete(e.active,id)
        e.mu.Unlock()
    }()
    if err=e.setStatus(runCtx,"actions",id,"starting","");err!=nil{return nil,err}
    vmID:=""
    runErr:=func()error{
        var err error
        vmID,err=runtime.Start(runCtx,m.VM.Profile,nil)
        if err!=nil{return fmt.Errorf("start VM: %w",err)}
        if err=e.setStatus(runCtx,"actions",id,"running","");err!=nil{return err}
        for _,item:=range m.Action.Imports {
            // Deploy is bound to an explicit, already-validated local archive.
            if err=runtime.Deploy(runCtx,vmID,item.Source,"",nil);err!=nil{
                return fmt.Errorf("deploy %q: %w",item.Source,err)
            }
        }
        for j,job:=range m.Action.Jobs{
            if err=e.setStatus(runCtx,"jobs",jobs[j],"running","");err!=nil{return err}
            failed:=false
            for k,cmd:=range job.Commands {
                commandID:=commands[j][k]
                if err=e.setStatus(runCtx,"commands",commandID,"running","");err!=nil{return err}
                writer:=NewOutputWriter(func(stream,line string)error{
                    if stream=="system" {stream="stdout"}
                    // ExecVM provides decorated text, not native byte-exact streams.
                    return e.appendOutput(runCtx,commandID,stream,[]byte(line+"\n"))
                })
                exit,commandErr:=runtime.Command(runCtx,vmID,runtime.Workspace(),cmd,writer)
                commandErr=errors.Join(commandErr,writer.Flush())
                var code any
                if exit>=0 {code=exit}
                var kind any
                if commandErr!=nil {
                    if exit>=0{kind="remote_exit"}else{kind="infrastructure"}
                }
                _,dbErr:=e.db.ExecContext(context.Background(),
                    `UPDATE commands SET exit_code=?,failure_kind=? WHERE id=?`,
                    code,kind,commandID)
                if dbErr!=nil{return dbErr}
                if commandErr!=nil {
                    failed=true
                    if err=e.setStatus(context.Background(),"commands",commandID,"failed",commandErr.Error());err!=nil{return err}
                    if !job.AllowFailure {
                        _ = e.setStatus(context.Background(),"jobs",jobs[j],"failed",commandErr.Error())
                        return fmt.Errorf("job %q command %d: %w",job.Name,k,commandErr)
                    }
                }else{
                    if err=e.setStatus(runCtx,"commands",commandID,"succeeded","");err!=nil{return err}
                }
            }
            if failed&&job.AllowFailure {
                // Tolerated remote exit failures are still visible on commands.
                if err=e.setStatus(runCtx,"jobs",jobs[j],"succeeded","allowed command failure");err!=nil{return err}
            }else{
                if err=e.setStatus(runCtx,"jobs",jobs[j],"succeeded","");err!=nil{return err}
            }
        }
        return nil
    }()
    if vmID!="" {
        cleanupCtx,stop:=context.WithTimeout(context.Background(),60*time.Second)
        stopErr:=runtime.Stop(cleanupCtx,vmID)
        stop()
        runErr=errors.Join(runErr,stopErr)
    }
    status:="succeeded"
    if runErr!=nil{status="failed"}
    if errors.Is(runCtx.Err(),context.Canceled)||errors.Is(runCtx.Err(),context.DeadlineExceeded){
        status="cancelled"
        if runErr==nil{runErr=runCtx.Err()}
    }
    finalCtx,cancelFinal:=context.WithTimeout(context.Background(),10*time.Second)
    defer cancelFinal()
    message:=""
    if runErr!=nil{message=runErr.Error()}
    finishErr:=e.setStatus(finalCtx,"actions",id,status,message)
    runErr=errors.Join(runErr,finishErr)
    action,readErr:=e.GetAction(finalCtx,id)
    if action!=nil{action.VMID=vmID}
    return action,errors.Join(runErr,readErr)
}
func(e *Engine) CancelAction(ctx context.Context,id string)error{
    if err:=e.assertOpen();err!=nil{return err}
    if err:=ctx.Err();err!=nil{return err}
    e.mu.Lock()
    cancel,ok:=e.active[id]
    e.mu.Unlock()
    if !ok{return errors.New("action is not running on this Engine")}
    cancel()
    return nil
}
