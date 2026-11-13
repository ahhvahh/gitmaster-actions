package main

import (
    "encoding/json"
    "fmt"
    "os"
)

type output struct {
    Action struct { Status string `json:"status"` } `json:"action"`
    Jobs []struct {
        Job struct {Name string `json:"name"`;Status string `json:"status"`} `json:"job"`
        SkipReason string `json:"skip_reason"`
        Commands []struct { Command struct { ExitCode *int `json:"exit_code"` } `json:"command"` } `json:"commands"`
    } `json:"jobs"`
}

func main(){
    if len(os.Args)!=4{fmt.Fprintln(os.Stderr,"usage: go run verify-workflow-json.go <branch> <result.json> <cli-exit-code>");os.Exit(2)}
    branch:=os.Args[1]
    data,err:=os.ReadFile(os.Args[2])
    if err!=nil{fail("%v",err)}
    var result output
    if err:=json.Unmarshal(data,&result);err!=nil{fail("invalid JSON output: %v",err)}
    expected:="succeeded"
    code:="0"
    switch branch{
    case "develop","main":
    case "teste-falha":
        expected="failed";code="1"
    default:fail("unknown branch %q",branch)
    }
    if result.Action.Status!=expected{fail("Action status %q expected %q",result.Action.Status,expected)}
    if code=="0"&&os.Args[3]!="0"{fail("CLI exit %s (expected 0)",os.Args[3])}
    if code=="1"&&os.Args[3]=="0"{fail("CLI must exit nonzero for intentional failure")}
    got:=map[string]struct{status,reason string;exit *int}{}
    for _,j:=range result.Jobs{
        var exit *int
        if len(j.Commands)>0{exit=j.Commands[0].Command.ExitCode}
        got[j.Job.Name]=struct{status,reason string;exit *int}{j.Job.Status,j.SkipReason,exit}
    }
    wanted:=map[string]string{
        "preparar":"succeeded","paralelo_a":"succeeded","paralelo_b":"succeeded",
        "unir_paralelos":"succeeded","finalizar":"succeeded",
        "somente_develop":"skipped","somente_main":"skipped",
        "bloqueado_por_filtro":"skipped","falha_intencional":"skipped",
        "bloqueado_por_falha":"skipped",
    }
    switch branch{
    case "develop":wanted["somente_develop"]="succeeded"
    case "main":wanted["somente_main"]="succeeded";wanted["bloqueado_por_filtro"]="succeeded"
    case "teste-falha":wanted["falha_intencional"]="failed"
    }
    if len(got)!=len(wanted){fail("number of Jobs: %d expected %d",len(got),len(wanted))}
    for name,desired:=range wanted{
        j,ok:=got[name]
        if !ok||j.status!=desired{fail("Job %s status %q expected %q",name,j.status,desired)}
    }
    if branch=="teste-falha" {
        j:=got["falha_intencional"]
        if j.exit==nil||*j.exit!=17{fail("expected command exit code 17")}
        if got["bloqueado_por_falha"].reason!="dependency_not_succeeded:falha_intencional"{
            fail("blocked job does not record dependency reason")
        }
    }
    fmt.Printf("OK: %s, Action=%s, %d Job statuses verified\n",branch,result.Action.Status,len(wanted))
}

func fail(format string,args ...any) {
    fmt.Fprintf(os.Stderr,format+"\n",args...)
    os.Exit(1)
}
