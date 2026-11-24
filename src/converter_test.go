package gitmasteractions

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "reflect"
    "strconv"
    "strings"
    "testing"
)

// The same TSV fixture matrix is executed by both Go and CLI.
func TestConverterFixtureMatrix(t *testing.T){
    root:=".."
    for _,kind:=range []string{"valid","invalid"}{
        t.Run(kind,func(t *testing.T){
            raw,err:=os.ReadFile(filepath.Join(root,"tests","plan-cases",kind+".tsv"))
            if err!=nil{t.Fatal(err)}
            for _,line:=range strings.Split(strings.TrimSpace(string(raw)),"\n"){
                if strings.HasPrefix(line,"#")||strings.TrimSpace(line)==""{continue}
                fields:=strings.Split(line,"\t")
                if len(fields)!=6{t.Fatalf("bad TSV row: %q",line)}
                id,format,branch,event,yamlFile,wantPath:=fields[0],fields[1],fields[2],fields[3],fields[4],fields[5]
                t.Run(id,func(t *testing.T){
                    yamlData,err:=os.ReadFile(filepath.Join(root,yamlFile))
                    if err!=nil{t.Fatal(err)}
                    if branch=="-"{branch=""}
                    opts:=ConvertOptions{Format:YAMLFormat(format),Branch:branch,EventType:event}
                    got,err:=ConvertYAML(yamlData,opts)
                    if kind=="invalid"{
                        if err==nil{t.Fatalf("invalid configuration accepted: %#v",got)}
                        if !strings.Contains(err.Error(),wantPath){t.Fatalf("want error %q, got %v",wantPath,err)}
                        return
                    }
                    if err!=nil{t.Fatal(err)}
                    wantJSON,err:=os.ReadFile(filepath.Join(root,wantPath))
                    if err!=nil{t.Fatal(err)}
                    var want []CommandPair
                    if err:=json.Unmarshal(wantJSON,&want);err!=nil{t.Fatal(err)}
                    if !reflect.DeepEqual(got,want){
                        t.Fatalf("different ordered plan:\n got: %#v\nwant: %#v",got,want)
                    }
                    got2,err:=ConvertYAML(yamlData,opts)
                    if err!=nil||!reflect.DeepEqual(got,got2){t.Fatalf("not deterministic: %#v %#v %v",got,got2,err)}
                    for _,entry:=range got{
                        if entry.Job==""||entry.Command==""{t.Errorf("empty pair: %#v",entry)}
                    }
                })
            }
        })
    }
}

// Exhaustively explore 64 acyclic graphs, all 16 filter masks, two branches:
// 2048 DAG/branch/filter combinations.
func TestConverterExhaustiveDAGAndBranchMasks(t *testing.T){
    names:=[]string{"job0","job1","job2","job3"}
    edges:=[][2]int{{0,1},{0,2},{0,3},{1,2},{1,3},{2,3}}
    for edgeMask:=0;edgeMask<1<<len(edges);edgeMask++{
        depends:=make([][]int,len(names))
        for e,pair:=range edges{
            if edgeMask&(1<<e)!=0{depends[pair[1]]=append(depends[pair[1]],pair[0])}
        }
        for filterMask:=0;filterMask<1<<len(names);filterMask++{
            var yaml strings.Builder
            yaml.WriteString("version: 1\nname: generated\nvm_profile: ci-smoke\nmax_parallel_jobs: 4\njobs:\n")
            for i,name:=range names{
                fmt.Fprintf(&yaml,"  %s:\n",name)
                if filterMask&(1<<i)!=0{yaml.WriteString("    branches:\n      include: [main]\n")}
                if len(depends[i])!=0{
                    yaml.WriteString("    needs: [")
                    for n,dep:=range depends[i]{
                        if n>0{yaml.WriteString(", ")}
                        yaml.WriteString(names[dep])
                    }
                    yaml.WriteString("]\n")
                }
                fmt.Fprintf(&yaml,"    steps:\n      - name: run\n        run: echo %s\n",name)
            }
            for _,branch:=range []string{"develop","main"}{
                key:=fmt.Sprintf("edges=%d/filters=%d/branch=%s",edgeMask,filterMask,branch)
                t.Run(key,func(t *testing.T){
                    selected:=make([]bool,len(names))
                    for i:=range names {
                        selected[i]=branch=="main" || filterMask&(1<<i)==0
                        for _,need:=range depends[i]{
                            if !selected[need]{selected[i]=false}
                        }
                    }
                    result,err:=ConvertYAML([]byte(yaml.String()),ConvertOptions{
                        Format:FormatGitMasterV1,Branch:branch,
                    })
                    if err!=nil{t.Fatal(err)}
                    expectedCount:=0
                    for _,yes:=range selected{if yes{expectedCount++}}
                    if len(result)!=expectedCount{t.Fatalf("rows %v expected %d",result,expectedCount)}
                    seen:=map[string]int{}
                    for index,command:=range result{
                        if seen[command.Job]>0{t.Fatalf("duplicate job %s",command.Job)}
                        seen[command.Job]=index+1
                        i,err:=strconv.Atoi(strings.TrimPrefix(command.Job,"job"))
                        if err!=nil||i>=len(names)||i<0{t.Fatalf("invalid job: %#v",command)}
                        if !selected[i]{t.Fatalf("skipped job emitted: %#v",command)}
                        if command.Command!="echo "+command.Job{t.Errorf("wrong command %#v",command)}
                        for _,dep:=range depends[i]{
                            if seen[names[dep]]==0{t.Fatalf("dependency %s did not precede %s",names[dep],command.Job)}
                        }
                    }
                })
            }
        }
    }
}

func TestConverterInputLimitAndUnsupportedFormats(t *testing.T){
    _,err:=ConvertYAML([]byte(strings.Repeat("x",1<<20+1)),ConvertOptions{Format:FormatGitMasterV1})
    if err==nil{t.Fatal("oversized YAML accepted")}
    _,err=ConvertYAML([]byte("version: 1"),ConvertOptions{})
    if err==nil{t.Fatal("unspecified format accepted")}
}
