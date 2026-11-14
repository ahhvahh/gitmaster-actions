package gitmasteractions

import (
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/ahhvahh/InsulaVM/insulavm"
    "golang.org/x/crypto/ssh"
)

// Runtime is the sole VM boundary used by library, CLI and GitMaster.
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
    profiles map[string]ConfiguredVMProfile
    workspace string
}
var registerConfiguredProfileMu sync.Mutex

// NewRuntime uses existing InsulaVM profiles.
func NewRuntime(configPath string, scripts ...map[string]string) (Runtime,error) {
    return NewRuntimeWithProfiles(configPath,"",scripts...)
}

// NewRuntimeWithProfiles optionally provisions missing registered profiles from
// administrator-owned frozen QCOW2 images. The YAML never comes from the repository.
func NewRuntimeWithProfiles(configPath, profilesConfigPath string, scripts ...map[string]string) (Runtime,error) {
    if configPath == "" { return nil, errors.New("InsulaVM config path required") }
    e,err:=insulavm.New(configPath)
    if err!=nil{return nil,err}
    var configured map[string]string
    if len(scripts)>0 { configured=scripts[0] }
    r:=&vmRuntime{engine:e,startupScripts:configured,workspace:e.Configuration().Guest.Destination}
    if profilesConfigPath!="" {
        catalog,err:=LoadVMProfilesConfig(profilesConfigPath)
        if err!=nil{return nil,err}
        r.profiles=make(map[string]ConfiguredVMProfile,len(catalog.Profiles))
        for _,p:=range catalog.Profiles {r.profiles[p.Name]=p}
    }
    return r,nil
}

func (r *vmRuntime) Profile(name string) error {
    if r.profiles!=nil {
        selected,ok:=r.profiles[name]
        if !ok{return fmt.Errorf("profile %q is not in the administrator catalog",name)}
        registerConfiguredProfileMu.Lock()
        existing,err:=r.engine.GetProfile(name)
        if errors.Is(err,os.ErrNotExist) {
            p:=insulavm.Profile{Name:selected.Name,MemoryMiB:selected.MemoryMiB,VCPUs:selected.VCPUs}
            p.Workspace.Destination=selected.Workspace
            err=r.engine.RegisterProfile(p,selected.ImagePath)
            if err==nil {existing,err=r.engine.GetProfile(name)}
        }
        registerConfiguredProfileMu.Unlock()
        if err!=nil{return fmt.Errorf("configured profile %s: %w",name,err)}
        // InsulaVM's direct-boot profiles may only offer QGA, whereas
        // ZIP import and command output here require managed SSH.
        if existing.BootMode=="direct"{
            return fmt.Errorf("profile %q uses direct boot; GitMaster Actions requires a Cloud-Init/SSH-enabled frozen image",name)
        }
        if existing.MemoryMiB!=selected.MemoryMiB||existing.VCPUs!=selected.VCPUs||
           existing.GuestDestination("")!=selected.Workspace {
            return fmt.Errorf("registered profile %q differs from administrator YAML; create a new profile name or explicitly replace it in InsulaVM",name)
        }
    } else if _,err:=r.engine.GetProfile(name);err!=nil{return err}
    workspace,err:=r.engine.ResolveVMWorkspace(name)
    if err!=nil{return err}
    r.workspace=workspace
    return nil
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
    if filepath.Ext(source)==".zip" {
        // InsulaVM DeployToVM extracts only TAR.GZ. For ZIP, use its supported
        // ImportToVM + ExecVM operations with the profile's workspace.
        imported,err:=r.engine.ImportToVM(ctx,insulavm.VMImportRequest{VMID:id,SourcePath:source,Output:out})
        if err!=nil{return err}
        if imported==nil||!imported.Imported{return errors.New("InsulaVM did not confirm ZIP import")}
        filename:=filepath.Base(source) // stageSnapshot generates a safe basename.
        _,err=r.engine.ExecVM(ctx,insulavm.ExecRequest{
            VMID:id, Command:"unzip -q -o "+quoteWorkspaceArg(filename),
            Output:out,CommandTimeout:10*time.Minute,
        })
        return err
    }
    _,err:=r.engine.DeployToVM(ctx,insulavm.VMDeployRequest{
        VMID:id,SourcePath:source,ScriptPath:script,Extract:true,
        Output:out,CommandTimeout:10*time.Minute,
    })
    return err
}

// quoteWorkspaceArg quotes a single guest argument, never a shell fragment.
func quoteWorkspaceArg(s string) string {
    q:="'"
    for _,ch:=range s {
        if ch=='\'' {q+="'\\''"} else {q+=string(ch)}
    }
    return q+"'"
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
func (r *vmRuntime) Workspace()string{return r.workspace}
