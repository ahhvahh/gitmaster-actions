package gitmasteractions_test

import (
 "testing"
 actions "github.com/ahhvahh/gitmaster-actions"
)

func TestRootPublicJobGraph(t *testing.T){
 source:=[]byte("version: 1\nname: public-graph\nvm_profile: ci-smoke\njobs:\n  base:\n    steps: [{name: run, run: echo base}]\n  dependent:\n    needs: [base]\n    steps: [{name: run, run: echo dependent}]\n")
 g,err:=actions.NewJobGraph(source,actions.ConvertOptions{Format:actions.FormatGitMasterV1})
 if err!=nil{t.Fatal(err)}
 if len(g.Jobs())!=2{t.Fatal("public graph missing nodes")}
 decision,err:=g.Decision("dependent",map[string]string{"base":"succeeded"})
 if err!=nil||!decision.CanRun{t.Fatalf("decision %+v err=%v",decision,err)}
}
