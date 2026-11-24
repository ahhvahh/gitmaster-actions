package main

import("encoding/json";"fmt";"os")
type item struct{ Job string `json:"job"`; Needs []string `json:"needs"`; Dependents []string `json:"dependents"`; State string `json:"state"`; CanRun bool `json:"can_run"`; Reason string `json:"reason"` }
func fatal(format string,v ...any){fmt.Fprintf(os.Stderr,format+"\n",v...);os.Exit(1)}
func main(){
 if len(os.Args)!=3{fatal("usage: verify <case> <file>")}
 bytes,err:=os.ReadFile(os.Args[2]);if err!=nil{fatal("%v",err)}
 var list []item
 if err:=json.Unmarshal(bytes,&list);err!=nil{fatal("JSON: %v",err)}
 nodes:=map[string]item{};ready:=0
 for _,n:=range list {if _,exists:=nodes[n.Job];exists{fatal("duplicate: %s",n.Job)};nodes[n.Job]=n;if n.CanRun{ready++}}
 check:=func(name,state,reason string,can bool){n,ok:=nodes[name];if !ok||n.State!=state||n.Reason!=reason||n.CanRun!=can{fatal("%s: incorrect state (%+v)",name,n)}}
 switch os.Args[1]{
 case "initial":if len(list)!=5||ready!=1{fatal("initial: %d nodes / %d ready",len(list),ready)};check("lint","ready","",true);check("build","waiting","",false);check("release","waiting","",false)
 case "success":if len(list)!=5||ready!=1{fatal("success: %d nodes / %d ready",len(list),ready)};check("release","ready","",true);check("lint","succeeded","",false)
 case "failure":if len(list)!=5||ready!=0{fatal("failure: %d nodes / %d ready",len(list),ready)};check("release","blocked","dependency_not_succeeded:package",false)
 case "develop","main":
  if len(list)!=2||ready!=1{fatal("branch: %d nodes / %d ready",len(list),ready)}
  active:="job_"+os.Args[1];inactive:="job_main";if os.Args[1]=="main"{inactive="job_develop"}
  check(active,"ready","",true);check(inactive,"skipped","branch_filter",false)
 default:fatal("unknown case")
 }
 fmt.Printf("[graph] %s: %d Jobs, %d ready\n",os.Args[1],len(list),ready)
}
