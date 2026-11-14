package main

import (
    "encoding/json"
    "fmt"
    "os"
    "strings"
)

type commandItem struct {
    Status string `json:"status"`
    ExitCode *int `json:"exit_code"`
}
type jobItem struct {
    Job struct {
        Name string `json:"name"`
        Status string `json:"status"`
    } `json:"job"`
    SkipReason string `json:"skip_reason"`
    Commands []struct {
        Command commandItem `json:"command"`
    } `json:"commands"`
}
type actionResult struct {
    Action struct {Status string `json:"status"`} `json:"action"`
    Jobs []jobItem `json:"jobs"`
}

func fail(format string, values ...any) {
    fmt.Fprintf(os.Stderr,format+"\n",values...)
    os.Exit(1)
}

func main(){
    if len(os.Args)!=4{
        fail("usage: verify-branch-filter-json <develop|main> <details.json> <cli-exit>")
    }
    branch,path,exit:=os.Args[1],os.Args[2],os.Args[3]
    if branch!="develop"&&branch!="main"{fail("unsupported branch %s",branch)}
    if exit!="0"{fail("CLI returned exit %s (expected 0)",exit)}
    file,err:=os.ReadFile(path)
    if err!=nil{fail("read output: %v",err)}
    var result actionResult
    if err:=json.Unmarshal(file,&result);err!=nil{fail("invalid CLI JSON: %v",err)}
    if result.Action.Status!="succeeded"{
        fail("Action=%s, expected succeeded",result.Action.Status)
    }
    if len(result.Jobs)!=2{fail("got %d jobs, expected exactly 2",len(result.Jobs))}
    expectedRun:="job_"+branch
    expectedSkip:="job_main"
    if branch=="main"{expectedSkip="job_develop"}
    found:=map[string]bool{}
    for _,job:=range result.Jobs{
        name,status:=job.Job.Name,job.Job.Status
        if found[name]{fail("duplicated job %q",name)}
        found[name]=true
        if len(job.Commands)!=1{fail("job %s must have exactly 1 command",name)}
        cmd:=job.Commands[0].Command
        switch name{
        case expectedRun:
            if status!="succeeded"||cmd.Status!="succeeded"||
                cmd.ExitCode==nil||*cmd.ExitCode!=0||job.SkipReason!=""{
                fail("job %s must run successfully; job=%+v command=%+v",name,job,cmd)
            }
        case expectedSkip:
            if status!="skipped"||cmd.Status!="skipped"||cmd.ExitCode!=nil||
                !strings.EqualFold(job.SkipReason,"branch_filter"){
                fail("job %s must be skipped by branch filter; job=%+v command=%+v",name,job,cmd)
            }
        default:fail("unexpected job %s",name)
        }
    }
    if !found[expectedRun]||!found[expectedSkip]{fail("missing matching or skipped job")}
    fmt.Printf("OK branch=%s: %s=succeeded, %s=skipped (no execution)\n",branch,expectedRun,expectedSkip)
}
