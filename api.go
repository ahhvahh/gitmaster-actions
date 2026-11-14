// Package gitmasteractions exposes the stable root import path for GitMaster.
// The implementation is organized under /src; these aliases preserve the
// published Go API without requiring changes in consumers.
package gitmasteractions

import core "github.com/ahhvahh/gitmaster-actions/src"

type (
	Engine         = core.Engine
	Options        = core.Options
	Action         = core.Action
	Job            = core.Job
	Command        = core.Command
	CommandLog     = core.CommandLog
	CommandOutput  = core.CommandOutput
	ActionFilter   = core.ActionFilter
	Page           = core.Page
	OutputFilter   = core.OutputFilter
	Runtime        = core.Runtime
	OutputWriter   = core.OutputWriter
	GuestCommand   = core.GuestCommand
	YAMLFormat     = core.YAMLFormat
	CommandPair    = core.CommandPair
	JobGraph       = core.JobGraph
	GraphJob       = core.GraphJob
	JobDecision    = core.JobDecision
	ConvertOptions = core.ConvertOptions
	ActionOrigin   = core.ActionOrigin
	SubmitRequest  = core.SubmitRequest
	RuntimeEvent   = core.RuntimeEvent
	ActionDetails  = core.ActionDetails
	JobDetails     = core.JobDetails
	CommandDetails = core.CommandDetails
	VMImage        = core.VMImage
	VMProfile      = core.VMProfile
	ProfileRequest = core.ProfileRequest
	ConfiguredVMProfile = core.ConfiguredVMProfile
	VMProfilesConfig = core.VMProfilesConfig
	LegacyRecord   = core.LegacyRecord
)

const (
	FormatActionV1    = core.FormatActionV1
	FormatGitMasterV1 = core.FormatGitMasterV1
)

var (
	ConvertYAML              = core.ConvertYAML
	NewJobGraph              = core.NewJobGraph
	New                      = core.New
	NewRuntime               = core.NewRuntime
	NewRuntimeWithProfiles   = core.NewRuntimeWithProfiles
	LoadVMProfilesConfig     = core.LoadVMProfilesConfig
	NewOutputWriter          = core.NewOutputWriter
	NewGuestCommand          = core.NewGuestCommand
	ImageFormat              = core.ImageFormat
	ListVMImages             = core.ListVMImages
	ValidImageFile           = core.ValidImageFile
	ListVMProfiles           = core.ListVMProfiles
	RegisterVMProfile        = core.RegisterVMProfile
	ConfigurationFingerprint = core.ConfigurationFingerprint
	LegacyPrimaryKey         = core.LegacyPrimaryKey
)