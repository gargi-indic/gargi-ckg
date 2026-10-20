package orchestrator_test

import (
	"errors"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/orchestrator"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestResolveParentClosePolicy(t *testing.T) {
	tests := []struct {
		input    string
		expected enums.ParentClosePolicy
		err      bool
	}{
		{"", enums.PARENT_CLOSE_POLICY_TERMINATE, false},
		{"TERMINATE", enums.PARENT_CLOSE_POLICY_TERMINATE, false},
		{"REQUEST_CANCEL", enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL, false},
		{"ABANDON", enums.PARENT_CLOSE_POLICY_ABANDON, false},
		{"INVALID", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			actual, err := orchestrator.ResolveParentClosePolicy(tt.input)
			if tt.err {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, actual)
			}
		})
	}
}

func TestDecodeSubGraphConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		raw := map[string]any{
			"definition": map[string]any{
				"apiVersion": "graph/v1",
				"id":         "child-graph",
				"nodes": []any{
					map[string]any{"id": "node1", "type": "passthrough"},
				},
			},
			"parentClosePolicy": "ABANDON",
		}

		cfg, err := orchestrator.DecodeSubGraphConfig(raw)
		require.NoError(t, err)
		assert.Equal(t, "ABANDON", cfg.ParentClosePolicy)
		require.NotNil(t, cfg.Definition)
		assert.Equal(t, "child-graph", cfg.Definition.ID)
	})

	t.Run("missing definition", func(t *testing.T) {
		raw := map[string]any{
			"parentClosePolicy": "ABANDON",
		}

		_, err := orchestrator.DecodeSubGraphConfig(raw)
		assert.ErrorContains(t, err, "sub_graph definition is required")
	})

	t.Run("invalid policy", func(t *testing.T) {
		raw := map[string]any{
			"definition": map[string]any{
				"apiVersion": "graph/v1",
				"id":         "child-graph",
				"nodes": []any{
					map[string]any{"id": "node1", "type": "passthrough"},
				},
			},
			"parentClosePolicy": "INVALID",
		}

		_, err := orchestrator.DecodeSubGraphConfig(raw)
		assert.ErrorContains(t, err, "invalid parent close policy")
	})

	t.Run("unknown fields", func(t *testing.T) {
		raw := map[string]any{
			"definition": map[string]any{
				"id": "child-graph",
			},
			"unknown_field": "123",
		}

		_, err := orchestrator.DecodeSubGraphConfig(raw)
		assert.ErrorContains(t, err, "failed to decode config")
	})
}

func setupRegistry() *engine.Registry {
	reg := engine.NewRegistry()
	_ = reg.Register("passthrough", func(_ any) (engine.NodeInterface, error) {
		return &engine.BaseNode{}, nil
	}, nil)
	return reg
}

func TestSubGraphWorkflow(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	// Main graph definition containing sub_graph node
	mainDef := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "main-graph-id",
		Nodes: []engine.NodeDefinition{
			{
				ID:   "sub_node",
				Type: orchestrator.SubGraphNodeType,
				Config: map[string]any{
					"definition": map[string]any{
						"apiVersion": "graph/v1",
						"id":         "child-workflow-id",
						"nodes": []map[string]any{
							{"id": "child_node_1", "type": "passthrough"},
						},
					},
					"parentClosePolicy": "TERMINATE",
				},
			},
		},
	}

	runner := orchestrator.NewWorkflowRunner(nil)

	env.RegisterWorkflowWithOptions(runner.GraphExecutionWorkflow, workflow.RegisterOptions{
		Name: orchestrator.GraphExecutionWorkflowName,
	})

	env.RegisterActivityWithOptions(orchestrator.NewNodeActivities(setupRegistry()).ExecuteNodeActivity, activity.RegisterOptions{
		Name: orchestrator.ExecuteNodeActivityName,
	})

	input := orchestrator.GraphWorkflowInput{
		ExecutionID: "exec-123",
		Definition:  mainDef,
		InitialState: map[string]any{
			"key1": "val1",
		},
	}

	env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var res orchestrator.GraphWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&res))

	assert.Equal(t, engine.StatusSucceeded, res.Status)
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("sub_node").Status())

	out := res.FinalState.NodeState("sub_node").Output()
	require.NotNil(t, out["nodes"])

	// Check output
	nodesOutput, ok := out["nodes"].(map[string]any)
	require.True(t, ok)
	require.NotNil(t, nodesOutput["child_node_1"])
}

func TestSubGraphWorkflow_ChildFailure(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	// Child workflow graph definition that will fail
	// wait_for_signal without timeout will get stuck, but we don't have timeouts here easily
	// Let's make an unknown node type so it fails
	mainDef := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "main-graph-id",
		Nodes: []engine.NodeDefinition{
			{
				ID:   "sub_node",
				Type: orchestrator.SubGraphNodeType,
				Config: map[string]any{
					"definition": map[string]any{
						"apiVersion": "graph/v1",
						"id":         "child-workflow-id",
						"nodes": []map[string]any{
							// This node will fail to instantiate, making the child workflow fail
							{"id": "fail_node", "type": "unknown_type"},
						},
					},
					"parentClosePolicy": "TERMINATE",
				},
			},
		},
	}

	runner := orchestrator.NewWorkflowRunner(nil)

	env.RegisterWorkflowWithOptions(runner.GraphExecutionWorkflow, workflow.RegisterOptions{
		Name: orchestrator.GraphExecutionWorkflowName,
	})

	env.RegisterActivityWithOptions(orchestrator.NewNodeActivities(setupRegistry()).ExecuteNodeActivity, activity.RegisterOptions{
		Name: orchestrator.ExecuteNodeActivityName,
	})

	input := orchestrator.GraphWorkflowInput{
		ExecutionID: "exec-123",
		Definition:  mainDef,
	}

	env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var res orchestrator.GraphWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&res))

	assert.Equal(t, engine.StatusFailed, res.Status)
	assert.Equal(t, engine.StatusFailed, res.FinalState.NodeState("sub_node").Status())
	assert.Contains(t, res.FinalState.NodeState("sub_node").ErrorMessage(), "graph execution failed")
}

func TestMergeChildWorkflowResult(t *testing.T) {
	nodeID := engine.NodeID("sub_node")

	t.Run("success nil input", func(t *testing.T) {
		res, err := orchestrator.MergeChildWorkflowResult(nodeID, nil)
		assert.Error(t, err)
		assert.Nil(t, res)
	})

	t.Run("success state mutation", func(t *testing.T) {
		n1 := &engine.NodeState{}
		_ = n1.UnmarshalJSON([]byte(`{"nodeId":"n1","status":"succeeded","output":{"k1":"v1"},"version":0}`))

		n2 := &engine.NodeState{}
		_ = n2.UnmarshalJSON([]byte(`{"nodeId":"n2","status":"failed","output":{"k2":"v2"},"version":0}`))

		state := &engine.GraphState{}
		_ = state.UnmarshalJSON([]byte(`{"executionId":"test","graphId":"g1","status":"Succeeded","nodes":["n1","n2"],"nodeStates":{"n1":{"nodeId":"n1","status":"Succeeded","output":{"k1":"v1"},"version":0},"n2":{"nodeId":"n2","status":"Failed","output":{"k2":"v2"},"version":0}},"version":0}`))

		childRes := &orchestrator.GraphWorkflowResult{
			Status:     engine.StatusSucceeded,
			FinalState: state,
		}

		res, err := orchestrator.MergeChildWorkflowResult(nodeID, childRes)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, nodeID, res.NodeID)
		require.NotNil(t, res.Mutation)

		expectedMerged := map[string]any{
			"nodes": map[string]any{
				"n1": map[string]any{"k1": "v1"},
			},
			"k1": "v1",
		}

		assert.Equal(t, expectedMerged, res.Mutation.Set)
	})

	t.Run("child failed", func(t *testing.T) {
		childRes := &orchestrator.GraphWorkflowResult{
			Status: engine.StatusFailed,
			Error:  "my error",
		}

		res, err := orchestrator.MergeChildWorkflowResult(nodeID, childRes)
		assert.Error(t, err)
		assert.Nil(t, res)
		assert.Contains(t, err.Error(), "my error")
		assert.Equal(t, engine.ClassNonRetryable, engine.ClassOf(errors.Unwrap(err)))
	})

	t.Run("child compensated", func(t *testing.T) {
		childRes := &orchestrator.GraphWorkflowResult{
			Status: engine.StatusCompensated,
			Error:  "critical failure",
		}

		res, err := orchestrator.MergeChildWorkflowResult(nodeID, childRes)
		assert.Error(t, err)
		assert.Nil(t, res)
		assert.Contains(t, err.Error(), "critical failure")
		assert.Equal(t, engine.ClassCritical, engine.ClassOf(errors.Unwrap(err)))
	})
}
