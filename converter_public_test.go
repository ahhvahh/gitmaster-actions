package gitmasteractions_test

import (
    "testing"

    actions "github.com/ahhvahh/gitmaster-actions"
)

func TestPublicFacadeConvertsYAMLWithoutEngine(t *testing.T) {
    source:=[]byte("version: 1\nname: public-api\nvm_profile: ci-smoke\njobs:\n  first:\n    steps:\n      - name: execute\n        run: echo ok\n")
    entries,err:=actions.ConvertYAML(source,actions.ConvertOptions{
        Format:actions.FormatGitMasterV1,
    })
    if err!=nil{t.Fatal(err)}
    if len(entries)!=1{t.Fatalf("one command expected, got %#v",entries)}
    if entries[0].Job!="first"||entries[0].Command!="echo ok"{
        t.Errorf("incorrect public API output: %#v",entries[0])
    }
}
