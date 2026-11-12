package main

import (
    "context"
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "io"
    "os"
    "os/signal"
    "syscall"

    actions "github.com/ahhvahh/gitmaster-actions"
)

func output(v any)error {
    enc:=json.NewEncoder(os.Stdout)
    enc.SetIndent("","  ")
    return enc.Encode(v)
}
func run(args []string)error{
    if len(args)==0||args[0]=="--help"||args[0]=="help"{
        fmt.Println("usage: gitmaster-actions run|actions|jobs|commands|logs|outputs|profiles [options]")
        return nil
    }
    cmd:=args[0]
    sub:=""
    rest:=args[1:]
    if cmd!="run" {
        if len(rest)==0{return errors.New("subcommand required")}
        sub=rest[0]
        rest=rest[1:]
    }
    f:=flag.NewFlagSet(cmd,flag.ContinueOnError)
    db:=f.String("db","actions.db","GitMaster Actions private SQLite")
    config:=f.String("insula-config","","InsulaVM administrator config")
    file:=f.String("file","","YAML manifest path")
    stdin:=f.Bool("stdin",false,"read YAML from standard input")
    id:=f.String("id","","Action, Job or Command UUID")
    actionID:=f.String("action-id","","Action UUID")
    jobID:=f.String("job-id","","Job UUID")
    commandID:=f.String("command-id","","Command UUID")
    skip:=f.Uint64("skip-lines",0,"number of already read lines")
    stream:=f.String("stream","","stdin, stdout or stderr")
    limit:=f.Int("limit",100,"maximum results")
    imageDir:=f.String("images-dir","","administrator-controlled image directory")
    name:=f.String("name","","new VM profile name")
    image:=f.String("image","","QCOW2 filename in --images-dir")
    osName:=f.String("os","","operating system label")
    vcpus:=f.Int("vcpus",0,"VM CPU count")
    memory:=f.Int("memory-mib",0,"VM memory")
    disk:=f.Int("disk-gib",0,"VM disk size in GiB")
    if err:=f.Parse(rest);err!=nil{return err}
    if len(f.Args())!=0{return errors.New("unexpected positional arguments")}
    ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
    defer stop()
    if cmd=="profiles"{
        if *config==""{return errors.New("--insula-config is required")}
        switch sub{
        case "list":
            profiles,err:=actions.ListVMProfiles(*config)
            if err!=nil{return err}
            return output(profiles)
        case "images":
            if *imageDir==""{return errors.New("--images-dir required")}
            list,err:=actions.ListVMImages(*imageDir)
            if err!=nil{return err}
            return output(list)
        case "register":
            if *imageDir==""{return errors.New("--images-dir required")}
            p,err:=actions.RegisterVMProfile(*config,*imageDir,actions.ProfileRequest{
                Name:*name,OS:*osName,Image:*image,VCPUs:*vcpus,MemoryMiB:*memory,DiskGiB:*disk,
            })
            if err!=nil{return err}
            return output(p)
        default:return errors.New("profiles: expected list, images or register")
        }
    }
    engine,err:=actions.New(actions.Options{DatabasePath:*db,InsulaConfigPath:*config})
    if err!=nil{return err}
    defer engine.Close()
    switch cmd{
    case "run":
        if (*file=="")==!*stdin{return errors.New("run requires exactly one --file or --stdin")}
        var a *actions.Action
        if *stdin{
            data,err:=io.ReadAll(io.LimitReader(os.Stdin,2<<20))
            if err!=nil{return err}
            a,err=engine.RunYAML(ctx,data)
            if a!=nil{_ = output(a)}
            return err
        }
        a,err=engine.RunFile(ctx,*file)
        if a!=nil{_ = output(a)}
        return err
    case "actions":
        if sub=="list"{
            a,err:=engine.ListActions(ctx,actions.ActionFilter{},actions.Page{Limit:*limit})
            if err!=nil{return err}
            return output(a)
        }
        if sub=="get"{
            a,err:=engine.GetAction(ctx,*id)
            if err!=nil{return err}
            return output(a)
        }
    case "jobs":
        if sub=="list"{
            a,err:=engine.GetAction(ctx,*actionID)
            if err!=nil{return err}
            jobs,err:=a.Jobs(ctx)
            if err!=nil{return err}
            return output(jobs)
        }
        if sub=="get"{
            j,err:=engine.GetJob(ctx,*id)
            if err!=nil{return err}
            return output(j)
        }
    case "commands":
        if sub=="list"{
            job,err:=engine.GetJob(ctx,*jobID)
            if err!=nil{return err}
            commands,err:=job.Commands(ctx)
            if err!=nil{return err}
            return output(commands)
        }
        if sub=="get"{
            command,err:=engine.GetCommand(ctx,*id)
            if err!=nil{return err}
            return output(command)
        }
    case "logs":
        if sub=="list"{
            command,err:=engine.GetCommand(ctx,*commandID)
            if err!=nil{return err}
            logs,err:=command.Logs(ctx,*skip)
            if err!=nil{return err}
            return output(logs)
        }
    case "outputs":
        if sub=="list"{
            command,err:=engine.GetCommand(ctx,*commandID)
            if err!=nil{return err}
            chunks,err:=command.Outputs(ctx,actions.OutputFilter{StreamType:*stream},
                actions.Page{Limit:*limit})
            if err!=nil{return err}
            return output(chunks)
        }
    }
    return fmt.Errorf("unknown command %q subcommand %q",cmd,sub)
}
func main(){
    if err:=run(os.Args[1:]);err!=nil{
        fmt.Fprintln(os.Stderr,"gitmaster-actions:",err)
        os.Exit(1)
    }
}
