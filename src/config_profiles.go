package gitmasteractions

import (
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"

    "gopkg.in/yaml.v3"
)

// ConfiguredVMProfile is a host-owned source image, not a repository-controlled setting.
type ConfiguredVMProfile struct {
    Name string `yaml:"name" json:"name"`
    ImagePath string `yaml:"image_path" json:"image_path"`
    MemoryMiB int `yaml:"memory_mib" json:"memory_mib"`
    VCPUs int `yaml:"vcpus" json:"vcpus"`
    Workspace string `yaml:"workspace" json:"workspace"`
}
type VMProfilesConfig struct {
    Profiles []ConfiguredVMProfile `yaml:"profiles" json:"profiles"`
}

// LoadVMProfilesConfig validates the catalog and the QCOW2 file headers.
// InsulaVM registers each missing profile by copying the frozen base image once.
func LoadVMProfilesConfig(filename string)(VMProfilesConfig,error){
    var catalog VMProfilesConfig
    info,err:=os.Lstat(filename)
    if err!=nil{return catalog,err}
    if !info.Mode().IsRegular()||info.Size()==0||info.Size()>1<<20 {
        return catalog,errors.New("profiles config must be a regular YAML file <=1 MiB")
    }
    file,err:=os.Open(filename)
    if err!=nil{return catalog,err}
    defer file.Close()
    d:=yaml.NewDecoder(file)
    d.KnownFields(true)
    if err:=d.Decode(&catalog);err!=nil{return catalog,fmt.Errorf("decode profiles YAML: %w",err)}
    var extra any
    if err:=d.Decode(&extra);!errors.Is(err,io.EOF){
        if err==nil{return catalog,errors.New("profiles YAML must have exactly one document")}
        return catalog,err
    }
    if len(catalog.Profiles)==0||len(catalog.Profiles)>64{
        return catalog,errors.New("profiles requires 1..64 entries")
    }
    seen:=map[string]bool{}
    for _,p:=range catalog.Profiles {
        if !safeWorkflowName.MatchString(p.Name)||seen[p.Name]{
            return catalog,fmt.Errorf("invalid or duplicated profile name %q",p.Name)
        }
        seen[p.Name]=true
        if !filepath.IsAbs(p.ImagePath)||strings.ToLower(filepath.Ext(p.ImagePath))!=".qcow2"{
            return catalog,fmt.Errorf("profile %s: image_path must be an absolute .qcow2 file",p.Name)
        }
        if p.MemoryMiB<128||p.MemoryMiB>1048576||p.VCPUs<1||p.VCPUs>256{
            return catalog,fmt.Errorf("profile %s: memory_mib or vcpus out of InsulaVM bounds",p.Name)
        }
        if p.Workspace==""||p.Workspace=="/"||!strings.HasPrefix(p.Workspace,"/")||strings.ContainsAny(p.Workspace,"\\\x00\r\n"){
            return catalog,fmt.Errorf("profile %s: workspace must be an absolute guest directory other than /",p.Name)
        }
        clean:=filepath.Clean(p.Workspace)
        if clean!=p.Workspace{return catalog,fmt.Errorf("profile %s: workspace must be normalized",p.Name)}
        info,err:=os.Lstat(p.ImagePath)
        if err!=nil{return catalog,fmt.Errorf("profile %s QCOW2: %w",p.Name,err)}
        if !info.Mode().IsRegular(){return catalog,fmt.Errorf("profile %s: QCOW2 must be a regular file, not a symlink",p.Name)}
        f,err:=os.Open(p.ImagePath)
        if err!=nil{return catalog,err}
        var magic [4]byte
        _,readErr:=io.ReadFull(f,magic[:])
        closeErr:=f.Close()
        if err:=errors.Join(readErr,closeErr);err!=nil{return catalog,err}
        if magic!=[4]byte{'Q','F','I',0xfb}{
            return catalog,fmt.Errorf("profile %s: invalid QCOW2 magic",p.Name)
        }
    }
    return catalog,nil
}
