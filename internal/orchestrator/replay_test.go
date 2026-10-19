package orchestrator_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/gargi-indic/gargi-ckg/internal/orchestrator"
)

// IncompatibleWorkflow simulates a developer modifying workflow logic without version gating
// (e.g. inserting an unexpected timer command into the execution sequence).
func IncompatibleWorkflow(ctx workflow.Context, input orchestrator.GraphWorkflowInput) (*orchestrator.GraphWorkflowResult, error) {
	_ = workflow.Sleep(ctx, 10*time.Second)
	return orchestrator.GraphExecutionWorkflow(ctx, input)
}

// DeprecatedWorkflow simulates a workflow where VersionInitial has been deprecated
// by advancing minSupported to VersionV1.
func DeprecatedWorkflow(ctx workflow.Context, input orchestrator.GraphWorkflowInput) (*orchestrator.GraphWorkflowResult, error) {
	_ = orchestrator.GetWorkflowVersion(ctx, orchestrator.ChangeIDGraphWorkflowVersion, orchestrator.VersionV1, orchestrator.VersionV1)
	return orchestrator.GraphExecutionWorkflow(ctx, input)
}

func TestWorkflowReplay_UnversionedHistory(t *testing.T) {
	historyPath := filepath.Join("testdata", "graph_workflow_unversioned_history.json")

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(orchestrator.GraphExecutionWorkflow)

	err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, historyPath)
	require.NoError(t, err, "unversioned historical execution must replay deterministically without errors")
}

func TestWorkflowReplay_V1History(t *testing.T) {
	historyPath := filepath.Join("testdata", "graph_workflow_v1_history.json")

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(orchestrator.GraphExecutionWorkflow)

	err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, historyPath)
	require.NoError(t, err, "V1 versioned execution must replay deterministically without errors")
}

func TestWorkflowReplay_RecordedHistoryV1(t *testing.T) {
	historyPath := filepath.Join("testdata", "recorded_history_v1.json")

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(orchestrator.GraphExecutionWorkflow)

	err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, historyPath)
	require.NoError(t, err, "recorded v1 historical execution must replay deterministically without errors")
}

func TestWorkflowReplay_IncompatibleChangeDetectsNonDeterminism(t *testing.T) {
	historyPath := filepath.Join("testdata", "graph_workflow_unversioned_history.json")

	replayer := worker.NewWorkflowReplayer()
	// Register the incompatible workflow under the workflow type name recorded in the history
	replayer.RegisterWorkflowWithOptions(IncompatibleWorkflow, workflow.RegisterOptions{
		Name: orchestrator.GraphExecutionWorkflowName,
	})

	err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, historyPath)
	require.Error(t, err, "replayer must detect non-deterministic command sequence changes")
	require.Contains(t, err.Error(), "TMPRL1100", "error message should cite temporal replay mismatch error code")
}

func TestWorkflowReplay_Deprecation_FailsForPreV1History(t *testing.T) {
	unversionedHistoryPath := filepath.Join("testdata", "graph_workflow_unversioned_history.json")

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflowWithOptions(DeprecatedWorkflow, workflow.RegisterOptions{
		Name: orchestrator.GraphExecutionWorkflowName,
	})

	err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, unversionedHistoryPath)
	require.Error(t, err, "replaying pre-V1 execution against workflow with minSupported=VersionV1 must fail safely")
}

func TestWorkflowReplay_Deprecation_SucceedsForV1History(t *testing.T) {
	v1HistoryPath := filepath.Join("testdata", "recorded_history_v1.json")

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflowWithOptions(DeprecatedWorkflow, workflow.RegisterOptions{
		Name: orchestrator.GraphExecutionWorkflowName,
	})

	err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, v1HistoryPath)
	require.NoError(t, err, "replaying V1 execution against workflow with minSupported=VersionV1 must succeed")
}
