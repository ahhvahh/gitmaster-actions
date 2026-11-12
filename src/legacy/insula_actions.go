package legacy

import (
 "context"
 "errors"
 "fmt"
)

// ListQueuedTriggeredActions feeds the background InsulaVM worker. Console
// actions have no Git event and are intentionally excluded from the queue.
func (s *Store) ListQueuedTriggeredActions(ctx context.Context, limit int)([]string,error){
 if limit<1||limit>20{return nil,errors.New("INVALID_ARGUMENT: queue limit must be 1..20")}
 rows,err:=s.DB.QueryContext(ctx,`SELECT action_run_id FROM action_runs
  WHERE status='queued' AND event_id IS NOT NULL
  ORDER BY created_at ASC,action_run_id ASC LIMIT ?`,limit)
 if err!=nil{return nil,err}
 defer rows.Close()
 ids:=[]string{}
 for rows.Next(){var id string;if err=rows.Scan(&id);err!=nil{return nil,err};ids=append(ids,id)}
 return ids,rows.Err()
}

// RecordInsulaVM connects a running action to its disposable InsulaVM instance.
// The VM itself is never managed by this store.
func (s *Store) RecordInsulaVM(ctx context.Context,actionID,vmID string)error{
 if vmID==""{return errors.New("INVALID_ARGUMENT: InsulaVM id required")}
 r,err:=s.DB.ExecContext(ctx,`UPDATE action_runs SET insula_vm_id=?
  WHERE action_run_id=? AND status='running' AND insula_vm_id IS NULL`,
  vmID,actionID)
 return checkSingleRow(r,err)
}

type ActionRuntimeMessage struct {
 ID int64 `json:"id"`
 Stage string `json:"stage"`
 Message string `json:"message"`
 RecordedAt string `json:"recorded_at"`
}

func (s *Store) AddActionRuntimeMessage(ctx context.Context,actionID,stage,message string)error{
 if actionID==""||stage==""{return errors.New("INVALID_ARGUMENT: action and stage required")}
 _,err:=s.DB.ExecContext(ctx,`INSERT INTO action_runtime_messages
  (action_run_id,stage,message,recorded_at)
  VALUES(?,?,?,?)`,actionID,stage,message,now())
 return err
}

func (s *Store) ReadActionRuntimeMessages(ctx context.Context,actionID string)([]ActionRuntimeMessage,error){
 rows,err:=s.DB.QueryContext(ctx,`SELECT id,stage,message,recorded_at
  FROM action_runtime_messages WHERE action_run_id=?
  ORDER BY id ASC LIMIT 500`,actionID)
 if err!=nil{return nil,err}
 defer rows.Close()
 messages:=[]ActionRuntimeMessage{}
 for rows.Next(){
  var m ActionRuntimeMessage
  if err=rows.Scan(&m.ID,&m.Stage,&m.Message,&m.RecordedAt);err!=nil{return nil,err}
  messages=append(messages,m)
 }
 if err=rows.Err();err!=nil{return nil,fmt.Errorf("read action VM events: %w",err)}
 return messages,nil
}

// If InsulaVM cannot confirm teardown, a completed execution is not fully
// successful. Preserve job/command results but mark the action as failed.
func (s *Store) MarkActionCleanupFailed(ctx context.Context,actionID string)error{
 _,err:=s.DB.ExecContext(ctx,`UPDATE action_runs SET status='failed',finished_at=?
  WHERE action_run_id=? AND status IN ('running','succeeded')`,now(),actionID)
 return err
}

type InterruptedInsulaVM struct {
 ActionID string
 VMID string
}

// Startup reconciliation only touches VMs attached to this GitMaster's
// interrupted actions, never unrelated InsulaVM instances.
func (s *Store) InterruptedInsulaVMs(ctx context.Context)([]InterruptedInsulaVM,error){
 rows,err:=s.DB.QueryContext(ctx,`SELECT action_run_id,insula_vm_id
  FROM action_runs WHERE insula_vm_id IS NOT NULL AND status='interrupted'`)
 if err!=nil{return nil,err}
 defer rows.Close()
 items:=[]InterruptedInsulaVM{}
 for rows.Next(){var item InterruptedInsulaVM
  if err=rows.Scan(&item.ActionID,&item.VMID);err!=nil{return nil,err}
  items=append(items,item)
 }
 return items,rows.Err()
}
