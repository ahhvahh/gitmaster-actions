package gitmasteractions

import (
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sort"

    "github.com/ahhvahh/InsulaVM/insulavm"
)

// ConfigurationFingerprint hashes only administrator-owned VM inputs. Consumers
// must additionally bind the result to Git SHA, clone, event and workflows.
func ConfigurationFingerprint(configPath string, profileNames []string, startupScripts map[string]string)(string,error){
    if configPath==""{return "",errors.New("VALIDATION_UNAVAILABLE: InsulaVM config path required")}
    conf,err:=os.ReadFile(configPath)
    if err!=nil{return "",err}
    e,err:=insulavm.New(configPath)
    if err!=nil{return "",err}
    h:=sha256.New()
    _,_ = fmt.Fprintf(h,"gitmaster-actions-runtime-v1\nconfig=%s\n",configPath)
    _,_ = h.Write(conf)
    unique:=map[string]bool{}
    for _,n:=range profileNames{if n!=""{unique[n]=true}}
    names:=make([]string,0,len(unique))
    for n:=range unique{names=append(names,n)}
    sort.Strings(names)
    for _,name:=range names{
        p,err:=e.GetProfile(name)
        if err!=nil{return "",fmt.Errorf("profile %q: %w",name,err)}
        b,err:=os.ReadFile(filepath.Join(p.Directory,"profile.yaml"))
        if err!=nil{return "",err}
        _,_ = fmt.Fprintf(h,"profile=%s\n",name)
        _,_ = h.Write(b)
        image,err:=p.ImagePath()
        if err!=nil{return "",err}
        st,err:=os.Stat(image)
        if err!=nil{return "",err}
        _,_ = fmt.Fprintf(h,"qcow2=%s\nsize=%d\nmodtime=%d\n",image,st.Size(),st.ModTime().UnixNano())
        if startup:=startupScripts[name];startup!=""{
            st,err:=os.Lstat(startup)
            if err!=nil{return "",err}
            if !st.Mode().IsRegular(){return "",fmt.Errorf("startup script for %q is not regular",name)}
            b,err:=os.ReadFile(startup)
            if err!=nil{return "",err}
            _,_ = fmt.Fprintf(h,"startup=%s\n",startup)
            _,_ = h.Write(b)
        }
    }
    return hex.EncodeToString(h.Sum(nil)),nil
}
