package main

import (
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"

 actions "github.com/ahhvahh/gitmaster-actions"
)

// Verify that the real-VM scenarios remain coherent even on a host without
// KVM. The parser, JobGraph and converter are the production implementations.
func TestVMExamplesStatic(t *testing.T) {
 root:=filepath.Join("..","..","..")
 matrix,err:=os.ReadFile(filepath.Join(root,"tests","vm-examples","cases.tsv"))
 if err!=nil{t.Fatal(err)}
 cases:=0
 for _,line:=range strings.Split(strings.TrimSpace(string(matrix)),"\n"){
  if line==""||strings.HasPrefix(line,"#"){continue}
  parts:=strings.Split(line,"\t")
  if len(parts)!=4{t.Fatalf("invalid matrix line: %q",line)}
  name,branch,yamlPath,expectedPath:=parts[0],parts[1],parts[2],parts[3]
  cases++
  t.Run(name,func(t *testing.T){
   yamlData,err:=os.ReadFile(filepath.Join(root,yamlPath))
   if err!=nil{t.Fatal(err)}
   specData,err:=os.ReadFile(filepath.Join(root,expectedPath))
   if err!=nil{t.Fatal(err)}
   var spec expectedCase
   if err=json.Unmarshal(specData,&spec);err!=nil{t.Fatal(err)}
   if err=validateSpec(spec);err!=nil{t.Fatal(err)}
   opts:=actions.ConvertOptions{Format:actions.FormatGitMasterV1,Branch:branch}
   graph,err:=actions.NewJobGraph(yamlData,opts)
   if err!=nil{t.Fatal(err)}
   nodes:=graph.Jobs()
   if len(nodes)!=len(spec.Jobs){
    t.Fatalf("graph nodes=%d expected=%d",len(nodes),len(spec.Jobs))
   }
   expectedCount:=0
   for _,node:=range nodes{
    job,ok:=spec.Jobs[node.Name]
    if !ok{t.Errorf("unexpected job %s",node.Name);continue}
    if !sameStrings(node.Needs,job.Needs){
     t.Errorf("job %s needs mismatch: %v vs %v",node.Name,node.Needs,job.Needs)
    }
    // Branch filtering is static; failure propagation is only known
    // at runtime, so dependent Commands still appear in ConvertYAML.
    staticallyFiltered:=strings.HasPrefix(job.SkipReason,"branch_filter")||
      strings.HasPrefix(job.SkipReason,"dependency_filtered:")
    if staticallyFiltered{
     if node.SkipReason!=job.SkipReason{
      t.Errorf("job %s filtered reason=%q expected=%q",node.Name,node.SkipReason,job.SkipReason)
     }
    }else{
     if node.SkipReason!=""{t.Errorf("eligible job %s statically skipped: %s",node.Name,node.SkipReason)}
     expectedCount+=len(job.Commands)
    }
   }
   pairs,err:=actions.ConvertYAML(yamlData,opts)
   if err!=nil{t.Fatal(err)}
   if len(pairs)!=expectedCount{t.Fatalf("planned %d eligible Commands expected %d",len(pairs),expectedCount)}
   used:=map[string]int{}
   for _,pair:=range pairs{
    j,ok:=spec.Jobs[pair.Job]
    if !ok{t.Fatalf("unexpected command Job %s",pair.Job)}
    if strings.HasPrefix(j.SkipReason,"branch_filter")||
       strings.HasPrefix(j.SkipReason,"dependency_filtered:"){
       t.Fatalf("statically skipped Job %s planned command %s",pair.Job,pair.Command)
    }
    position:=used[pair.Job]
    if position>=len(j.Commands){t.Fatalf("too many commands in %s",pair.Job)}
    marker:=j.Commands[position].OutputContains
    if marker!=""&&!strings.Contains(pair.Command,marker){
     t.Errorf("Job %s Command %d missing marker %q in script",pair.Job,position,marker)
    }
    used[pair.Job]++
   }
   // Ensure no dependent of a statically excluded Job is eligible.
   for _,node:=range nodes{
    if node.SkipReason!=""{continue}
    for _,dep:=range node.Needs{
     ancestor,_:=graph.Job(dep)
     if ancestor.SkipReason!=""{
      t.Errorf("Job %s eligible despite excluded dependency %s",node.Name,dep)
     }
    }
   }
  })
 }
 if cases!=7{t.Errorf("expected 7 test scenarios, found %d",cases)}
}

// The validator must reject missing result/SQLite evidence instead of
// accidentally passing an unexecuted VM scenario.
func TestVMExamplesVerifierRejectsMissingEvidence(t *testing.T) {
 dir:=t.TempDir()
 expected:=filepath.Join(dir,"expected.json")
 if err:=os.WriteFile(expected,[]byte(`{"action_status":"succeeded","jobs":{"one":{"status":"succeeded","needs":[],"commands":[{"status":"succeeded","exit_code":0,"output_contains":"OK"}]}}}`),0600);err!=nil{t.Fatal(err)}
 if err:=parseFiles(filepath.Join(dir,"missing-action.json"),filepath.Join(dir,"missing.db"),expected,0);err==nil{
  t.Fatal("accepted missing CLI/SQLite evidence")
 }
}
