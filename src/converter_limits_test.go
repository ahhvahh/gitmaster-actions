package gitmasteractions

import (
    "fmt"
    "strings"
    "testing"
)

func TestConvertYAMLMaxGitMasterJobsAndSteps(t *testing.T) {
    var yaml strings.Builder
    yaml.WriteString("version: 1\nname: max-workflow\nvm_profile: ci-smoke\njobs:\n")
    for job:=0;job<100;job++{
        fmt.Fprintf(&yaml,"  job%03d:\n    steps:\n",job)
        for step:=0;step<100;step++{
            fmt.Fprintf(&yaml,"      - name: step%03d\n        run: echo %03d-%03d\n",step,job,step)
        }
    }
    if yaml.Len()>1<<20 {t.Fatalf("fixture exceeds parser 1 MiB limit: %d",yaml.Len())}
    out,err:=ConvertYAML([]byte(yaml.String()),ConvertOptions{Format:FormatGitMasterV1})
    if err!=nil{t.Fatal(err)}
    if len(out)!=10000{t.Fatalf("want 10000 command rows, got %d",len(out))}
    for i,item:=range out{
        wantJob:=fmt.Sprintf("job%03d",i/100)
        wantCmd:=fmt.Sprintf("echo %03d-%03d",i/100,i%100)
        if item.Job!=wantJob||item.Command!=wantCmd{
            t.Fatalf("out-of-order command at %d: %#v, want %s / %s",i,item,wantJob,wantCmd)
        }
    }
}

func TestConvertYAMLMaxActionV1JobsAndCommands(t *testing.T) {
    var yaml strings.Builder
    yaml.WriteString("schema_version: 1\nvm:\n  profile: ci-smoke\naction:\n  name: max-legacy\n  jobs:\n")
    for job:=0;job<128;job++{
        fmt.Fprintf(&yaml,"    - name: job%03d\n      commands:\n",job)
        for step:=0;step<256;step++{
            fmt.Fprintf(&yaml,"        - echo %03d-%03d\n",job,step)
        }
    }
    if yaml.Len()>1<<20 {t.Fatalf("fixture exceeds parser 1 MiB limit: %d",yaml.Len())}
    out,err:=ConvertYAML([]byte(yaml.String()),ConvertOptions{Format:FormatActionV1})
    if err!=nil{t.Fatal(err)}
    if len(out)!=128*256{t.Fatalf("want 32768 command rows, got %d",len(out))}
    for i,item:=range out{
        wantJob:=fmt.Sprintf("job%03d",i/256)
        wantCmd:=fmt.Sprintf("echo %03d-%03d",i/256,i%256)
        if item.Job!=wantJob||item.Command!=wantCmd{
            t.Fatalf("out-of-order legacy command at %d: %#v, want %s / %s",i,item,wantJob,wantCmd)
        }
    }
}

// Verify exported facade aliases remain usable by external GitMaster consumers
// without importing the internal /src module or initializing an Engine.
