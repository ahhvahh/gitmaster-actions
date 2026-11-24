package gitmasteractions

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "github.com/ahhvahh/InsulaVM/insulavm"
)

type VMImage struct {
    Name string `json:"name"`
    Format string `json:"format"`
    SizeBytes int64 `json:"size_bytes"`
}
type VMProfile struct {
    Name string `json:"name"`
    OS string `json:"os"`
    Version string `json:"version"`
    Image string `json:"image"`
    VCPUs int `json:"vcpus"`
    MemoryMiB int `json:"memory_mib"`
    DiskGiB int `json:"disk_gib"`
    ManagedBy string `json:"managed_by"`
    Workspace string `json:"workspace"`
}
type ProfileRequest struct {
    OS string `json:"os,omitempty"`
    Name string `json:"name"`
    Image string `json:"image"`
    VCPUs int `json:"vcpus"`
    MemoryMiB int `json:"memory_mib"`
    DiskGiB int `json:"disk_gib"`
}
func ImageFormat(name string)string{
    switch strings.ToLower(filepath.Ext(name)){
    case ".iso":return "iso"
    case ".qcow2":return "qcow2"
    case ".img":return "raw"
    }
    return ""
}
// ListVMImages only inspects regular files inside an administrator-owned dir.
func ListVMImages(dir string)([]VMImage,error){
    entries,err:=os.ReadDir(dir)
    if errors.Is(err,os.ErrNotExist){return []VMImage{},nil}
    if err!=nil{return nil,fmt.Errorf("read VM images: %w",err)}
    out:=[]VMImage{}
    for _,entry:=range entries{
        if ImageFormat(entry.Name())==""||!entry.Type().IsRegular(){continue}
        info,err:=entry.Info()
        if err!=nil{return nil,err}
        if !info.Mode().IsRegular(){continue}
        out=append(out,VMImage{Name:entry.Name(),Format:ImageFormat(entry.Name()),SizeBytes:info.Size()})
    }
    sort.Slice(out,func(i,j int)bool{return out[i].Name<out[j].Name})
    return out,nil
}
func ValidImageFile(dir,name string)(bool,error){
    if name==""||filepath.Base(name)!=name||name=="."||name==".."||
        strings.ContainsAny(name,"\x00\r\n\\")||ImageFormat(name)==""{
        return false,errors.New("INVALID_ARGUMENT: image must be a supported filename")
    }
    info,err:=os.Lstat(filepath.Join(dir,name))
    if errors.Is(err,os.ErrNotExist){return false,nil}
    if err!=nil{return false,err}
    return info.Mode().IsRegular(),nil
}
func profileInfo(p *insulavm.Profile) VMProfile {
    return VMProfile{Name:p.Name,OS:p.OS,Version:p.Version,Image:p.Image,
        VCPUs:p.VCPUs,MemoryMiB:p.MemoryMiB,DiskGiB:p.DiskGB,ManagedBy:"InsulaVM",Workspace:p.GuestDestination("")}
}
func ListVMProfiles(configPath string)([]VMProfile,error){
    e,err:=insulavm.New(configPath)
    if err!=nil{return nil,err}
    names,err:=e.ListProfiles()
    if err!=nil{return nil,err}
    out:=make([]VMProfile,0,len(names))
    for _,name:=range names{
        p,err:=e.GetProfile(name)
        if err!=nil{return nil,err}
        out=append(out,profileInfo(p))
    }
    return out,nil
}
func RegisterVMProfile(configPath,imagesDir string,req ProfileRequest)(VMProfile,error){
    if filepath.Ext(req.Image)!=".qcow2"{return VMProfile{},errors.New("INVALID_ARGUMENT: QCOW2 image required")}
    ok,err:=ValidImageFile(imagesDir,req.Image)
    if err!=nil{return VMProfile{},err}
    if !ok{return VMProfile{},errors.New("NOT_FOUND: approved QCOW2 image not found")}
    e,err:=insulavm.New(configPath)
    if err!=nil{return VMProfile{},err}
    p:=insulavm.Profile{Name:req.Name,OS:req.OS,MemoryMiB:req.MemoryMiB,VCPUs:req.VCPUs,DiskGB:req.DiskGiB}
    if err=e.RegisterProfile(p,filepath.Join(imagesDir,req.Image));err!=nil{return VMProfile{},err}
    saved,err:=e.GetProfile(req.Name)
    if err!=nil{return VMProfile{},err}
    return profileInfo(saved),nil
}
