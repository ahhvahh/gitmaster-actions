package gitmasteractions

import (
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/ahhvahh/InsulaVM/insulavm"
)

type smokeRuntime struct {
    *testRuntime
    muSmoke sync.Mutex
    calls []string
    active int
    maxActive int
    holdA chan struct{}
    firstA chan struct{}
    aOnce sync.Once
    busyLeft int
}

func (r *smokeRuntime) Profile(name string) error {
    if name!="ci-smoke"&&name!="ci-go"{return fmt.Errorf("unknown profile: %s",name)}
    return nil
}
func (r *smokeRuntime) Command(ctx context.Context,_,_,script string,output io.Writer)(int,error){
    r.muSmoke.Lock()
    if r.busyLeft>0{
        r.busyLeft--
        r.muSmoke.Unlock()
        return -1,fmt.Errorf("wrapped: %w",insulavm.ErrVMExecBusy)
    }
    r.active++
    if r.active>r.maxActive{r.maxActive=r.active}
    r.calls=append(r.calls,script)
    r.muSmoke.Unlock()
    defer func(){r.muSmoke.Lock();r.active--;r.muSmoke.Unlock()}()
    if r.firstA!=nil&&strings.Contains(script,".smoke/a.txt"){
        r.aOnce.Do(func(){close(r.firstA)})
        select {
        case <-r.holdA:
        case <-ctx.Done():return -1,ctx.Err()
        }
    }
    if strings.Contains(script,"exit 17"){
        _,_=io.WriteString(output,"planned failure\n")
        return 17,errors.New("remote exit 17")
    }
    if _,err:=io.WriteString(output,"mock command completed\n");err!=nil{return -1,err}
    return 0,nil
}

func smokeYAML(t *testing.T) []byte{
    t.Helper()
    data,err:=os.ReadFile(filepath.Join("..","examples","workflow","dependency-parallel-smoke.yaml"))
    if err!=nil{t.Fatal(err)}
    return data
}
func smokeEngine(t *testing.T,r Runtime)*Engine{
    t.Helper()
    e,err:=New(Options{
        DatabasePath:filepath.Join(t.TempDir(),"actions.db"),
        RuntimeFactory:func()(Runtime,error){return r,nil},
        Workers:1,
    })
    if err!=nil{t.Fatal(err)}
    t.Cleanup(func(){
        for attempt:=0;attempt<100;attempt++ {
            if err:=e.Close();err==nil{return}
            time.Sleep(time.Millisecond)
        }
        t.Error("engine did not release running actions")
    })
    return e
}
func runSmokeBranch(t *testing.T,branch string,rt Runtime)(*Engine,ActionDetails){
    t.Helper()
    e:=smokeEngine(t,rt)
    a,err:=e.SubmitYAML(context.Background(),SubmitRequest{
        YAML:smokeYAML(t),Format:FormatGitMasterV1,
        Origin:ActionOrigin{Branch:branch,EventType:"manual"},
    })
    if err!=nil{t.Fatal(err)}
    done,err:=e.WaitAction(context.Background(),a.ID)
    if err!=nil{t.Fatal(err)}
    full,err:=e.GetActionDetails(context.Background(),a.ID)
    if err!=nil{t.Fatal(err)}
    if full.Action.Status!=done.Status{t.Fatal("inconsistent terminal state")}
    return e,full
}
func jobStates(t *testing.T,full ActionDetails) map[string]JobDetails{
    t.Helper()
    out:=map[string]JobDetails{}
    for _,entry:=range full.Jobs{out[entry.Job.Name]=entry}
    return out
}

func TestSmokeWorkflowBranchesAndDependencyBarriers(t *testing.T){
    for _,tc:=range []struct{
        branch string
        wantAction string
        want map[string]string
    }{
        {"develop","succeeded",map[string]string{
            "preparar":"succeeded","paralelo_a":"succeeded","paralelo_b":"succeeded",
            "unir_paralelos":"succeeded","finalizar":"succeeded",
            "somente_develop":"succeeded","somente_main":"skipped",
            "bloqueado_por_filtro":"skipped","falha_intencional":"skipped",
            "bloqueado_por_falha":"skipped",
        }},
        {"main","succeeded",map[string]string{
            "preparar":"succeeded","paralelo_a":"succeeded","paralelo_b":"succeeded",
            "unir_paralelos":"succeeded","finalizar":"succeeded",
            "somente_develop":"skipped","somente_main":"succeeded",
            "bloqueado_por_filtro":"succeeded","falha_intencional":"skipped",
            "bloqueado_por_falha":"skipped",
        }},
        {"teste-falha","failed",map[string]string{
            "preparar":"succeeded","paralelo_a":"succeeded","paralelo_b":"succeeded",
            "unir_paralelos":"succeeded","finalizar":"succeeded",
            "somente_develop":"skipped","somente_main":"skipped",
            "bloqueado_por_filtro":"skipped","falha_intencional":"failed",
            "bloqueado_por_falha":"skipped",
        }},
    }{
        t.Run(tc.branch,func(t *testing.T){
            r:=&smokeRuntime{testRuntime:&testRuntime{}}
            _,full:=runSmokeBranch(t,tc.branch,r)
            if full.Action.Status!=tc.wantAction{t.Fatalf("action=%s want=%s",full.Action.Status,tc.wantAction)}
            states:=jobStates(t,full)
            if len(states)!=len(tc.want){t.Fatalf("job count %d",len(states))}
            for name,want:=range tc.want{
                j,ok:=states[name]
                if !ok||j.Job.Status!=want{t.Errorf("job %s=%v want %s",name,j.Job,want)}
                for _,c:=range j.Commands{
                    if want=="skipped"&&c.Command.Status!="skipped"{
                        t.Errorf("skipped job %s executed command %s",name,c.Command.Status)
                    }
                    if want=="succeeded"&&c.Command.Status!="succeeded"{
                        t.Errorf("successful job %s command=%s",name,c.Command.Status)
                    }
                }
            }
            if tc.branch=="teste-falha"{
                if states["bloqueado_por_falha"].SkipReason!="dependency_not_succeeded:falha_intencional" {
                    t.Fatalf("dependency skip reason=%q",states["bloqueado_por_falha"].SkipReason)
                }
                if code:=states["falha_intencional"].Commands[0].Command.ExitCode;code==nil||*code!=17{
                    t.Fatalf("missing intentional exit code: %#v",code)
                }
            }
            r.muSmoke.Lock()
            activeMax:=r.maxActive
            calls:=append([]string(nil),r.calls...)
            r.muSmoke.Unlock()
            if activeMax>1{t.Errorf("overlapping ExecVM commands: %d",activeMax)}
            if len(calls)==0{t.Error("no commands called")}
            for _,command:=range calls{
                if strings.Contains(command,"exit 99"){t.Fatal("blocked command was executed")}
            }
        })
    }
}

func TestIndependentJobsRunningWhileVMCommandsAreSerialized(t *testing.T){
    hold:=make(chan struct{})
    first:=make(chan struct{})
    runtime:=&smokeRuntime{testRuntime:&testRuntime{},holdA:hold,firstA:first}
    e:=smokeEngine(t,runtime)
    a,err:=e.SubmitYAML(context.Background(),SubmitRequest{
        YAML:smokeYAML(t),Format:FormatGitMasterV1,
        Origin:ActionOrigin{Branch:"develop",EventType:"manual"},
    })
    if err!=nil{t.Fatal(err)}
    select{
    case <-first:
    case <-time.After(5*time.Second):t.Fatal("parallel A command did not start")
    }
    deadline:=time.Now().Add(5*time.Second)
    simultaneous:=false
    for time.Now().Before(deadline){
        jobs,err:=e.ListJobs(context.Background(),a.ID)
        if err!=nil{t.Fatal(err)}
        active:=0
        for _,j:=range jobs{
            if j.Status=="running"&&(j.Name=="paralelo_a"||j.Name=="paralelo_b"){active++}
        }
        if active==2{simultaneous=true;break}
        time.Sleep(10*time.Millisecond)
    }
    if !simultaneous{t.Fatal("independent jobs did not both reach running")}
    close(hold)
    // WaitAction returns only after all workers, command logs and cleanup.
    done,err:=e.WaitAction(context.Background(),a.ID)
    if err!=nil||done.Status!="succeeded"{t.Fatalf("state=%v err=%v",done,err)}
    runtime.muSmoke.Lock()
    max:=runtime.maxActive
    runtime.muSmoke.Unlock()
    if max!=1{t.Fatalf("expected serialized VM commands, max=%d",max)}
}

func TestBusyRetrySixAttemptsAndNonBusyNotRetried(t *testing.T){
    ctx:=context.Background()
    count:=0
    delays:=0
    exit,err:=runWithBusyRetry(ctx,func()(int,error){
        count++
        return -1,fmt.Errorf("host already executing: %w",insulavm.ErrVMExecBusy)
    },func(n int,d time.Duration){
        if n<1||n>maxVMBusyRetries||d!=10*time.Millisecond{t.Errorf("bad delay %v, attempt %d",d,n)}
        delays++
    },func()time.Duration{return 10*time.Millisecond})
    if exit!=-1||!errors.Is(err,insulavm.ErrVMExecBusy)||count!=6||delays!=5{
        t.Fatalf("retry exhaustion exit=%d err=%v attempts=%d delays=%d",exit,err,count,delays)
    }
    count=0
    exit,err=runWithBusyRetry(ctx,func()(int,error){
        count++
        if count<3{return -1,insulavm.ErrVMExecBusy}
        return 0,nil
    },nil,func()time.Duration{return 10*time.Millisecond})
    if err!=nil||exit!=0||count!=3{t.Fatalf("retry recovery: %d %v calls=%d",exit,err,count)}
    count=0
    _,err=runWithBusyRetry(ctx,func()(int,error){count++;return 17,errors.New("normal remote failure")},nil,func()time.Duration{return 10*time.Millisecond})
    if err==nil||count!=1{t.Fatalf("non-busy error was retried: %d %v",count,err)}
    for i:=0;i<500;i++{
        d:=randomBusyDelay()
        if d<10*time.Millisecond||d>time.Second{t.Fatalf("jitter out of range: %v",d)}
    }
}
func TestBusyRetryCancellation(t *testing.T){
    ctx,cancel:=context.WithCancel(context.Background())
    n:=0
    _,err:=runWithBusyRetry(ctx,func()(int,error){
        n++
        return -1,insulavm.ErrVMExecBusy
    },func(_ int,_ time.Duration){cancel()},func()time.Duration{return time.Second})
    if !errors.Is(err,context.Canceled)||n!=1{t.Fatalf("cancellation: %v calls=%d",err,n)}
}

func TestBusyRetryEventsVisibleInLibraryAndCLIContract(t *testing.T){
    r:=&smokeRuntime{testRuntime:&testRuntime{},busyLeft:2}
    _,full:=runSmokeBranch(t,"develop",r)
    if full.Action.Status!="succeeded"{t.Fatalf("status=%s",full.Action.Status)}
    retries:=0
    for _,ev:=range full.Events{if ev.Stage=="vm_busy_retry"{retries++}}
    if retries!=2{t.Errorf("busy retry events=%d want 2",retries)}
}
