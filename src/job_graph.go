package gitmasteractions

import "fmt"

// GraphJob describes the static dependency links of a workflow Job.
// Needs are prerequisites; Dependents are the Jobs that directly need this Job.
// SkipReason is assigned by the workflow parser for branch filtering.
type GraphJob struct {
    Name            string   `json:"job"`
    Needs           []string `json:"needs"`
    Dependents      []string `json:"dependents"`
    HasDependencies bool     `json:"has_dependencies"`
    SkipReason      string   `json:"skip_reason,omitempty"`
}

// JobDecision describes whether a Job may start with the supplied execution
// statuses. State is ready, waiting, blocked, pending (never returned),
// running, succeeded, failed, skipped or cancelled. Missing statuses are pending.
type JobDecision struct {
    GraphJob
    State      string   `json:"state"`
    CanRun     bool     `json:"can_run"`
    WaitingFor []string `json:"waiting_for,omitempty"`
    BlockedBy  []string `json:"blocked_by,omitempty"`
    Reason     string   `json:"reason,omitempty"`
}

// JobGraph is an immutable, ordered DAG with direct dependency and dependent
// links. It is safe for concurrent read-only queries; the current Job statuses
// are provided by the caller as a snapshot and never stored in the graph.
type JobGraph struct {
    jobs  map[string]GraphJob
    order []string
}

// NewJobGraph parses a supported workflow without initializing Engine,
// SQLite, InsulaVM, or a VM. The same YAML parser used for real Actions
// rejects unknown dependencies, duplicate needs, cycles and invalid filters.
func NewJobGraph(yamlData []byte, opts ConvertOptions) (*JobGraph, error) {
    plan, err := parseConversionPlan(yamlData, opts)
    if err != nil {
        return nil, err
    }
    return newJobGraphFromPlan(plan)
}

func newJobGraphFromPlan(plan executionPlan) (*JobGraph, error) {
    g := &JobGraph{jobs: make(map[string]GraphJob, len(plan.Jobs))}
    for _, j := range plan.Jobs {
        if j.Name == "" {
            return nil, fmt.Errorf("empty job name in execution plan")
        }
        if _, exists := g.jobs[j.Name]; exists {
            return nil, fmt.Errorf("duplicate job %q in execution plan", j.Name)
        }
        g.order = append(g.order, j.Name)
        needs := append([]string{}, j.Needs...)
        g.jobs[j.Name] = GraphJob{
            Name: j.Name, Needs: needs, Dependents: []string{},
            HasDependencies: len(needs) > 0, SkipReason: j.SkipReason,
        }
    }
    for _, name := range g.order {
        node := g.jobs[name]
        for _, dependency := range node.Needs {
            dep, ok := g.jobs[dependency]
            if !ok {
                return nil, fmt.Errorf("job %q references missing dependency %q", name, dependency)
            }
            dep.Dependents = append(dep.Dependents, name)
            g.jobs[dependency] = dep
        }
    }
    return g, nil
}

func copyGraphJob(j GraphJob) GraphJob {
    j.Needs = append([]string{}, j.Needs...)
    j.Dependents = append([]string{}, j.Dependents...)
    return j
}

// Jobs returns the Jobs in the deterministic, topological order of the parser.
// Each result owns independent copies of its slices, so callers cannot alter
// the graph accidentally.
func (g *JobGraph) Jobs() []GraphJob {
    out := make([]GraphJob, 0, len(g.order))
    for _, name := range g.order {
        out = append(out, copyGraphJob(g.jobs[name]))
    }
    return out
}

func (g *JobGraph) Job(name string) (GraphJob, bool) {
    node, ok := g.jobs[name]
    if !ok { return GraphJob{}, false }
    return copyGraphJob(node), true
}

func (g *JobGraph) Dependencies(name string) ([]string, error) {
    node, ok := g.Job(name)
    if !ok { return nil, fmt.Errorf("unknown job %q", name) }
    return node.Needs, nil
}

func (g *JobGraph) Dependents(name string) ([]string, error) {
    node, ok := g.Job(name)
    if !ok { return nil, fmt.Errorf("unknown job %q", name) }
    return node.Dependents, nil
}

func (g *JobGraph) HasDependencies(name string) (bool, error) {
    node, ok := g.jobs[name]
    if !ok { return false, fmt.Errorf("unknown job %q", name) }
    return node.HasDependencies, nil
}

// DependsOn checks for direct OR transitive dependencies without confusing
// structural reachability with execution status or branch eligibility.
func (g *JobGraph) DependsOn(job, ancestor string) (bool, error) {
    if _, ok := g.jobs[job]; !ok { return false, fmt.Errorf("unknown job %q", job) }
    if _, ok := g.jobs[ancestor]; !ok { return false, fmt.Errorf("unknown job %q", ancestor) }
    seen := make(map[string]bool)
    var visit func(string) bool
    visit = func(name string) bool {
        for _, need := range g.jobs[name].Needs {
            if need == ancestor { return true }
            if !seen[need] {
                seen[need] = true
                if visit(need) { return true }
            }
        }
        return false
    }
    return visit(job), nil
}

func (g *JobGraph) validateStatuses(statuses map[string]string) error {
    for name, status := range statuses {
        if _, ok := g.jobs[name]; !ok { return fmt.Errorf("unknown job status %q", name) }
        switch status {
        case "", "pending", "running", "succeeded", "failed", "skipped", "cancelled":
        default: return fmt.Errorf("unknown job status %q for %s", status, name)
        }
    }
    return nil
}

func (g *JobGraph) decide(name string, statuses map[string]string) JobDecision {
    node := g.jobs[name]
    result := JobDecision{GraphJob: copyGraphJob(node)}
    // Branch filters (including statically filtered prerequisites) are
    // evaluated before runtime status. Such Jobs can never be scheduled.
    if node.SkipReason != "" {
        result.State = "skipped"
        result.Reason = node.SkipReason
        return result
    }
    current := statuses[name]
    switch current {
    case "running", "succeeded", "failed", "skipped", "cancelled":
        result.State = current
        return result
    }
    for _, need := range node.Needs {
        switch statuses[need] {
        case "succeeded":
        case "failed", "skipped", "cancelled":
            result.BlockedBy = append(result.BlockedBy, need)
        default:
            result.WaitingFor = append(result.WaitingFor, need)
        }
    }
    // A terminal failure is irrecoverable, but wait for other active needs
    // before declaring this Job blocked, like the existing scheduler.
    if len(result.WaitingFor) > 0 {
        result.State = "waiting"
    } else if len(result.BlockedBy) > 0 {
        result.State = "blocked"
        result.Reason = "dependency_not_succeeded:" + result.BlockedBy[0]
    } else {
        result.State = "ready"
        result.CanRun = true
    }
    return result
}

// Decision answers "can this Job execute now?". A Job without dependencies
// is ready if it is pending and allowed by its branch filter. Jobs with
// dependencies become ready only when ALL their needs have succeeded.
func (g *JobGraph) Decision(name string, statuses map[string]string) (JobDecision, error) {
    if _, ok := g.jobs[name]; !ok { return JobDecision{}, fmt.Errorf("unknown job %q", name) }
    if err := g.validateStatuses(statuses); err != nil { return JobDecision{}, err }
    return g.decide(name, statuses), nil
}

// Evaluate lists every Job with its dependency links and readiness decision.
func (g *JobGraph) Evaluate(statuses map[string]string) ([]JobDecision, error) {
    if err := g.validateStatuses(statuses); err != nil { return nil, err }
    out := make([]JobDecision, 0, len(g.order))
    for _, name := range g.order {
        out = append(out, g.decide(name, statuses))
    }
    return out, nil
}

// ReadyJobs returns only the Jobs that could start immediately, ordered
// deterministically. The scheduler separately enforces MaxParallelJobs.
func (g *JobGraph) ReadyJobs(statuses map[string]string) ([]string, error) {
    if err := g.validateStatuses(statuses); err != nil { return nil, err }
    out := make([]string, 0)
    for _, name := range g.order {
        if g.decide(name, statuses).CanRun { out = append(out, name) }
    }
    return out, nil
}
