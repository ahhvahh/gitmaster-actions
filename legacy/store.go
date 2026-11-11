package legacy

import (
    "context"
    "crypto/rand"
    "database/sql"
    "fmt"
    "regexp"
    "strings"
    "time"
)

// ExecutionLogAdapter preserves GitMaster's existing MCP execution logs while
// action-owned SQL, jobs, VM lifecycles and result queries move to this module.
type ExecutionLogAdapter interface {
    CreateExecutionWithType(context.Context,string,string,string,uint32,string)error
    AppendActionMessages(context.Context,string,[]string)error
    FinishExecution(context.Context,string,string,string,[]string)error
}
type Store struct {
    DB *sql.DB
    Logs ExecutionLogAdapter
}
func NewStore(db *sql.DB, logs ExecutionLogAdapter)*Store {
    return &Store{DB:db,Logs:logs}
}
func EnsureActionSchema(db *sql.DB)error{return ensureActionSchema(db)}
func now()string{return time.Now().UTC().Format(time.RFC3339Nano)}
func newUUIDv7()(string,error){
    var b [16]byte
    if _,err:=rand.Read(b[:]);err!=nil{return "",err}
    ms:=uint64(time.Now().UnixMilli())
    for i:=5;i>=0;i--{b[i]=byte(ms);ms>>=8}
    b[6]=(b[6]&0x0f)|0x70;b[8]=(b[8]&0x3f)|0x80
    return fmt.Sprintf("%x-%x-%x-%x-%x",b[:4],b[4:6],b[6:8],b[8:10],b[10:]),nil
}
func(s *Store) CreateExecutionWithType(ctx context.Context,id,tool,scope string,uid uint32,kind string)error{
    if s.Logs==nil{return fmt.Errorf("execution log adapter required")}
    return s.Logs.CreateExecutionWithType(ctx,id,tool,scope,uid,kind)
}
func(s *Store) AppendActionMessages(ctx context.Context,id string,m []string)error{
    if s.Logs==nil{return fmt.Errorf("execution log adapter required")}
    return s.Logs.AppendActionMessages(ctx,id,m)
}
func(s *Store) FinishExecution(ctx context.Context,id,status,code string,m []string)error{
    if s.Logs==nil{return fmt.Errorf("execution log adapter required")}
    return s.Logs.FinishExecution(ctx,id,status,code,m)
}
var secrets=regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]+|gh[pousr]_[A-Za-z0-9_]+|Bearer[[:space:]]+[^[:space:]]+)`)
func redact(v,token string)string{
    if token!=""{v=strings.ReplaceAll(v,token,"[redacted]")}
    return secrets.ReplaceAllString(v,"[redacted]")
}
func redactShort(v,token string)string{
    v=redact(v,token)
    if len(v)>1024{v=v[:1024]}
    return v
}
