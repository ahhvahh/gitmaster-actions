package gitmasteractions

import (
    "context"
    "errors"
    "fmt"
    "math/rand/v2"
    "time"

    "github.com/ahhvahh/InsulaVM/insulavm"
)

const maxVMBusyRetries = 5 // Five RETRIES in addition to the initial attempt.

func randomBusyDelay() time.Duration {
    return time.Duration(10+rand.IntN(991))*time.Millisecond
}

// runWithBusyRetry retries ONLY InsulaVM's typed contention error.
// Remote command failures, canceled contexts and other transport errors are
// never retried (replaying a non-idempotent shell command is unsafe).
func runWithBusyRetry(ctx context.Context, call func()(int,error),
    onRetry func(attempt int,timeToWait time.Duration), delayFn func()time.Duration)(int,error){
    if ctx==nil{return -1,errors.New("context is required")}
    if call==nil||delayFn==nil{return -1,errors.New("command and delay generator are required")}
    for attempt:=0;attempt<=maxVMBusyRetries;attempt++ {
        if err:=ctx.Err();err!=nil{return -1,err}
        code,err:=call()
        if err==nil||!errors.Is(err,insulavm.ErrVMExecBusy){return code,err}
        if attempt==maxVMBusyRetries{
            return -1,fmt.Errorf("VM remained busy after %d attempts: %w",maxVMBusyRetries+1,err)
        }
        pause:=delayFn()
        if pause<10*time.Millisecond||pause>1000*time.Millisecond{
            return -1,errors.New("invalid busy retry delay (must be 10..1000ms)")
        }
        if onRetry!=nil{onRetry(attempt+1,pause)}
        ticker:=time.NewTimer(pause)
        select{
        case <-ctx.Done():
            if !ticker.Stop(){<-ticker.C}
            return -1,ctx.Err()
        case <-ticker.C:
        }
    }
    return -1,errors.New("unreachable retry state")
}
