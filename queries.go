package gitmasteractions

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"
)
func(e *Engine) GetAction(ctx context.Context,id string)(*Action,error){
    if err:=e.assertOpen();err!=nil{return nil,err}
    var a Action
    err:=e.db.QueryRowContext(ctx,`SELECT id,name,status,created_at,started_at,finished_at,error_message
       FROM actions WHERE id=?`,id).
       Scan(&a.ID,&a.Name,&a.Status,&a.CreatedAt,&a.StartedAt,&a.FinishedAt,&a.ErrorMessage)
    if err!=nil{return nil,err}
    a.engine=e
    return &a,nil
}
func(e *Engine) ListActions(ctx context.Context,filter ActionFilter,page Page)([]*Action,error){
    if err:=e.assertOpen();err!=nil{return nil,err}
    limit:=page.Limit
    if limit==0{limit=100}
    if limit<1||limit>500{return nil,errors.New("limit must be 1..500")}
    var rows *sql.Rows
    var err error
    if filter.Status==""{
        rows,err=e.db.QueryContext(ctx,`SELECT id,name,status,created_at,started_at,finished_at,error_message
           FROM actions ORDER BY created_at DESC,id DESC LIMIT ?`,limit)
    }else{
        rows,err=e.db.QueryContext(ctx,`SELECT id,name,status,created_at,started_at,finished_at,error_message
           FROM actions WHERE status=? ORDER BY created_at DESC,id DESC LIMIT ?`,filter.Status,limit)
    }
    if err!=nil{return nil,err}
    defer rows.Close()
    out:=[]*Action{}
    for rows.Next(){
        a:=&Action{engine:e}
        if err:=rows.Scan(&a.ID,&a.Name,&a.Status,&a.CreatedAt,&a.StartedAt,&a.FinishedAt,&a.ErrorMessage);err!=nil{return nil,err}
        out=append(out,a)
    }
    return out,rows.Err()
}
func(a *Action) Jobs(ctx context.Context)([]*Job,error){
    if a==nil||a.engine==nil{return nil,errors.New("detached action")}
    return a.engine.jobs(ctx,a.ID)
}
func(e *Engine) jobs(ctx context.Context,id string)([]*Job,error){
    if err:=e.assertOpen();err!=nil{return nil,err}
    rows,err:=e.db.QueryContext(ctx,`SELECT id,action_id,name,position,allow_failure,
       status,started_at,finished_at FROM jobs WHERE action_id=? ORDER BY position`,id)
    if err!=nil{return nil,err}
    defer rows.Close()
    out:=[]*Job{}
    for rows.Next(){
        var j Job
        var allowed int
        if err:=rows.Scan(&j.ID,&j.ActionID,&j.Name,&j.Position,&allowed,&j.Status,&j.StartedAt,&j.FinishedAt);err!=nil{return nil,err}
        j.AllowFailure=allowed!=0
        j.engine=e
        out=append(out,&j)
    }
    return out,rows.Err()
}
func(e *Engine) GetJob(ctx context.Context,id string)(*Job,error){
    if err:=e.assertOpen();err!=nil{return nil,err}
    j:=&Job{engine:e}
    var allowed int
    err:=e.db.QueryRowContext(ctx,`SELECT id,action_id,name,position,allow_failure,status,started_at,finished_at
       FROM jobs WHERE id=?`,id).Scan(
        &j.ID,&j.ActionID,&j.Name,&j.Position,&allowed,&j.Status,&j.StartedAt,&j.FinishedAt)
    if err!=nil{return nil,err}
    j.AllowFailure=allowed!=0
    return j,nil
}
func(j *Job) Commands(ctx context.Context)([]*Command,error){
    if j==nil||j.engine==nil{return nil,errors.New("detached job")}
    return j.engine.commands(ctx,j.ID)
}
func(e *Engine) commands(ctx context.Context,id string)([]*Command,error){
    if err:=e.assertOpen();err!=nil{return nil,err}
    rows,err:=e.db.QueryContext(ctx,`SELECT id,job_id,position,command_text,status,exit_code,started_at,finished_at
       FROM commands WHERE job_id=? ORDER BY position`,id)
    if err!=nil{return nil,err}
    defer rows.Close()
    out:=[]*Command{}
    for rows.Next(){
        c:=&Command{engine:e}
        if err:=rows.Scan(&c.ID,&c.JobID,&c.Position,&c.Text,&c.Status,&c.ExitCode,&c.StartedAt,&c.FinishedAt);err!=nil{return nil,err}
        out=append(out,c)
    }
    return out,rows.Err()
}
func(e *Engine) GetCommand(ctx context.Context,id string)(*Command,error){
    if err:=e.assertOpen();err!=nil{return nil,err}
    c:=&Command{engine:e}
    err:=e.db.QueryRowContext(ctx,`SELECT id,job_id,position,command_text,status,exit_code,started_at,finished_at
       FROM commands WHERE id=?`,id).Scan(
       &c.ID,&c.JobID,&c.Position,&c.Text,&c.Status,&c.ExitCode,&c.StartedAt,&c.FinishedAt)
    if err!=nil{return nil,err}
    return c,nil
}
func(c *Command) Logs(ctx context.Context,skipLines ...uint64)([]CommandLog,error){
    if c==nil||c.engine==nil{return nil,errors.New("detached command")}
    if err:=c.engine.assertOpen();err!=nil{return nil,err}
    if len(skipLines)>1{return nil,errors.New("only one cursor argument allowed")}
    cursor:=uint64(0)
    if len(skipLines)==1{cursor=skipLines[0]}
    rows,err:=c.engine.db.QueryContext(ctx,`SELECT line_no,stream_type,payload,captured_at
       FROM command_log_lines WHERE command_id=? AND line_no>? ORDER BY line_no LIMIT 500`,c.ID,cursor)
    if err!=nil{return nil,err}
    defer rows.Close()
    out:=[]CommandLog{}
    for rows.Next(){
        var l CommandLog
        var dt string
        if err:=rows.Scan(&l.LineNumber,&l.StreamType,&l.Payload,&dt);err!=nil{return nil,err}
        l.CapturedAt,err=time.Parse(time.RFC3339Nano,dt)
        if err!=nil{return nil,err}
        out=append(out,l)
    }
    return out,rows.Err()
}
func(c *Command) Outputs(ctx context.Context,filter OutputFilter,page Page)([]CommandOutput,error){
    if c==nil||c.engine==nil{return nil,errors.New("detached command")}
    if err:=c.engine.assertOpen();err!=nil{return nil,err}
    limit:=page.Limit
    if limit==0{limit=100}
    if limit<1||limit>500||page.AfterID<0{return nil,errors.New("invalid output pagination")}
    if filter.StreamType!=""&&filter.StreamType!="stdin"&&filter.StreamType!="stdout"&&filter.StreamType!="stderr"{
        return nil,fmt.Errorf("invalid output stream %q",filter.StreamType)
    }
    var rows *sql.Rows
    var err error
    if filter.StreamType==""{
        rows,err=c.engine.db.QueryContext(ctx,`SELECT id,sequence_no,stream_type,payload,captured_at
           FROM command_outputs WHERE command_id=? AND id>? ORDER BY id LIMIT ?`,c.ID,page.AfterID,limit)
    }else{
        rows,err=c.engine.db.QueryContext(ctx,`SELECT id,sequence_no,stream_type,payload,captured_at
           FROM command_outputs WHERE command_id=? AND id>? AND stream_type=? ORDER BY id LIMIT ?`,c.ID,page.AfterID,filter.StreamType,limit)
    }
    if err!=nil{return nil,err}
    defer rows.Close()
    out:=[]CommandOutput{}
    for rows.Next(){
        var o CommandOutput
        var dt string
        if err:=rows.Scan(&o.ID,&o.SequenceNumber,&o.StreamType,&o.Payload,&dt);err!=nil{return nil,err}
        o.CapturedAt,err=time.Parse(time.RFC3339Nano,dt)
        if err!=nil{return nil,err}
        out=append(out,o)
    }
    return out,rows.Err()
}
