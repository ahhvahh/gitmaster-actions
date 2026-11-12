package gitmasteractions

import (
    "context"
    "database/sql"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"
)
const validManifest = `schema_version: 1
vm:
  profile: ci-go
action:
  name: validate
  jobs:
    - name: build
      commands: ["go test ./..."]
`

func TestManifestStrictAndNoVMCreation(t *testing.T){
    m,err:=parseManifest([]byte(validManifest),"")
    if err!=nil{t.Fatal(err)}
    if m.VM.Profile!="ci-go"||m.Action.Name!="validate"||len(m.Action.Jobs)!=1{
        t.Fatalf("unexpected manifest: %#v",m)
    }
    for _,sample:=range []string{
        strings.Replace(validManifest,"schema_version: 1","schema_version: 2",1),
        validManifest+"unknown_field: true\n",
        strings.Replace(validManifest,"go test ./...","",1),
        validManifest+"---\nschema_version: 1\n",
    }{
        if _,err:=parseManifest([]byte(sample),"");err==nil{
            t.Fatalf("accepted invalid manifest %q",sample)
        }
    }
}
func TestManifestImportsRestricted(t *testing.T){
    dir:=t.TempDir()
    archive:=filepath.Join(dir,"source.tar.gz")
    if err:=os.WriteFile(archive,[]byte("test"),0600);err!=nil{t.Fatal(err)}
    yaml:=validManifest+""
    yaml=strings.Replace(yaml,"  jobs:", "  imports:\n    - source: source.tar.gz\n  jobs:",1)
    if _,err:=parseManifest([]byte(yaml),"");err==nil{t.Fatal("RunYAML must reject relative imports")}
    m,err:=parseManifest([]byte(yaml),dir)
    if err!=nil{t.Fatal(err)}
    if m.Action.Imports[0].Source!=archive{t.Fatalf("resolved wrong path: %s",m.Action.Imports[0].Source)}
}
func TestStoreQueriesAndCursors(t *testing.T){
    ctx:=context.Background()
    e,err:=New(Options{DatabasePath:filepath.Join(t.TempDir(),"actions.db")})
    if err!=nil{t.Fatal(err)}
    defer e.Close()
    aid,_:=uuidv7();jid,_:=uuidv7();cid,_:=uuidv7()
    if _,err=e.db.ExecContext(ctx,`INSERT INTO actions(id,name,status,created_at)VALUES(?,?,?,?)`,
       aid,"build","pending",timestamp());err!=nil{t.Fatal(err)}
    if _,err=e.db.ExecContext(ctx,`INSERT INTO jobs(id,action_id,name,position,allow_failure,status)
       VALUES(?,?,?,?,?,?)`,jid,aid,"test",0,0,"pending");err!=nil{t.Fatal(err)}
    if _,err=e.db.ExecContext(ctx,`INSERT INTO commands(id,job_id,position,command_text,status)
       VALUES(?,?,?,?,?)`,cid,jid,0,"go test ./...","running");err!=nil{t.Fatal(err)}
    if err=e.appendOutput(ctx,cid,"stdout",[]byte("ok\n"));err!=nil{t.Fatal(err)}
    if err=e.appendOutput(ctx,cid,"stderr",[]byte("warning\n"));err!=nil{t.Fatal(err)}
    a,err:=e.GetAction(ctx,aid);if err!=nil{t.Fatal(err)}
    jobs,err:=a.Jobs(ctx);if err!=nil{t.Fatal(err)}
    commands,err:=jobs[0].Commands(ctx);if err!=nil{t.Fatal(err)}
    logs,err:=commands[0].Logs(ctx);if err!=nil{t.Fatal(err)}
    if len(logs)!=2||logs[0].LineNumber!=1||logs[1].LineNumber!=2{t.Fatalf("unexpected logs: %#v",logs)}
    next,err:=commands[0].Logs(ctx,logs[0].LineNumber);if err!=nil{t.Fatal(err)}
    if len(next)!=1||next[0].StreamType!="stderr"{t.Fatalf("bad cursor: %#v",next)}
    chunks,err:=commands[0].Outputs(ctx,OutputFilter{StreamType:"stdout"},Page{})
    if err!=nil{t.Fatal(err)}
    if len(chunks)!=1||string(chunks[0].Payload)!="ok\n"{t.Fatalf("bad output: %#v",chunks)}
    _,err=commands[0].Logs(ctx,1,2)
    if err==nil{t.Fatal("accepted multiple cursors")}
    if _,err=e.GetAction(ctx,"no-such-id");!errors.Is(err,sql.ErrNoRows){t.Fatalf("missing id: %v",err)}
    if err=e.Close();err!=nil{t.Fatal(err)}
    if _,err=e.GetAction(ctx,aid);err==nil{t.Fatal("read after Close should fail")}
}
func TestNoActionAcceptedWithoutInsulaVM(t *testing.T){
    e,err:=New(Options{DatabasePath:filepath.Join(t.TempDir(),"actions.db")})
    if err!=nil{t.Fatal(err)}
    defer e.Close()
    a,err:=e.RunYAML(context.Background(),[]byte(validManifest))
    if err==nil||a!=nil{t.Fatalf("expected unavailable runtime, got %#v %v",a,err)}
}
