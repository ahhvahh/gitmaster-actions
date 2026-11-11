package gitmasteractions

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"

    _ "modernc.org/sqlite"
)

// Store belongs only to GitMaster Actions; its SQL handle is never exported.
func openStore(path string)(*sql.DB,error){
    if path==""{return nil,errors.New("DatabasePath is required")}
    db,err:=sql.Open("sqlite",path)
    if err!=nil{return nil,err}
    db.SetMaxOpenConns(1)
    if _,err=db.Exec("PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;");err!=nil{
        _=db.Close();return nil,err
    }
    schema:=[]string{
        `CREATE TABLE IF NOT EXISTS actions (
          id TEXT PRIMARY KEY, name TEXT NOT NULL,
          status TEXT NOT NULL CHECK(status IN ('pending','starting','running','succeeded','failed','cancelled')),
          created_at TEXT NOT NULL, started_at TEXT, finished_at TEXT, error_message TEXT)`,
        `CREATE TABLE IF NOT EXISTS jobs (
          id TEXT PRIMARY KEY, action_id TEXT NOT NULL REFERENCES actions(id),
          name TEXT NOT NULL, position INTEGER NOT NULL, allow_failure INTEGER NOT NULL,
          status TEXT NOT NULL, started_at TEXT,finished_at TEXT,error_message TEXT,
          UNIQUE(action_id,position),UNIQUE(action_id,name))`,
        `CREATE TABLE IF NOT EXISTS commands (
          id TEXT PRIMARY KEY,job_id TEXT NOT NULL REFERENCES jobs(id),
          position INTEGER NOT NULL,command_text TEXT NOT NULL,status TEXT NOT NULL,
          exit_code INTEGER,failure_kind TEXT,error_message TEXT,started_at TEXT,finished_at TEXT,
          UNIQUE(job_id,position))`,
        `CREATE TABLE IF NOT EXISTS command_outputs (
          id INTEGER PRIMARY KEY AUTOINCREMENT,command_id TEXT NOT NULL REFERENCES commands(id),
          sequence_no INTEGER NOT NULL,stream_type TEXT NOT NULL,
          payload BLOB NOT NULL,captured_at TEXT NOT NULL,UNIQUE(command_id,sequence_no))`,
        `CREATE TABLE IF NOT EXISTS command_log_lines (
          command_id TEXT NOT NULL REFERENCES commands(id),line_no INTEGER NOT NULL,
          stream_type TEXT NOT NULL,payload BLOB NOT NULL,captured_at TEXT NOT NULL,
          PRIMARY KEY(command_id,line_no))`,
        `CREATE INDEX IF NOT EXISTS idx_jobs_action ON jobs(action_id)`,
        `CREATE INDEX IF NOT EXISTS idx_commands_job ON commands(job_id)`,
        `CREATE INDEX IF NOT EXISTS idx_outputs_command ON command_outputs(command_id,sequence_no)`,
        `CREATE INDEX IF NOT EXISTS idx_actions_status ON actions(status,created_at)`,
    }
    for _,stmt:=range schema{
        if _,err=db.Exec(stmt);err!=nil{_ = db.Close();return nil,fmt.Errorf("migrate actions db: %w",err)}
    }
    return db,nil
}
func timestamp()string{return time.Now().UTC().Format(time.RFC3339Nano)}
func (e *Engine) setStatus(ctx context.Context,table,id,status,errMessage string)error {
    // Only these internal, compile-time callsites can select a table.
    switch table {
    case "actions","jobs","commands":
    default:return errors.New("invalid status table")
    }
    var query string
    switch status{
    case "starting","running":
        query="UPDATE "+table+" SET status=?,started_at=COALESCE(started_at,?) WHERE id=?"
        _,err:=e.db.ExecContext(ctx,query,status,timestamp(),id)
        return err
    case "pending":
        return errors.New("cannot reset to pending")
    case "succeeded","failed","cancelled","skipped":
        query="UPDATE "+table+" SET status=?,finished_at=?"
        if table=="commands"{query+=",error_message=?"}
        if table=="jobs"{query+=",error_message=?"}
        if table=="actions"{query+=",error_message=?"}
        query+=" WHERE id=?"
        _,err:=e.db.ExecContext(ctx,query,status,timestamp(),errMessage,id)
        return err
    default:return errors.New("invalid status")
    }
}
func (e *Engine) appendOutput(ctx context.Context,commandID,stream string,payload []byte)error {
    if stream!="stdout"&&stream!="stderr"&&stream!="stdin" {
        return fmt.Errorf("unsupported stream %q",stream)
    }
    tx,err:=e.db.BeginTx(ctx,nil)
    if err!=nil{return err}
    defer tx.Rollback()
    var next int64
    if err=tx.QueryRowContext(ctx,`SELECT COALESCE(MAX(sequence_no),-1)+1
       FROM command_outputs WHERE command_id=?`,commandID).Scan(&next);err!=nil{return err}
    now:=timestamp()
    if _,err=tx.ExecContext(ctx,`INSERT INTO command_outputs
       (command_id,sequence_no,stream_type,payload,captured_at)
       VALUES(?,?,?,?,?)`,commandID,next,stream,payload,now);err!=nil{return err}
    var line int64
    if err=tx.QueryRowContext(ctx,`SELECT COALESCE(MAX(line_no),0)+1
       FROM command_log_lines WHERE command_id=?`,commandID).Scan(&line);err!=nil{return err}
    if _,err=tx.ExecContext(ctx,`INSERT INTO command_log_lines
       (command_id,line_no,stream_type,payload,captured_at)
       VALUES(?,?,?,?,?)`,commandID,line,stream,payload,now);err!=nil{return err}
    return tx.Commit()
}
