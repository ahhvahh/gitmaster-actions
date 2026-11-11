package gitmasteractions

import (
    "context"
    "crypto/rand"
    "database/sql"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sync"
    "syscall"
    "time"

    "gopkg.in/yaml.v3"
)

type Options struct {
    DatabasePath string
    InsulaConfigPath string
    StartupScripts map[string]string
    ImagesDir string
    Workers int
    RuntimeFactory func() (Runtime,error)
}
type Engine struct {
    db *sql.DB
    options Options
    mu sync.Mutex
    active map[string]context.CancelFunc
    lockFile *os.File
    asyncSlots chan struct{}
    closed bool
}
// New exclusively owns an actions.db file for the life of this Engine.
// A nonblocking advisory lock prevents a second process from marking live
// actions as interrupted during startup recovery.
func New(options Options)(*Engine,error){
    if options.DatabasePath=="" {return nil,errors.New("DatabasePath is required")}
    if options.Workers<0||options.Workers>32{return nil,errors.New("Workers must be 0..32")}
    workers:=options.Workers
    if workers==0{workers=2}
    lock,err:=os.OpenFile(options.DatabasePath+".lock",os.O_CREATE|os.O_RDWR,0600)
    if err!=nil{return nil,fmt.Errorf("open actions lock: %w",err)}
    if err=syscall.Flock(int(lock.Fd()),syscall.LOCK_EX|syscall.LOCK_NB);err!=nil{
        _=lock.Close()
        return nil,fmt.Errorf("actions database is already owned by another Engine: %w",err)
    }
    release:=func(){
        _=syscall.Flock(int(lock.Fd()),syscall.LOCK_UN)
        _=lock.Close()
    }
    db,err:=openStore(options.DatabasePath)
    if err!=nil{release();return nil,err}
    if err=ensureServiceSchema(db);err!=nil{_ = db.Close();release();return nil,err}
    if err=ensureLegacyArchive(db);err!=nil{_ = db.Close();release();return nil,err}
    engine:=&Engine{db:db,lockFile:lock,options:options,active:map[string]context.CancelFunc{},asyncSlots:make(chan struct{},workers)}
    if err=engine.recoverInterrupted(context.Background());err!=nil{_ = db.Close();release();return nil,err}
    return engine,nil
}
func(e *Engine) Close()error{
    e.mu.Lock()
    defer e.mu.Unlock()
    if len(e.active)>0{return errors.New("cannot close Engine while actions are running")}
    if e.closed{return nil}
    e.closed=true
    closeErr:=e.db.Close()
    unlockErr:=syscall.Flock(int(e.lockFile.Fd()),syscall.LOCK_UN)
    return errors.Join(closeErr,unlockErr,e.lockFile.Close())
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
    CursorID int64 `json:"cursor_id"`
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
type ActionFilter struct{Status string;CloneID string}
type Page struct{Limit int; AfterID int64}
type OutputFilter struct{StreamType string}

// RunFile uses the same durable executor as SubmitYAML. Relative imports are
// resolved against the YAML file before submitting its normalized manifest.
func (e *Engine) RunFile(ctx context.Context, filename string) (*Action, error) {
    info, err := os.Lstat(filename)
    if err != nil { return nil, err }
    if !info.Mode().IsRegular() || info.Size() > 1<<20 {
        return nil, errors.New("manifest must be a regular YAML file up to 1 MiB")
    }
    data, err := os.ReadFile(filename)
    if err != nil { return nil, err }
    absolute, err := filepath.Abs(filename)
    if err != nil { return nil, err }
    parsed, err := parseManifest(data, filepath.Dir(absolute))
    if err != nil { return nil, err }
    canonical, err := yaml.Marshal(parsed)
    if err != nil { return nil, err }
    return e.RunYAML(ctx, canonical)
}

// RunYAML is the synchronous action-v1 compatibility wrapper. All VM work,
// snapshot checks, SQL writes and cleanup are handled by the async executor.
func (e *Engine) RunYAML(ctx context.Context, data []byte) (*Action, error) {
    action, err := e.SubmitYAML(ctx, SubmitRequest{YAML: data, Format: FormatActionV1})
    if err != nil { return nil, err }
    finished, err := e.WaitAction(ctx, action.ID)
    if err != nil {
        if ctx.Err() != nil {
            _ = e.CancelAction(context.Background(), action.ID)
            cleanupCtx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
            _, _ = e.WaitAction(cleanupCtx, action.ID)
            cancel()
        }
        return nil, err
    }
    if finished.Status != "succeeded" {
        if finished.ErrorMessage != nil && *finished.ErrorMessage != "" {
            return finished, fmt.Errorf("action %s: %s", finished.Status, *finished.ErrorMessage)
        }
        return finished, fmt.Errorf("action %s", finished.Status)
    }
    return finished, nil
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
