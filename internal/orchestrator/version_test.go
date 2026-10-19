package orchestrator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/gargi-indic/gargi-ckg/internal/nodes"
	"github.com/gargi-indic/gargi-ckg/internal/orchestrator"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

// versionEvolutionWorkflow demonstrates safe evolution across multiple workflow versions.
func versionEvolutionWorkflow(ctx workflow.Context) (string, error) {
	v := orchestrator.GetWorkflowVersion(ctx, "evolution-change", orchestrator.VersionInitial, orchestrator.VersionV2)

	if v == orchestrator.VersionInitial {
		return "legacy-path", nil
	} else if v == orchestrator.VersionV1 {
		return "v1-path", nil
	}
	return "v2-path", nil
}

func TestVersion_Helpers(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	helperTestWorkflow := func(ctx workflow.Context) (bool, error) {
		isAtLeastV1 := orchestrator.IsVersionAtLeast(ctx, "test-change", orchestrator.VersionInitial, orchestrator.VersionV1, orchestrator.VersionV1)
		return isAtLeastV1, nil
	}

	env.ExecuteWorkflow(helperTestWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result bool
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result)
}

func TestVersion_EvolutionWorkflow_LegacyAndV1AndV2(t *testing.T) {
	tests := []struct {
		name          string
		mockVersion   workflow.Version
		expectedRoute string
	}{
		{
			name:          "Replay or execution on legacy version",
			mockVersion:   orchestrator.VersionInitial,
			expectedRoute: "legacy-path",
		},
		{
			name:          "Execution on version V1",
			mockVersion:   orchestrator.VersionV1,
			expectedRoute: "v1-path",
		},
		{
			name:          "Execution on version V2",
			mockVersion:   orchestrator.VersionV2,
			expectedRoute: "v2-path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			suite := &testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()

			env.OnGetVersion("evolution-change", orchestrator.VersionInitial, orchestrator.VersionV2).Return(tc.mockVersion)

			env.ExecuteWorkflow(versionEvolutionWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			require.NoError(t, env.GetWorkflowError())

			var res string
			require.NoError(t, env.GetWorkflowResult(&res))
			assert.Equal(t, tc.expectedRoute, res)
		})
	}
}

func TestGraphExecutionWorkflow_VersionGating(t *testing.T) {
	tests := []struct {
		name        string
		mockVersion workflow.Version
	}{
		{
			name:        "Legacy VersionInitial execution",
			mockVersion: orchestrator.VersionInitial,
		},
		{
			name:        "VersionV1 execution",
			mockVersion: orchestrator.VersionV1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			suite := &testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()

			reg := engine.NewRegistry()
			_ = nodes.RegisterDefaults(reg, nodes.Dependencies{Signals: orchestrator.NewTemporalSignalSource()})

			na := orchestrator.NewNodeActivities(reg)
			env.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{Name: orchestrator.ExecuteNodeActivityName})

			env.OnGetVersion(orchestrator.ChangeIDGraphWorkflowVersion, orchestrator.VersionInitial, orchestrator.VersionV1).Return(tc.mockVersion)

			def := &engine.GraphDefinition{
				ID:         "version_gate_graph",
				APIVersion: "v1",
				Nodes: []engine.NodeDefinition{
					{
						ID:   "n1",
						Type: "transform",
						Config: map[string]any{
							"set": map[string]any{"status": "ok"},
						},
					},
				},
			}

			input := orchestrator.GraphWorkflowInput{
				ExecutionID: "exec-version-gate",
				Definition:  def,
			}

			env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflow, input)
			require.True(t, env.IsWorkflowCompleted())
			require.NoError(t, env.GetWorkflowError())

			var res orchestrator.GraphWorkflowResult
			require.NoError(t, env.GetWorkflowResult(&res))
			assert.Equal(t, engine.StatusSucceeded, res.Status)
			require.NotNil(t, res.FinalState)
			ns1 := res.FinalState.NodeState("n1")
			require.NotNil(t, ns1)
			assert.Equal(t, engine.StatusSucceeded, ns1.Status())
			val, exists := ns1.Output()["status"]
			assert.True(t, exists)
			assert.Equal(t, "ok", val)
		})
	}
}
