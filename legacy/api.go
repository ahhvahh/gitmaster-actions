// Package legacy retains the old GitMaster import path for compatibility.
// The original implementation is in /src/legacy until the consumer migrates.
package legacy

import impl "github.com/ahhvahh/gitmaster-actions/src/legacy"

type (
	ExecutionLogAdapter   = impl.ExecutionLogAdapter
	Store                 = impl.Store
	GuestCommandRunner    = impl.GuestCommandRunner
	ActionCommandPlan     = impl.ActionCommandPlan
	ActionJobPlan         = impl.ActionJobPlan
	ActionPlan            = impl.ActionPlan
	ActionCommandResult   = impl.ActionCommandResult
	ActionJobResult       = impl.ActionJobResult
	ActionResult          = impl.ActionResult
	ActionRuntimeMessage  = impl.ActionRuntimeMessage
	InterruptedInsulaVM   = impl.InterruptedInsulaVM
	ArchiveProvider       = impl.ArchiveProvider
	ActionCommandResponse = impl.ActionCommandResponse
)

var (
	NewStore               = impl.NewStore
	EnsureActionSchema     = impl.EnsureActionSchema
	ExecuteAction          = impl.ExecuteAction
	ExecuteClaimedActionAt = impl.ExecuteClaimedActionAt
	GuestWorkingDirectory  = impl.GuestWorkingDirectory
	RunActionViaInsula     = impl.RunActionViaInsula
	StartInsulaWorker      = impl.StartInsulaWorker
	ReconcileInsulaVMs     = impl.ReconcileInsulaVMs
)