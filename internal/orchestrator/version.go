package orchestrator

import (
	"go.temporal.io/sdk/workflow"
)

// Workflow versioning identifiers.
// Change IDs are unique identifiers passed to workflow.GetVersion.
const (
	// ChangeIDGraphWorkflowVersion is the change ID for graph execution workflow evolution.
	ChangeIDGraphWorkflowVersion = "gargi-graph-workflow-version"

	// ChangeIDPayloadValidation is the change ID for payload validation gating.
	ChangeIDPayloadValidation = "gargi-payload-validation"
)

// Standard workflow versions.
const (
	// VersionInitial represents the baseline workflow version before explicit versioning was added (workflow.DefaultVersion = -1).
	VersionInitial workflow.Version = workflow.DefaultVersion

	// VersionV1 represents the first versioned milestone for GraphExecutionWorkflow.
	VersionV1 workflow.Version = 1

	// VersionV2 represents a subsequent version milestone.
	VersionV2 workflow.Version = 2
)

// GetWorkflowVersion wraps workflow.GetVersion with Gargi versioning conventions.
// It retrieves the version for a given changeID within [minSupported, maxSupported].
// When executed for the first time, maxSupported is recorded into the workflow history as a marker event.
// On replay, the version recorded in history is returned, preserving deterministic execution.
func GetWorkflowVersion(
	ctx workflow.Context,
	changeID string,
	minSupported, maxSupported workflow.Version,
) workflow.Version {
	return workflow.GetVersion(ctx, changeID, minSupported, maxSupported)
}

// IsVersionAtLeast checks if the current workflow execution has negotiated at least the required version
// for the given changeID.
func IsVersionAtLeast(
	ctx workflow.Context,
	changeID string,
	minSupported, maxSupported, required workflow.Version,
) bool {
	v := workflow.GetVersion(ctx, changeID, minSupported, maxSupported)
	return v >= required
}
