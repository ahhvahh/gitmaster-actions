package gitmasteractions

import (
 "os"
 "path/filepath"
 "reflect"
 "testing"
)

func loadJobGraphFixture(t *testing.T, file, branch string) *JobGraph {
 t.Helper()
 data,err:=os.ReadFile(filepath.Join("..","tests","plan-cases",file))
 if err!=nil { t.Fatal(err) }
 graph,err:=NewJobGraph(data,ConvertOptions{Format:FormatGitMasterV1,Branch:branch})
 if err!=nil { t.Fatal(err) }
 return graph
}

func TestJobGraphDependencyLinks(t *testing.T){
 graph:=loadJobGraphFixture(t,"needs-dag.yaml","")
 nodes:=graph.Jobs()
 names:=make([]string,0,len(nodes))
 for _,n:=range nodes{ names=append(names,n.Name) }
 if want:=[]string{"lint","build","package","verify","release"}; !reflect.DeepEqual(names,want){t.Fatalf("order=%v want=%v",names,want)}
 for _,tc:=range []struct{job string; needs,dependents []string}{
   {"lint",[]string{},[]string{"build"}},
   {"build",[]string{"lint"},[]string{"package","verify"}},
   {"package",[]string{"build"},[]string{"release"}},
   {"verify",[]string{"build"},[]string{"release"}},
   {"release",[]string{"package","verify"},[]string{}},
 } {
   got,err:=graph.Dependencies(tc.job)
   if err!=nil||!reflect.DeepEqual(got,tc.needs){t.Errorf("%s dependencies=%v err=%v",tc.job,got,err)}
   got,err=graph.Dependents(tc.job)
   if err!=nil||!reflect.DeepEqual(got,tc.dependents){t.Errorf("%s dependents=%v err=%v",tc.job,got,err)}
   has,err:=graph.HasDependencies(tc.job)
   if err!=nil||has!=(len(tc.needs)>0){t.Errorf("%s has_dependencies=%v err=%v",tc.job,has,err)}
 }
 for _,tc:=range []struct{job,dependency string; want bool}{
  {"release","lint",true},{"release","build",true},{"release","package",true},
  {"release","verify",true},{"build","lint",true},{"lint","release",false},
  {"package","verify",false},
 }{
  has,err:=graph.DependsOn(tc.job,tc.dependency)
  if err!=nil||has!=tc.want{t.Errorf("%s dependsOn %s = %v / %v",tc.job,tc.dependency,has,err)}
 }
 // Returned slices must not be aliases into the immutable dependency graph.
 nodes[1].Needs[0]="mutated"
 nodes[0].Dependents[0]="mutated"
 node,ok:=graph.Job("build")
 if !ok{t.Fatal("build not found")}
 node.Needs[0]="mutated"
 needs,_:=graph.Dependencies("build")
 if !reflect.DeepEqual(needs,[]string{"lint"}){t.Errorf("graph mutated: %v",needs)}
 dependents,_:=graph.Dependents("lint")
 if !reflect.DeepEqual(dependents,[]string{"build"}){t.Errorf("graph mutated: %v",dependents)}
}

func TestJobGraphReadinessTransitions(t *testing.T){
 graph:=loadJobGraphFixture(t,"needs-dag.yaml","")
 for _,tc:=range []struct{statuses map[string]string; ready []string}{
  {nil,[]string{"lint"}},
  {map[string]string{"lint":"running"},[]string{}},
  {map[string]string{"lint":"succeeded"},[]string{"build"}},
  {map[string]string{"lint":"succeeded","build":"succeeded"},[]string{"package","verify"}},
  {map[string]string{"lint":"succeeded","build":"succeeded","package":"succeeded","verify":"succeeded"},[]string{"release"}},
  {map[string]string{"lint":"succeeded","build":"succeeded","package":"succeeded","verify":"succeeded","release":"running"},[]string{}},
 }{
  got,err:=graph.ReadyJobs(tc.statuses)
  if err!=nil||!reflect.DeepEqual(got,tc.ready){t.Errorf("statuses=%v ready=%v want=%v err=%v",tc.statuses,got,tc.ready,err)}
 }
 waiting,err:=graph.Decision("release",map[string]string{"package":"succeeded"})
 if err!=nil||waiting.State!="waiting"||waiting.CanRun||!reflect.DeepEqual(waiting.WaitingFor,[]string{"verify"}){
  t.Errorf("waiting decision %+v %v",waiting,err)
 }
 for _,status:=range []string{"failed","skipped","cancelled"}{
  blocked,err:=graph.Decision("release",map[string]string{"package":status,"verify":"succeeded"})
  if err!=nil||blocked.State!="blocked"||blocked.CanRun||
     blocked.Reason!="dependency_not_succeeded:package"||!reflect.DeepEqual(blocked.BlockedBy,[]string{"package"}){
   t.Errorf("status=%s blocked=%+v err=%v",status,blocked,err)
  }
 }
 for _,status:=range []string{"running","succeeded","failed","skipped","cancelled"}{
  result,err:=graph.Decision("lint",map[string]string{"lint":status})
  if err!=nil||result.CanRun||result.State!=status{t.Errorf("terminal/active state=%s decision=%+v err=%v",status,result,err)}
 }
}

func TestJobGraphBranchFiltering(t *testing.T){
 bytes,err:=os.ReadFile(filepath.Join("..","examples","workflow","branch-filter-smoke.yaml"))
 if err!=nil{t.Fatal(err)}
 for _,branch:=range []string{"develop","main"}{
  t.Run(branch,func(t *testing.T){
   graph,err:=NewJobGraph(bytes,ConvertOptions{Format:FormatGitMasterV1,Branch:branch})
   if err!=nil{t.Fatal(err)}
   allowed:="job_"+branch
   filtered:="job_main"
   if branch=="main"{filtered="job_develop"}
   ready,err:=graph.ReadyJobs(nil)
   if err!=nil||!reflect.DeepEqual(ready,[]string{allowed}){t.Errorf("ready=%v err=%v",ready,err)}
   yes,_:=graph.Decision(allowed,nil)
   no,_:=graph.Decision(filtered,nil)
   if !yes.CanRun||yes.State!="ready"||yes.HasDependencies{t.Errorf("unexpected allowed: %+v",yes)}
   if no.CanRun||no.State!="skipped"||no.Reason!="branch_filter"{t.Errorf("unexpected filtered: %+v",no)}
  })
 }
 graph:=loadJobGraphFixture(t,"branches.yaml","develop")
 skip,err:=graph.Decision("after_main",nil)
 if err!=nil||skip.State!="skipped"||skip.Reason!="dependency_filtered:main_job"{
  t.Errorf("filtered prerequisite: %+v %v",skip,err)
 }
 ready,err:=graph.ReadyJobs(nil)
 if err!=nil||!reflect.DeepEqual(ready,[]string{"restricted","shared"}){
  t.Errorf("expected independent active jobs, got %v (%v)",ready,err)
 }
}

func TestJobGraphInvalidStatusesAndInputs(t *testing.T){
 g:=loadJobGraphFixture(t,"needs-dag.yaml","")
 for _,statuses:=range []map[string]string{{"not_a_job":"running"},{"lint":"ready"},{"build":"invalid"}}{
  if _,err:=g.Evaluate(statuses);err==nil{t.Errorf("accepted statuses %v",statuses)}
  if _,err:=g.ReadyJobs(statuses);err==nil{t.Errorf("accepted statuses %v",statuses)}
  if _,err:=g.Decision("lint",statuses);err==nil{t.Errorf("accepted statuses %v",statuses)}
 }
 if _,err:=g.Decision("missing",nil);err==nil{t.Error("unknown job accepted")}
 if _,err:=g.Dependencies("missing");err==nil{t.Error("unknown job accepted")}
 if _,err:=g.Dependents("missing");err==nil{t.Error("unknown job accepted")}
 if _,err:=g.HasDependencies("missing");err==nil{t.Error("unknown job accepted")}
 if _,err:=g.DependsOn("build","missing");err==nil{t.Error("unknown job accepted")}
 for _,invalid:=range []string{"cycle","unknown-need","duplicate-need","self-need"}{
  bytes,err:=os.ReadFile(filepath.Join("..","tests","plan-cases","invalid",invalid+".yaml"))
  if err!=nil{t.Fatal(err)}
  if _,err:=NewJobGraph(bytes,ConvertOptions{Format:FormatGitMasterV1,Branch:"develop"});err==nil{
   t.Errorf("invalid dependency structure accepted: %s",invalid)
  }
 }
}

func TestJobGraphActionV1IndependentJobs(t *testing.T){
 bytes,err:=os.ReadFile(filepath.Join("..","tests","plan-cases","legacy.yaml"))
 if err!=nil{t.Fatal(err)}
 graph,err:=NewJobGraph(bytes,ConvertOptions{Format:FormatActionV1})
 if err!=nil{t.Fatal(err)}
 ready,err:=graph.ReadyJobs(nil)
 if err!=nil||!reflect.DeepEqual(ready,[]string{"zeta","alpha"}){t.Fatalf("legacy jobs=%v err=%v",ready,err)}
}
