package gitmasteractions

import (
    "bytes"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"

    "gopkg.in/yaml.v3"
)
type manifest struct {
    SchemaVersion int `yaml:"schema_version"`
    VM struct{
        Profile string `yaml:"profile"`
        MemoryMiB int `yaml:"memory_mib"`
        VCPUs int `yaml:"vcpus"`
    }`yaml:"vm"`
    Action struct {
        Name string `yaml:"name"`
        Imports []struct{Source string `yaml:"source"`} `yaml:"imports"`
        Jobs []struct{
            Name string `yaml:"name"`
            Commands []string `yaml:"commands"`
            AllowFailure bool `yaml:"allow_failure"`
        }`yaml:"jobs"`
    }`yaml:"action"`
    Logging struct{CaptureStdin bool `yaml:"capture_stdin"`}`yaml:"logging"`
}
func parseManifest(data []byte,baseDir string)(manifest,error){
    var m manifest
    if len(data)==0||len(data)>1<<20{return m,errors.New("YAML must be 1..1048576 bytes")}
    decoder:=yaml.NewDecoder(bytes.NewReader(data))
    decoder.KnownFields(true)
    if err:=decoder.Decode(&m);err!=nil{return m,err}
    var extra any
    if err:=decoder.Decode(&extra);!errors.Is(err,io.EOF){
        if err==nil{return m,errors.New("YAML must contain one document")}
        return m,err
    }
    if m.SchemaVersion!=1{return m,errors.New("schema_version must be 1")}
    if strings.TrimSpace(m.VM.Profile)==""||strings.TrimSpace(m.Action.Name)==""{
        return m,errors.New("vm.profile and action.name are required")
    }
    if m.VM.MemoryMiB!=0||m.VM.VCPUs!=0 {
        return m,errors.New("runtime resource overrides require a future InsulaVM API; use configured profile values")
    }
    if m.Logging.CaptureStdin{return m,errors.New("stdin capture is not supported by current InsulaVM ExecVM")}
    if len(m.Action.Jobs)==0||len(m.Action.Jobs)>128 {return m,errors.New("action.jobs requires 1..128 jobs")}
    seen:=map[string]bool{}
    for _,job:=range m.Action.Jobs {
        if strings.TrimSpace(job.Name)==""||seen[job.Name] {return m,fmt.Errorf("invalid or repeated job name %q",job.Name)}
        seen[job.Name]=true
        if len(job.Commands)==0||len(job.Commands)>256 {return m,fmt.Errorf("job %q must have 1..256 commands",job.Name)}
        for _,c:=range job.Commands{
            if strings.TrimSpace(c)==""||strings.ContainsRune(c,0) {return m,fmt.Errorf("invalid command in job %q",job.Name)}
        }
    }
    if len(m.Action.Imports)>8{return m,errors.New("too many imports")}
    for i,item:=range m.Action.Imports{
        source:=item.Source
        if !filepath.IsAbs(source) {
            if baseDir==""{return m,errors.New("RunYAML imports must be absolute paths")}
            source=filepath.Join(baseDir,source)
        }
        source,err:=filepath.Abs(source)
        if err!=nil{return m,err}
        if !strings.HasSuffix(strings.ToLower(source),".tar.gz") && !strings.HasSuffix(strings.ToLower(source),".zip") {return m,errors.New("imports must be .tar.gz or .zip archives")}
        fi,err:=os.Lstat(source)
        if err!=nil{return m,err}
        if !fi.Mode().IsRegular() {return m,errors.New("import must be a regular file")}
        if fi.Size()>512<<20 {return m,errors.New("import exceeds 512 MiB")}
        m.Action.Imports[i].Source=source
    }
    return m,nil
}
