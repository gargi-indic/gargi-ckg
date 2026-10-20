package orchestrator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/internal/orchestrator"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

// versionEvolutionWorkflow demonstrates safe evolution across multiple workflow versions.
func versionEvolutionWorkflow(ctx workflow.Context) (string, error) {
	v := orchestrator.GetWorkflowVersion(ctx, "evolution-change", orchestrator.VersionInitial, orchestrator.VersionV2)

	switch v {
	case orchestrator.VersionInitial:
		return "legacy-path", nil
	case orchestrator.VersionV1:
		return "v1-path", nil
	default:
		return "v2-path", nil
	}
}

func TestVersion_Helpers(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}

	t.Run("Default execution returns maxSupported", func(t *testing.T) {
		env := suite.NewTestWorkflowEnvironment()
		workflowFunc := func(ctx workflow.Context) (workflow.Version, error) {
			v := orchestrator.GetWorkflowVersion(ctx, "test-change", orchestrator.VersionInitial, orchestrator.VersionV1)
			return v, nil
		}

		env.ExecuteWorkflow(workflowFunc)
		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())

		var result workflow.Version
		require.NoError(t, env.GetWorkflowResult(&result))
		assert.Equal(t, orchestrator.VersionV1, result)
	})

	t.Run("IsVersionAtLeast returns true when version equals required", func(t *testing.T) {
		env := suite.NewTestWorkflowEnvironment()
		env.OnGetVersion("test-change", orchestrator.VersionInitial, orchestrator.VersionV1).Return(orchestrator.VersionV1)

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
	})

	t.Run("IsVersionAtLeast returns true when version exceeds required", func(t *testing.T) {
		env := suite.NewTestWorkflowEnvironment()
		env.OnGetVersion("test-change", orchestrator.VersionInitial, orchestrator.VersionV2).Return(orchestrator.VersionV2)

		helperTestWorkflow := func(ctx workflow.Context) (bool, error) {
			isAtLeastV1 := orchestrator.IsVersionAtLeast(ctx, "test-change", orchestrator.VersionInitial, orchestrator.VersionV2, orchestrator.VersionV1)
			return isAtLeastV1, nil
		}

		env.ExecuteWorkflow(helperTestWorkflow)
		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())

		var result bool
		require.NoError(t, env.GetWorkflowResult(&result))
		assert.True(t, result)
	})

	t.Run("IsVersionAtLeast returns false when version is below required", func(t *testing.T) {
		env := suite.NewTestWorkflowEnvironment()
		env.OnGetVersion("test-change", orchestrator.VersionInitial, orchestrator.VersionV2).Return(orchestrator.VersionInitial)

		helperTestWorkflow := func(ctx workflow.Context) (bool, error) {
			isAtLeastV1 := orchestrator.IsVersionAtLeast(ctx, "test-change", orchestrator.VersionInitial, orchestrator.VersionV2, orchestrator.VersionV1)
			return isAtLeastV1, nil
		}

		env.ExecuteWorkflow(helperTestWorkflow)
		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())

		var result bool
		require.NoError(t, env.GetWorkflowResult(&result))
		assert.False(t, result)
	})
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

func TestVersion_DeprecationStep_LockToMinVersion(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	// Simulating Phase 3 where minSupported is locked to VersionV1
	deprecatedWorkflow := func(ctx workflow.Context) (workflow.Version, error) {
		v := orchestrator.GetWorkflowVersion(ctx, "deprecated-change", orchestrator.VersionV1, orchestrator.VersionV1)
		return v, nil
	}

	env.ExecuteWorkflow(deprecatedWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var res workflow.Version
	require.NoError(t, env.GetWorkflowResult(&res))
	assert.Equal(t, orchestrator.VersionV1, res)
}

func TestVersion_MultipleChangeIDs(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	multiChangeWorkflow := func(ctx workflow.Context) (map[string]bool, error) {
		hasGraphV1 := orchestrator.IsVersionAtLeast(
			ctx,
			orchestrator.ChangeIDGraphWorkflowVersion,
			orchestrator.VersionInitial,
			orchestrator.VersionV1,
			orchestrator.VersionV1,
		)
		hasPayloadVal := orchestrator.IsVersionAtLeast(
			ctx,
			orchestrator.ChangeIDPayloadValidation,
			orchestrator.VersionInitial,
			orchestrator.VersionV1,
			orchestrator.VersionV1,
		)

		return map[string]bool{
			"graph":   hasGraphV1,
			"payload": hasPayloadVal,
		}, nil
	}

	env.OnGetVersion(orchestrator.ChangeIDGraphWorkflowVersion, orchestrator.VersionInitial, orchestrator.VersionV1).Return(orchestrator.VersionV1)
	env.OnGetVersion(orchestrator.ChangeIDPayloadValidation, orchestrator.VersionInitial, orchestrator.VersionV1).Return(orchestrator.VersionInitial)

	env.ExecuteWorkflow(multiChangeWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var res map[string]bool
	require.NoError(t, env.GetWorkflowResult(&res))
	assert.True(t, res["graph"])
	assert.False(t, res["payload"])
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
