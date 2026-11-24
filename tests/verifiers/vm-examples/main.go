package main

import (
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "strconv"
    "strings"

    _ "modernc.org/sqlite"
)

// A suite expectation is intentionally independent of the application's
// JSON serialization. SQLite is inspected directly after the CLI exits.
type expectedCommand struct {
    Status         string `json:"status"`
    ExitCode       *int   `json:"exit_code"`
    FailureKind    string `json:"failure_kind"`
    OutputContains string `json:"output_contains"`
}

type expectedJob struct {
    Status     string            `json:"status"`
    Needs      []string          `json:"needs"`
    SkipReason string            `json:"skip_reason"`
    Commands   []expectedCommand `json:"commands"`
}

type expectedCase struct {
    ActionStatus string                 `json:"action_status"`
    Jobs         map[string]expectedJob `json:"jobs"`
}

type actualCommand struct {
    Command struct {
        ID       string `json:"id"`
        Text     string `json:"command_text"`
        Position int    `json:"position"`
        Status   string `json:"status"`
        ExitCode *int   `json:"exit_code"`
    } `json:"command"`
    FailureKind string `json:"failure_kind"`
}

type actualJob struct {
    Job struct {
        ID     string `json:"id"`
        Name   string `json:"name"`
        Status string `json:"status"`
    } `json:"job"`
    Needs      []string        `json:"needs"`
    SkipReason string          `json:"skip_reason"`
    Commands   []actualCommand `json:"commands"`
}

type actualCase struct {
    Action struct {
        ID     string `json:"id"`
        Status string `json:"status"`
    } `json:"action"`
    VMID           string      `json:"vm_id"`
    VMProfile      string      `json:"vm_profile"`
    CleanupStatus  string      `json:"cleanup_status"`
    MaterialSHA256 string      `json:"material_sha256"`
    Jobs           []actualJob `json:"jobs"`
    Events         []struct{}  `json:"runtime_events"`
}

func fail(format string, args ...any) {
    fmt.Fprintf(os.Stderr,format+"\n",args...)
    os.Exit(1)
}

func parseFiles(resultFile, dbFile, specFile string, cliCode int) error {
    specJSON,err:=os.ReadFile(specFile)
    if err!=nil{return fmt.Errorf("expected JSON: %w",err)}
    var expect expectedCase
    if err=json.Unmarshal(specJSON,&expect);err!=nil{return fmt.Errorf("expected JSON parse: %w",err)}
    if err=validateSpec(expect);err!=nil{return err}

    resultJSON,err:=os.ReadFile(resultFile)
    if err!=nil{return fmt.Errorf("CLI result JSON: %w",err)}
    var actual actualCase
    if err=json.Unmarshal(resultJSON,&actual);err!=nil{return fmt.Errorf("invalid CLI result: %w",err)}
    if actual.Action.ID==""{return errors.New("CLI did not return an Action ID")}
    if actual.Action.Status!=expect.ActionStatus{
        return fmt.Errorf("Action status=%s, expected=%s",actual.Action.Status,expect.ActionStatus)
    }
    if expect.ActionStatus=="succeeded"&&cliCode!=0{
        return fmt.Errorf("successful Action returned CLI exit %d",cliCode)
    }
    if expect.ActionStatus=="failed"&&cliCode==0{
        return errors.New("intentional failed Action must cause a nonzero CLI exit")
    }
    if actual.VMID=="" || actual.CleanupStatus!="succeeded" || actual.VMProfile!="ci-smoke" ||
        len(actual.MaterialSHA256)!=64 || len(actual.Events)==0 {
        return fmt.Errorf("VM, cleanup, profile, snapshot hash or events missing: vm=%s cleanup=%s profile=%s sha=%s events=%d",
            actual.VMID,actual.CleanupStatus,actual.VMProfile,actual.MaterialSHA256,len(actual.Events))
    }

    db,err:=sql.Open("sqlite",dbFile)
    if err!=nil{return fmt.Errorf("SQLite open: %w",err)}
    defer db.Close()
    var integrity string
    if err=db.QueryRow("PRAGMA integrity_check").Scan(&integrity);err!=nil{return fmt.Errorf("SQLite integrity check: %w",err)}
    if integrity!="ok"{return fmt.Errorf("SQLite integrity_check: %s",integrity)}
    var actionCount int
    if err=db.QueryRow("SELECT COUNT(*) FROM actions").Scan(&actionCount);err!=nil{return err}
    if actionCount!=1{return fmt.Errorf("SQLite has %d Actions, expected exactly one",actionCount)}
    var dbID,dbStatus,dbVM,dbCleanup,dbSHA string
    err=db.QueryRow("SELECT id,status,COALESCE(vm_id,''),COALESCE(cleanup_status,''),COALESCE(material_sha256,'') FROM actions").
        Scan(&dbID,&dbStatus,&dbVM,&dbCleanup,&dbSHA)
    if err!=nil{return err}
    if dbID!=actual.Action.ID||dbStatus!=actual.Action.Status||dbVM!=actual.VMID||
        dbCleanup!=actual.CleanupStatus||dbSHA!=actual.MaterialSHA256 {
        return errors.New("SQLite Action metadata differs from CLI output")
    }
    var dbJobs int
    if err=db.QueryRow("SELECT COUNT(*) FROM jobs WHERE action_id=?",dbID).Scan(&dbJobs);err!=nil{return err}
    if dbJobs!=len(expect.Jobs)||len(actual.Jobs)!=len(expect.Jobs){
        return fmt.Errorf("Job count: SQLite=%d CLI=%d expected=%d",dbJobs,len(actual.Jobs),len(expect.Jobs))
    }

    seen:=map[string]bool{}
    validatedCommands:=0
    for _,job:=range actual.Jobs{
        exp,ok:=expect.Jobs[job.Job.Name]
        if !ok||seen[job.Job.Name]{return fmt.Errorf("unexpected or duplicate Job %s",job.Job.Name)}
        seen[job.Job.Name]=true
        if job.Job.ID==""||job.Job.Status!=exp.Status||job.SkipReason!=exp.SkipReason||
            !sameStrings(job.Needs,exp.Needs){
            return fmt.Errorf("Job %s metadata/status/needs: got=%v expected=%v",job.Job.Name,job,exp)
        }
        var dbName,dbJobStatus,dbReason,dbNeeds string
        err=db.QueryRow("SELECT name,status,skip_reason,needs_json FROM jobs WHERE id=? AND action_id=?",
            job.Job.ID,dbID).Scan(&dbName,&dbJobStatus,&dbReason,&dbNeeds)
        if err!=nil{return fmt.Errorf("SQLite Job %s: %w",job.Job.Name,err)}
        var persistedNeeds []string
        if err=json.Unmarshal([]byte(dbNeeds),&persistedNeeds);err!=nil{return err}
        if dbName!=job.Job.Name||dbJobStatus!=exp.Status||dbReason!=exp.SkipReason||
            !sameStrings(persistedNeeds,exp.Needs){
            return fmt.Errorf("SQLite Job %s differs from expected status/needs",job.Job.Name)
        }
        if len(job.Commands)!=len(exp.Commands){return fmt.Errorf("Job %s: %d Commands expected %d",
            job.Job.Name,len(job.Commands),len(exp.Commands))}
        var dbCommandCount int
        if err=db.QueryRow("SELECT COUNT(*) FROM commands WHERE job_id=?",job.Job.ID).
            Scan(&dbCommandCount);err!=nil{return err}
        if dbCommandCount!=len(exp.Commands){return fmt.Errorf("Job %s SQLite Commands: %d expected %d",
            job.Job.Name,dbCommandCount,len(exp.Commands))}
        for index,command:=range job.Commands{
            want:=exp.Commands[index]
            if command.Command.ID==""||command.Command.Position!=index||
                command.Command.Status!=want.Status||
                !sameExit(command.Command.ExitCode,want.ExitCode)||
                (want.FailureKind!=""&&command.FailureKind!=want.FailureKind){
                return fmt.Errorf("Job %s Command %d CLI=%+v expected=%+v",job.Job.Name,index,command,want)
            }
            var dbStatus, dbKind,dbText string
            var dbPosition int
            var dbExit sql.NullInt64
            err=db.QueryRow("SELECT position,status,exit_code,COALESCE(failure_kind,''),command_text FROM commands WHERE id=? AND job_id=?",
                command.Command.ID,job.Job.ID).
                Scan(&dbPosition,&dbStatus,&dbExit,&dbKind,&dbText)
            if err!=nil{return fmt.Errorf("Job %s Command %d SQLite: %w",job.Job.Name,index,err)}
            if dbPosition!=index||dbStatus!=want.Status||!sameDBExit(dbExit,want.ExitCode)||
                dbText!=command.Command.Text||dbKind!=command.FailureKind {
                return fmt.Errorf("Job %s Command %d differs between SQLite/CLI/spec",job.Job.Name,index)
            }
            var count,logLines int
            rows,err:=db.Query("SELECT stream_type,payload FROM command_outputs WHERE command_id=? ORDER BY sequence_no",
                command.Command.ID)
            if err!=nil{return err}
            var stdout strings.Builder
            for rows.Next(){
                var stream string
                var payload []byte
                if err=rows.Scan(&stream,&payload);err!=nil{break}
                if stream=="stdout"{stdout.Write(payload)}
                count++
            }
            if err==nil{err=rows.Err()}
            rows.Close()
            if err!=nil{return err}
            if err=db.QueryRow("SELECT COUNT(*) FROM command_log_lines WHERE command_id=?",command.Command.ID).
                Scan(&logLines);err!=nil{return err}
            if want.Status=="skipped"{
                if count!=0||logLines!=0{return fmt.Errorf("skipped Job %s Command %d unexpectedly produced output",job.Job.Name,index)}
            } else if want.OutputContains!=""{
                if count==0||logLines==0||!strings.Contains(stdout.String(),want.OutputContains){
                    return fmt.Errorf("Job %s Command %d missing persisted stdout marker %q (chunks=%d logs=%d stdout=%q)",
                        job.Job.Name,index,want.OutputContains,count,logLines,stdout.String())
                }
            }
            validatedCommands++
        }
    }
    fmt.Printf("OK: Action=%s VM=%s cleanup=%s Jobs=%d Commands=%d SQLite/logs verificados\n",
        actual.Action.Status,actual.VMID,actual.CleanupStatus,len(expect.Jobs),validatedCommands)
    return nil
}

func validateSpec(v expectedCase)error{
    if v.ActionStatus!="succeeded"&&v.ActionStatus!="failed"{return errors.New("expected action_status succeeded or failed")}
    if len(v.Jobs)==0{return errors.New("expected at least one Job")}
    for name,job:=range v.Jobs{
        if job.Status!="succeeded"&&job.Status!="failed"&&job.Status!="skipped"{
            return fmt.Errorf("invalid expected Job status for %s",name)
        }
        if len(job.Commands)==0{return fmt.Errorf("Job %s expected no Commands",name)}
        for i,c:=range job.Commands{
            if c.Status!="succeeded"&&c.Status!="failed"&&c.Status!="skipped"{
                return fmt.Errorf("Job %s Command %d: invalid expected status",name,i)
            }
            if c.Status=="skipped"&&c.ExitCode!=nil{return fmt.Errorf("skipped Job %s has an exit code",name)}
            if c.Status!="skipped"&&c.ExitCode==nil{return fmt.Errorf("Job %s expects missing exit code",name)}
        }
    }
    return nil
}
func sameStrings(a,b []string)bool{
    if len(a)!=len(b){return false}
    for i:=range a {if a[i]!=b[i]{return false}}
    return true
}
func sameExit(a,b *int)bool{
    if a==nil||b==nil{return a==nil&&b==nil}
    return *a==*b
}
func sameDBExit(db sql.NullInt64,want *int)bool{
    if want==nil{return !db.Valid}
    return db.Valid&&db.Int64==int64(*want)
}
func main(){
    if len(os.Args)!=5{fail("usage: verify-vm <action.json> <actions.db> <expected.json> <cli-exit-code>")}
    code,err:=strconv.Atoi(os.Args[4])
    if err!=nil{fail("invalid CLI exit: %v",err)}
    if err=parseFiles(os.Args[1],os.Args[2],os.Args[3],code);err!=nil{fail("%v",err)}
}
