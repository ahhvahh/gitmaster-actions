package gitmasteractions

import (
    "errors"
    "fmt"
)

// CommandPair is the intentionally flat, two-string representation of one
// command scheduled in a job. No runtime status or filesystem side effects.
type CommandPair struct {
    Job     string `json:"job"`
    Command string `json:"command"`
}

// ConvertOptions controls validation and branch/event selection. Format must
// be specified to prevent accidentally interpreting a different YAML dialect.
type ConvertOptions struct {
    Format    YAMLFormat
    Branch    string
    EventType string // "" or "manual" for preview, "commit"/"push" for trigger checks
}

// ConvertYAML is a deterministic, side-effect-free projection of the actual
// workflow parsers. Every returned row represents one potentially executable
// command after branch/needs filtering; skipped jobs have no rows.
//
// GitMaster workflow jobs follow the existing topological/lexical order, and
// steps keep their YAML sequence. Legacy action-v1 uses the YAML jobs order.
// Parallel jobs do not have a guaranteed wall-clock execution order; this is
// a deterministic plan, not a trace of actual concurrent execution.
// Runtime command failures cannot be predicted by the converter.
//
// No VM, SQLite, InsulaVM configuration, input archive, or network is needed.
// A valid workflow where all jobs are excluded returns an empty JSON array.
// SubmitYAML still rejects Actions with no executable jobs.
func ConvertYAML(yamlData []byte, opts ConvertOptions) ([]CommandPair, error) {
    plan,err:=parseConversionPlan(yamlData,opts)
    if err!=nil{return nil,err}
    commands:=make([]CommandPair,0)
    for _,job:=range plan.Jobs {
        if job.SkipReason!=""{continue}
        for _,cmd:=range job.Commands{
            commands=append(commands,CommandPair{Job:job.Name,Command:cmd.Run})
        }
    }
    return commands,nil
}

// parseConversionPlan centralizes the same YAML validation for the flat
// converter and the dependency graph; both reuse the executor's parsers.
func parseConversionPlan(yamlData []byte, opts ConvertOptions) (executionPlan, error) {
    if opts.Branch!="" && !validBranch(opts.Branch) {
        return executionPlan{},errors.New("invalid branch")
    }
    if opts.EventType!="" && opts.EventType!="manual" && opts.EventType!="commit" && opts.EventType!="push" {
        return executionPlan{},errors.New("event_type must be manual, commit or push")
    }
    var plan executionPlan
    var err error
    switch opts.Format {
    case FormatGitMasterV1:
        if (opts.EventType=="commit" || opts.EventType=="push") && opts.Branch=="" {
            return executionPlan{},errors.New("branch required for commit/push trigger")
        }
        plan,err=parseGitMasterWorkflow(yamlData,ActionOrigin{
            Branch:opts.Branch,EventType:opts.EventType,
        })
    case FormatActionV1:
        if opts.Branch!="" || opts.EventType=="commit" || opts.EventType=="push" {
            return executionPlan{},errors.New("action-v1 has no branch filters or event triggers")
        }
        plan,err=parseActionV1WithMode(yamlData,false)
    default:
        return executionPlan{},fmt.Errorf("unsupported YAML format %q",opts.Format)
    }
    if err!=nil{return executionPlan{},err}
    return plan,nil
}
