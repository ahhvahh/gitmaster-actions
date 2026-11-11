package gitmasteractions

import (
    "context"
    "errors"
    "fmt"
    "io"
    "time"

    "github.com/ahhvahh/InsulaVM/insulavm"
    "golang.org/x/crypto/ssh"
)

// Runtime is the sole VM boundary shared by the library, its CLI and GitMaster.
// Files and commands supplied here must already be authorized by the caller.
// All QEMU/Libvirt, SSH, networking and guest transfers belong to InsulaVM.
type Runtime interface {
    Profile(name string) error
    Start(ctx context.Context, name string, progress func(stage,message string)) (string,error)
    Deploy(ctx context.Context, id, source, script string, output io.Writer) error
    Command(ctx context.Context, id, dir, script string, output io.Writer) (int,error)
    Stop(ctx context.Context, id string) error
    Workspace() string
}

type vmRuntime struct {
    engine *insulavm.Engine
    startupScripts map[string]string
}

// NewRuntime never accepts a bootstrap script from an untrusted Action YAML.
// Scripts are administrator-supplied paths keyed by existing VM profile.
func NewRuntime(configPath string, scripts ...map[string]string) (Runtime,error) {
    if configPath == "" { return nil, errors.New("InsulaVM config path required") }
    e,err:=insulavm.New(configPath)
    if err!=nil{return nil,err}
    var configured map[string]string
    if len(scripts)>0 { configured=scripts[0] }
    return &vmRuntime{engine:e,startupScripts:configured},nil
}
func (r *vmRuntime) Profile(name string) error {
    _,err:=r.engine.GetProfile(name)
    return err
}
func (r *vmRuntime) Start(ctx context.Context,name string, progress func(stage,message string))(string,error) {
    opts:=insulavm.VMStartOptions{Profile:name,StartupScriptPath:r.startupScripts[name]}
    instance,err:=r.engine.StartVMWithOptions(ctx,opts,func(p insulavm.VMProgress) {
        if progress!=nil {progress(p.Stage,p.Message)}
    })
    if err!=nil{return "",err}
    return instance.ID,nil
}
func (r *vmRuntime) Deploy(ctx context.Context,id,source,script string,out io.Writer)error {
    _,err:=r.engine.DeployToVM(ctx,insulavm.VMDeployRequest{
        VMID:id,SourcePath:source,ScriptPath:script,Extract:true,
        Output:out,CommandTimeout:10*time.Minute,
    })
    return err
}
func (r *vmRuntime) Command(ctx context.Context,id,dir,script string,out io.Writer)(int,error){
    _,err:=r.engine.ExecVM(ctx,insulavm.ExecRequest{
        VMID:id,Workspace:dir,Command:script,Output:out,CommandTimeout:20*time.Minute,
    })
    if err==nil{return 0,nil}
    var exit *ssh.ExitError
    if errors.As(err,&exit) {return exit.ExitStatus(),err}
    return -1,fmt.Errorf("InsulaVM command transport: %w",err)
}
func (r *vmRuntime) Stop(ctx context.Context,id string)error{return r.engine.StopVM(ctx,id)}
func (r *vmRuntime) Workspace()string{return r.engine.Configuration().Guest.Destination}
