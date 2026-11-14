package gitmasteractions

import (
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

// A workflow with exactly two jobs must execute only the branch-matching job;
// the other job and all of its commands must be persisted as skipped.
func TestBranchFilterSmokeExecutesOnlyMatchingJob(t *testing.T) {
    yaml, err := os.ReadFile(filepath.Join("..", "examples", "workflow", "branch-filter-smoke.yaml"))
    if err != nil { t.Fatal(err) }

    for _, tc := range []struct{
        branch string
        run string
        skip string
        marker string
        forbidden string
    }{
        {"develop","job_develop","job_main","JOB_DEVELOP_EXECUTADO","JOB_MAIN_EXECUTADO"},
        {"main","job_main","job_develop","JOB_MAIN_EXECUTADO","JOB_DEVELOP_EXECUTADO"},
    } {
        t.Run(tc.branch,func(t *testing.T){
            rt:=&smokeRuntime{testRuntime:&testRuntime{}}
            engine:=smokeEngine(t,rt)

            a,err:=engine.SubmitYAML(context.Background(),SubmitRequest{
                YAML:yaml,
                Format:FormatGitMasterV1,
                Origin:ActionOrigin{Branch:tc.branch,EventType:"manual"},
            })
            if err!=nil{t.Fatal(err)}
            done,err:=engine.WaitAction(context.Background(),a.ID)
            if err!=nil{t.Fatal(err)}
            if done.Status!="succeeded"{t.Fatalf("action status=%s, expected succeeded",done.Status)}
            details,err:=engine.GetActionDetails(context.Background(),a.ID)
            if err!=nil{t.Fatal(err)}
            jobs:=jobStates(t,details)
            if len(jobs)!=2{t.Fatalf("got %d jobs; want exactly 2",len(jobs))}

            for _,expect:=range []struct{
                name string
                status string
                skipReason string
            }{
                {tc.run,"succeeded",""},
                {tc.skip,"skipped","branch_filter"},
            } {
                job,ok:=jobs[expect.name]
                if !ok{t.Fatalf("job %q not persisted",expect.name)}
                if job.Job.Status!=expect.status{
                    t.Errorf("job %q status=%q, want %q",expect.name,job.Job.Status,expect.status)
                }
                if job.SkipReason!=expect.skipReason{
                    t.Errorf("job %q skip reason=%q, want %q",expect.name,job.SkipReason,expect.skipReason)
                }
                if len(job.Commands)!=1{t.Fatalf("job %q command count=%d, want 1",expect.name,len(job.Commands))}
                cmd:=job.Commands[0].Command
                if cmd.Status!=expect.status{
                    t.Errorf("job %q command status=%q, want %q",expect.name,cmd.Status,expect.status)
                }
                if expect.status=="skipped"&&cmd.ExitCode!=nil{
                    t.Errorf("job %q was skipped but has exit code %d",expect.name,*cmd.ExitCode)
                }
                if expect.status=="succeeded"&&(cmd.ExitCode==nil||*cmd.ExitCode!=0){
                    t.Errorf("job %q did not finish with code 0: %v",expect.name,cmd.ExitCode)
                }
                logs,err:=engine.ListCommandLogs(context.Background(),cmd.ID,0)
                if err!=nil{t.Fatal(err)}
                if expect.status=="skipped"&&len(logs)!=0{
                    t.Errorf("skipped job %q produced %d output lines",expect.name,len(logs))
                }
                if expect.status=="succeeded"&&len(logs)==0{
                    t.Errorf("executed job %q produced no output",expect.name)
                }
            }

            rt.muSmoke.Lock()
            calls:=append([]string(nil),rt.calls...)
            rt.muSmoke.Unlock()
            if len(calls)!=1{t.Fatalf("runtime received %d commands, expected exactly 1: %v",len(calls),calls)}
            if !strings.Contains(calls[0],tc.marker)||strings.Contains(calls[0],tc.forbidden){
                t.Fatalf("wrong command executed on branch %s: %q",tc.branch,calls[0])
            }
        })
    }
}
