package gitmasteractions

import (
    "archive/zip"
    "bytes"
    "context"
    "io"
        "os"
    "path/filepath"
    "strings"
    "sync"
    "testing"
)

func testProfileCatalog(t *testing.T)(string,string){
    t.Helper()
    dir:=t.TempDir()
    image:=filepath.Join(dir,"frozen.qcow2")
    if err:=os.WriteFile(image,[]byte{'Q','F','I',0xfb},0600);err!=nil{t.Fatal(err)}
    host:=filepath.Join(dir,"insula.yaml")
    if err:=os.WriteFile(host,[]byte("storage:\n  root: "+filepath.Join(dir,"storage")+"\nguest:\n  destination: /tmp/legacy\n"),0600);err!=nil{t.Fatal(err)}
    yaml:=filepath.Join(dir,"profiles.yaml")
    content:="profiles:\n"+
        "  - name: ci-small\n    image_path: "+image+"\n    memory_mib: 1024\n    vcpus: 1\n    workspace: /tmp/workspace\n"+
        "  - name: ci-large\n    image_path: "+image+"\n    memory_mib: 2048\n    vcpus: 2\n    workspace: /tmp/other\n"
    if err:=os.WriteFile(yaml,[]byte(content),0600);err!=nil{t.Fatal(err)}
    return host,yaml
}
func TestFrozenProfilesRegisteredAndWorkspaceResolved(t *testing.T) {
    host,yaml:=testProfileCatalog(t)
    catalog,err:=LoadVMProfilesConfig(yaml)
    if err!=nil||len(catalog.Profiles)!=2{t.Fatalf("catalog: %#v %v",catalog,err)}
    rt,err:=NewRuntimeWithProfiles(host,yaml)
    if err!=nil{t.Fatal(err)}
    if err:=rt.Profile("ci-small");err!=nil{t.Fatal(err)}
    if rt.Workspace()!="/tmp/workspace"{t.Fatalf("workspace %q",rt.Workspace())}
    if err:=rt.Profile("ci-large");err!=nil{t.Fatal(err)}
    if rt.Workspace()!="/tmp/other"{t.Fatalf("workspace %q",rt.Workspace())}
    if err:=rt.Profile("unlisted");err==nil{t.Fatal("unlisted profile accepted")}
    reloaded,err:=NewRuntimeWithProfiles(host,yaml)
    if err!=nil{t.Fatal(err)}
    if err:=reloaded.Profile("ci-small");err!=nil{t.Fatalf("registered profile should be reusable: %v",err)}
    content,err:=os.ReadFile(yaml)
    if err!=nil{t.Fatal(err)}
    changed:=strings.Replace(string(content),"memory_mib: 1024","memory_mib: 4096",1)
    if err:=os.WriteFile(yaml,[]byte(changed),0600);err!=nil{t.Fatal(err)}
    mismatch,err:=NewRuntimeWithProfiles(host,yaml)
    if err!=nil{t.Fatal(err)}
    if err:=mismatch.Profile("ci-small");err==nil{t.Fatal("changed profile settings silently reused frozen image")}
    if err:=os.WriteFile(yaml,content,0600);err!=nil{t.Fatal(err)}
    // Direct-boot installer profiles cannot use SSH ImportToVM/ExecVM.
    profileFile:=filepath.Join(filepath.Dir(host),"storage","profiles","ci-small","profile.yaml")
    profileYAML,err:=os.ReadFile(profileFile)
    if err!=nil{t.Fatal(err)}
    profileYAML=bytes.Replace(profileYAML,[]byte("name: ci-small"),[]byte("name: ci-small\nboot_mode: direct"),1)
    if err:=os.WriteFile(profileFile,profileYAML,0600);err!=nil{t.Fatal(err)}
    direct,err:=NewRuntimeWithProfiles(host,yaml)
    if err!=nil{t.Fatal(err)}
    if err:=direct.Profile("ci-small");err==nil{t.Fatal("direct-boot profile accepted for SSH ZIP execution")}
}
func TestFrozenProfilesStrictValidation(t *testing.T) {
    _,catalogPath:=testProfileCatalog(t)
    raw,err:=os.ReadFile(catalogPath)
    if err!=nil{t.Fatal(err)}
    cases:=[]string{
        strings.Replace(string(raw),"/tmp/workspace","../outside",1),
        strings.Replace(string(raw),"vcpus: 1","vcpus: 0",1),
        strings.Replace(string(raw),"name: ci-large","name: ci-small",1),
        strings.Replace(string(raw),".qcow2",".raw",1),
        string(raw)+"unknown: value\n",
        string(raw)+"---\nprofiles: []\n",
    }
    for _,c:=range cases{
        if err:=os.WriteFile(catalogPath,[]byte(c),0600);err!=nil{t.Fatal(err)}
        if _,err:=LoadVMProfilesConfig(catalogPath);err==nil{t.Fatalf("invalid YAML accepted: %q",c)}
    }
}

func makeZipSnapshot(t *testing.T,name string,entries map[string]string) string {
    t.Helper()
    file:=filepath.Join(t.TempDir(),name)
    f,err:=os.Create(file)
    if err!=nil{t.Fatal(err)}
    zw:=zip.NewWriter(f)
    for filename,body:=range entries {
        w,err:=zw.Create(filename)
        if err!=nil{t.Fatal(err)}
        if _,err=w.Write([]byte(body));err!=nil{t.Fatal(err)}
    }
    if err:=zw.Close();err!=nil{t.Fatal(err)}
    if err:=f.Close();err!=nil{t.Fatal(err)}
    return file
}
func TestZipSnapshotValidationAndSafety(t *testing.T){
    good:=makeZipSnapshot(t,"source.zip",map[string]string{"src/main.go":"package main","README.md":"ok"})
    if err:=safeSnapshotArchive(good);err!=nil{t.Fatal(err)}
    for _,name:=range []string{"../../escape",".ssh/id_rsa","src/.env","a/../b"}{
        path:=makeZipSnapshot(t,"unsafe.zip",map[string]string{name:"no"})
        if err:=safeSnapshotArchive(path);err==nil{t.Fatalf("unsafe ZIP entry %q accepted",name)}
    }
    path:=filepath.Join(t.TempDir(),"symlink.zip")
    f,err:=os.Create(path)
    if err!=nil{t.Fatal(err)}
    zw:=zip.NewWriter(f)
    h:=&zip.FileHeader{Name:"link"}
    h.SetMode(os.ModeSymlink|0777)
    w,err:=zw.CreateHeader(h)
    if err!=nil{t.Fatal(err)}
    _,err=w.Write([]byte("target"))
    if err!=nil{t.Fatal(err)}
    if err:=zw.Close();err!=nil{t.Fatal(err)}
    if err:=f.Close();err!=nil{t.Fatal(err)}
    if err:=safeSnapshotArchive(path);err==nil{t.Fatal("symlink ZIP entry accepted")}
}
type zipTrackingRuntime struct {
    *testRuntime
    muZip sync.Mutex
    deployed bool
}
func (z *zipTrackingRuntime) Deploy(_ context.Context,_ string,source,_ string,_ io.Writer) error {
    z.muZip.Lock()
    z.deployed=strings.HasSuffix(source,".zip")
    z.muZip.Unlock()
    return safeSnapshotArchive(source)
}
func TestZipSubmissionPersistsJobsCommandsAndLogs(t *testing.T){
    zipFile:=makeZipSnapshot(t,"repo.zip",map[string]string{"src/main.go":"package main"})
    rt:=&zipTrackingRuntime{testRuntime:&testRuntime{}}
    engine,err:=New(Options{DatabasePath:filepath.Join(t.TempDir(),"actions.db"),RuntimeFactory:func()(Runtime,error){return rt,nil}})
    if err!=nil{t.Fatal(err)}
    defer engine.Close()
    req:=SubmitRequest{YAML:[]byte(gitmasterWorkflow),Format:FormatGitMasterV1,ArchivePath:zipFile,Origin:originTest()}
    a,err:=engine.SubmitYAML(context.Background(),req)
    if err!=nil{t.Fatal(err)}
    completed,err:=engine.WaitAction(context.Background(),a.ID)
    if err!=nil||completed.Status!="succeeded"{t.Fatalf("result: %#v %v",completed,err)}
    rt.muZip.Lock()
    uploaded:=rt.deployed
    rt.muZip.Unlock()
    if !uploaded {t.Fatal("ZIP snapshot was not transferred")}
    details,err:=engine.GetActionDetails(context.Background(),a.ID)
    if err!=nil{t.Fatal(err)}
    if details.MaterialSHA256==""||len(details.Jobs)!=2||len(details.Jobs[0].Commands)!=1{
        t.Fatalf("action graph not persisted: %#v",details)
    }
    for _,j:=range details.Jobs {
        for _,command:=range j.Commands{
            if command.Command.Status!="succeeded"{t.Fatalf("command status: %#v",command)}
            logs,err:=engine.ListCommandLogs(context.Background(),command.Command.ID,0)
            if err!=nil||len(logs)==0{t.Fatalf("command output missing: %v %#v",err,logs)}
        }
    }
}
func TestManualBranchFilters(t *testing.T){
    p,err:=parseSubmission(SubmitRequest{Format:FormatGitMasterV1,YAML:[]byte(gitmasterWorkflow),
        Origin:ActionOrigin{Branch:"develop",EventType:"manual"}})
    if err!=nil||len(p.Jobs)!=2{t.Fatalf("manual branch workflow: %#v %v",p,err)}
    if err:=validateOrigin(ActionOrigin{Branch:"../secret",EventType:"manual"});err==nil{t.Fatal("unsafe branch accepted")}
    if err:=validateOrigin(ActionOrigin{Branch:"develop",CommitSHA:"123"});err==nil{t.Fatal("incomplete Git identity accepted")}
}
func TestZipSnapshotStagedWithCorrectExtension(t *testing.T){
    a:=makeZipSnapshot(t,"repo.zip",map[string]string{"README":"content"})
    engine,err:=New(Options{DatabasePath:filepath.Join(t.TempDir(),"actions.db")})
    if err!=nil{t.Fatal(err)}
    defer engine.Close()
    dest,hash,err:=engine.stageSnapshot(context.Background(),a)
    if err!=nil{t.Fatal(err)}
    defer os.Remove(dest)
    if !strings.HasSuffix(dest,".zip")||hash==""{t.Fatalf("staging: %s %s",dest,hash)}
    data,err:=os.ReadFile(dest)
    if err!=nil{t.Fatal(err)}
    original,err:=os.ReadFile(a)
    if err!=nil{t.Fatal(err)}
    if !bytes.Equal(data,original){t.Fatal("staged bytes changed")}
    if err:=safeZipSnapshot(dest);err!=nil{t.Fatal(err)}
    if _,_,err=engine.stageSnapshot(context.Background(),"../repo.zip");err==nil{t.Fatal("relative source path accepted")}
}

