package legacy

import (
    "context"
    "database/sql"
    "path/filepath"
    "testing"

    _ "modernc.org/sqlite"
)
func TestActionSchemaAndIdempotency(t *testing.T){
    ctx:=context.Background()
    db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"legacy.db"))
    if err!=nil{t.Fatal(err)}
    defer db.Close()
    db.SetMaxOpenConns(1)
    for _,statement:=range []string{
        `PRAGMA foreign_keys=ON`,
        `CREATE TABLE clones(id TEXT PRIMARY KEY)`,
        `CREATE TABLE action_events(event_id TEXT PRIMARY KEY)`,
        `CREATE TABLE tool_executions(execution_id TEXT PRIMARY KEY)`,
        `INSERT INTO clones(id) VALUES('clone-1')`,
        `INSERT INTO action_events(event_id) VALUES('evt-1')`,
    }{
        if _,err=db.ExecContext(ctx,statement);err!=nil{t.Fatal(err)}
    }
    if err=EnsureActionSchema(db);err!=nil{t.Fatal(err)}
    store:=NewStore(db,nil)
    plan:=ActionPlan{
        CloneID:"clone-1",CommitSHA:"deadbeef",Branch:"develop",
        WorkflowPath:".gitmaster/workflow/go.yml",VMProfile:"ci-go",
        EventID:"evt-1",Scope:"operational",OwnerUID:1000,
        Jobs:[]ActionJobPlan{{
            Name:"unit",Steps:[]ActionCommandPlan{{Name:"test",Run:"go test ./..."}},
        }},
    }
    id,err:=store.EnqueueAction(ctx,plan)
    if err!=nil{t.Fatal(err)}
    again,err:=store.EnqueueAction(ctx,plan)
    if err!=nil{t.Fatal(err)}
    if id!=again{t.Fatalf("duplicate event produced two actions: %s %s",id,again)}
    a,err:=store.ReadAction(ctx,id)
    if err!=nil{t.Fatal(err)}
    if a.Status!="queued"||len(a.Jobs)!=1||len(a.Jobs[0].Commands)!=1{
        t.Fatalf("unexpected action: %#v",a)
    }
    if err=store.StartAction(ctx,id);err!=nil{t.Fatal(err)}
    if err=store.RecordInsulaVM(ctx,id,"vm-1");err!=nil{t.Fatal(err)}
    if err=store.AddActionRuntimeMessage(ctx,id,"start","VM started");err!=nil{t.Fatal(err)}
    a,err=store.ReadAction(ctx,id)
    if err!=nil{t.Fatal(err)}
    if a.InsulaVMID==nil||*a.InsulaVMID!="vm-1"||len(a.RuntimeEvents)!=1{
        t.Fatalf("runtime state missing: %#v",a)
    }
}
func TestInvalidActionPlanCannotQueue(t *testing.T){
    db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"empty.db"))
    if err!=nil{t.Fatal(err)}
    defer db.Close()
    store:=NewStore(db,nil)
    if _,err=store.EnqueueAction(context.Background(),ActionPlan{CloneID:"x"});err==nil{
        t.Fatal("incomplete action plan accepted")
    }
}
