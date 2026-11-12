package gitmasteractions

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// YAMLFormat requires an explicit adapter choice; no workflow is silently converted.
type YAMLFormat string

const (
	FormatActionV1    YAMLFormat = "action-v1"
	FormatGitMasterV1 YAMLFormat = "gitmaster-workflow-v1"
)

type ActionOrigin struct {
	CloneID      string `json:"clone_id"`
	CommitSHA    string `json:"commit_sha"`
	EventID      string `json:"event_id"`
	EventType    string `json:"event_type"`
	Branch       string `json:"branch"`
	WorkflowName string `json:"workflow_name"`
	WorkflowPath string `json:"workflow_path"`
}

type SubmitRequest struct {
	YAML   []byte
	Format YAMLFormat
	Origin ActionOrigin
	// ArchivePath is a trusted, already-authorized TAR.GZ snapshot of the commit.
	ArchivePath string
}

type plannedCommand struct{ Name, Run, Directory string }
type plannedJob struct {
	Name           string
	Needs          []string
	Commands       []plannedCommand
	AllowFailure   bool
	TimeoutSeconds int
	SkipReason     string
}
type executionPlan struct {
	Name, Profile string
	Jobs          []plannedJob
	ArchivePath   string
}

var safeWorkflowName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)
var safeBranch = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,127}$`)
var safeSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func validateOrigin(o ActionOrigin) error {
	if o.EventType != "" && o.EventType != "commit" && o.EventType != "push" && o.EventType != "manual" {
		return errors.New("event_type must be commit, push or manual")
	}
	if o.CloneID == "" && o.CommitSHA == "" && o.WorkflowPath == "" && o.EventID == "" && o.Branch == "" {
		return nil
	}
	if o.CloneID == "" || !safeSHA.MatchString(o.CommitSHA) || !validBranch(o.Branch) || o.WorkflowPath == "" || o.EventType == "" {
		return errors.New("complete clone_id, 40-character commit_sha, branch, workflow_path and event_type are required")
	}
	if strings.HasPrefix(o.WorkflowPath, "/") || strings.Contains(o.WorkflowPath, "\\") || strings.ContainsRune(o.WorkflowPath, 0) || path.Clean(o.WorkflowPath) != o.WorkflowPath || strings.HasPrefix(o.WorkflowPath, "../") || o.WorkflowPath == ".." {
		return errors.New("unsafe workflow_path")
	}
	return nil
}
func validBranch(v string) bool {
	return safeBranch.MatchString(v) && !strings.Contains(v, "..") && !strings.Contains(v, "//") && !strings.Contains(v, "/.") && !strings.HasSuffix(v, "/") && !strings.HasSuffix(v, ".lock")
}
func validateDirectory(v string) error {
	if v == "" || v == "." {
		return nil
	}
	if path.IsAbs(v) || strings.ContainsAny(v, "\\\x00\r\n") {
		return errors.New("unsafe working_directory")
	}
	for _, piece := range strings.Split(v, "/") {
		if piece == "" || piece == "." || piece == ".." {
			return errors.New("unsafe working_directory")
		}
	}
	return nil
}
func parseStrictYAML(data []byte, out any) error {
	if len(data) == 0 || len(data) > 1<<20 {
		return errors.New("YAML must be 1..1048576 bytes")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	var second any
	if err := d.Decode(&second); err == nil {
		return errors.New("YAML must contain one document")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

type workflowV1 struct {
	Version   int    `yaml:"version"`
	Name      string `yaml:"name"`
	VMProfile string `yaml:"vm_profile"`
	Triggers  []struct {
		Event    string   `yaml:"event"`
		Branches []string `yaml:"branches"`
	} `yaml:"triggers"`
	Jobs map[string]struct {
		Branches struct {
			Include []string `yaml:"include"`
			Exclude []string `yaml:"exclude"`
		} `yaml:"branches"`
		Needs          []string `yaml:"needs"`
		TimeoutSeconds int      `yaml:"timeout_seconds"`
		Steps          []struct {
			Name             string `yaml:"name"`
			WorkingDirectory string `yaml:"working_directory"`
			Run              string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func branchAllowed(branch string, include, exclude []string) bool {
	for _, v := range exclude {
		if v == branch {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, v := range include {
		if v == branch {
			return true
		}
	}
	return false
}
func validateBranchList(values []string) error {
	if len(values) > 100 {
		return errors.New("too many branch filters")
	}
	seen := map[string]bool{}
	for _, v := range values {
		if !validBranch(v) || seen[v] {
			return fmt.Errorf("invalid/duplicate branch %q", v)
		}
		seen[v] = true
	}
	return nil
}
func parseGitMasterWorkflow(data []byte, o ActionOrigin) (executionPlan, error) {
	var w workflowV1
	if err := parseStrictYAML(data, &w); err != nil {
		return executionPlan{}, err
	}
	if w.Version != 1 || strings.TrimSpace(w.Name) == "" || !safeWorkflowName.MatchString(w.VMProfile) || len(w.Jobs) == 0 || len(w.Jobs) > 100 {
		return executionPlan{}, errors.New("workflow requires version: 1, name, vm_profile and 1..100 jobs")
	}
	if o.WorkflowName != "" && o.WorkflowName != w.Name {
		return executionPlan{}, errors.New("origin workflow name does not match YAML")
	}
	for _, t := range w.Triggers {
		if t.Event != "commit" && t.Event != "push" {
			return executionPlan{}, errors.New("unknown workflow trigger")
		}
		if err := validateBranchList(t.Branches); err != nil {
			return executionPlan{}, err
		}
	}
	if o.EventType == "commit" || o.EventType == "push" {
		matched := false
		for _, t := range w.Triggers {
			if t.Event == o.EventType && branchAllowed(o.Branch, t.Branches, nil) {
				matched = true
			}
		}
		if !matched {
			return executionPlan{}, errors.New("workflow trigger does not match event and branch")
		}
	}
	names := make([]string, 0, len(w.Jobs))
	for name, j := range w.Jobs {
		if !safeWorkflowName.MatchString(name) || len(j.Steps) == 0 || len(j.Steps) > 100 || j.TimeoutSeconds < 0 || j.TimeoutSeconds > 86400 {
			return executionPlan{}, fmt.Errorf("invalid job %q", name)
		}
		if o.Branch == "" && (len(j.Branches.Include) > 0 || len(j.Branches.Exclude) > 0) {
			return executionPlan{}, errors.New("branch required when jobs use branch filters")
		}
		if err := validateBranchList(j.Branches.Include); err != nil {
			return executionPlan{}, err
		}
		if err := validateBranchList(j.Branches.Exclude); err != nil {
			return executionPlan{}, err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	states := map[string]int{}
	order := []string{}
	var visit func(string) error
	visit = func(name string) error {
		if states[name] == 1 {
			return fmt.Errorf("cyclic needs at %s", name)
		}
		if states[name] == 2 {
			return nil
		}
		states[name] = 1
		j, ok := w.Jobs[name]
		if !ok {
			return fmt.Errorf("unknown need %q", name)
		}
		seen := map[string]bool{}
		for _, dep := range j.Needs {
			if seen[dep] || dep == name {
				return fmt.Errorf("duplicate/self need %q", dep)
			}
			seen[dep] = true
			if err := visit(dep); err != nil {
				return err
			}
		}
		states[name] = 2
		order = append(order, name)
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return executionPlan{}, err
		}
	}
	p := executionPlan{Name: w.Name, Profile: w.VMProfile}
	skipped := map[string]bool{}
	for _, name := range order {
		j := w.Jobs[name]
		item := plannedJob{Name: name, Needs: append([]string(nil), j.Needs...), TimeoutSeconds: j.TimeoutSeconds}
		if o.Branch != "" && !branchAllowed(o.Branch, j.Branches.Include, j.Branches.Exclude) {
			item.SkipReason = "branch_filter"
		}
		for _, dep := range item.Needs {
			if skipped[dep] && item.SkipReason == "" {
				item.SkipReason = "dependency_filtered:" + dep
			}
		}
		skipped[name] = item.SkipReason != ""
		for i, s := range j.Steps {
			if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Run) == "" || strings.ContainsRune(s.Run, 0) || len(s.Run) > 1<<20 {
				return executionPlan{}, fmt.Errorf("invalid step %d in job %q", i, name)
			}
			if err := validateDirectory(s.WorkingDirectory); err != nil {
				return executionPlan{}, fmt.Errorf("job %q: %w", name, err)
			}
			item.Commands = append(item.Commands, plannedCommand{Name: s.Name, Run: s.Run, Directory: s.WorkingDirectory})
		}
		p.Jobs = append(p.Jobs, item)
	}
	return p, nil
}
func parseActionV1(data []byte) (executionPlan, error) {
	m, err := parseManifest(data, "")
	if err != nil {
		return executionPlan{}, err
	}
	p := executionPlan{Name: m.Action.Name, Profile: m.VM.Profile}
	if len(m.Action.Imports) > 1 {
		return executionPlan{}, errors.New("action-v1 supports only one authorized archive per run")
	}
	if len(m.Action.Imports) == 1 {
		p.ArchivePath = m.Action.Imports[0].Source
	}
	for _, j := range m.Action.Jobs {
		item := plannedJob{Name: j.Name, AllowFailure: j.AllowFailure}
		for _, c := range j.Commands {
			item.Commands = append(item.Commands, plannedCommand{Name: c, Run: c})
		}
		p.Jobs = append(p.Jobs, item)
	}
	return p, nil
}
func parseSubmission(r SubmitRequest) (executionPlan, error) {
	if err := validateOrigin(r.Origin); err != nil {
		return executionPlan{}, err
	}
	var p executionPlan
	var err error
	switch r.Format {
	case FormatActionV1:
		p, err = parseActionV1(r.YAML)
	case FormatGitMasterV1:
		p, err = parseGitMasterWorkflow(r.YAML, r.Origin)
	default:
		return p, errors.New("YAML Format must explicitly be action-v1 or gitmaster-workflow-v1")
	}
	if err != nil {
		return p, err
	}
	if r.ArchivePath != "" && p.ArchivePath != "" {
		return p, errors.New("archive specified in both request and YAML")
	}
	if r.ArchivePath != "" {
		p.ArchivePath = r.ArchivePath
	}
	active := 0
	for _, job := range p.Jobs {
		if job.SkipReason == "" {
			active++
		}
	}
	if active == 0 {
		return p, errors.New("workflow has no executable jobs on this branch")
	}
	return p, nil
}