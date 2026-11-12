package gitmasteractions

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "io"
    "strings"
    "sync"
)

// OutputWriter converts the current InsulaVM command console protocol to
// stream-labelled lines. The API does not falsely claim native split streams.
type OutputWriter struct {
    mu sync.Mutex
    pending bytes.Buffer
    emit func(stream,line string)error
}
func NewOutputWriter(emit func(string,string)error)*OutputWriter {
    return &OutputWriter{emit:emit}
}
func (w *OutputWriter) Write(p []byte)(int,error) {
    w.mu.Lock()
    defer w.mu.Unlock()
    for _,b:=range p {
        if b=='\n' {
            if err:=w.emitLocked(strings.TrimSuffix(w.pending.String(),"\r"));err!=nil{return 0,err}
            w.pending.Reset()
        }else {
            if w.pending.Len() >= 4<<20 {
                return 0,errors.New("command console line exceeds 4 MiB; refusing unbounded buffering")
            }
            _=w.pending.WriteByte(b)
        }
    }
    return len(p),nil
}
func (w *OutputWriter) emitLocked(line string)error {
    stream:="system"
    if strings.HasPrefix(line,"[command][stderr] ") {
        stream="stderr";line=strings.TrimPrefix(line,"[command][stderr] ")
    }else if strings.HasPrefix(line,"[command] ") {
        stream="stdout";line=strings.TrimPrefix(line,"[command] ")
    }
    if w.emit!=nil{return w.emit(stream,line)}
    return nil
}
func(w *OutputWriter) Flush()error{
    w.mu.Lock()
    defer w.mu.Unlock()
    if w.pending.Len()==0{return nil}
    err:=w.emitLocked(w.pending.String())
    w.pending.Reset()
    return err
}

// GuestCommand uses one existing VM for all jobs of an Action. Callers own
// lifecycle and guarantee they call Stop even when an earlier step fails.
type GuestCommand struct {
    backend Runtime
    vmID string
    console io.Writer
}
func NewGuestCommand(backend Runtime,vmID string,console io.Writer)GuestCommand{
    return GuestCommand{backend:backend,vmID:vmID,console:console}
}
func(c GuestCommand)Execute(ctx context.Context,dir,script string,emit func(string,string)error)(int,error){
    if c.backend==nil{return -1,errors.New("InsulaVM backend unavailable")}
    writer:=NewOutputWriter(func(stream,line string)error{
        if c.console!=nil {
            if _,err:=fmt.Fprintf(c.console,"[InsulaVM][%s] %s\n",stream,line);err!=nil{return err}
        }
        if emit!=nil{return emit(stream,line)}
        return nil
    })
    exit,err:=c.backend.Command(ctx,c.vmID,dir,script,writer)
    return exit,errors.Join(err,writer.Flush())
}
