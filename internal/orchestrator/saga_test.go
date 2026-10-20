package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/orchestrator"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestSagaCompensation_ReverseOrder(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	execID, _ := engine.NewExecutionID()

	def := &engine.GraphDefinition{
		ID: "g1",
		Nodes: []engine.NodeDefinition{
			{ID: "N1", Type: "pass", Compensation: "comp1"},
			{ID: "N2", Type: "pass", Compensation: "comp2"},
			{ID: "N3", Type: "fail"},
		},
		Edges: []engine.EdgeDefinition{
			{From: "N1", To: "N2"},
			{From: "N2", To: "N3"},
		},
		Compensations: map[string]engine.CompensationDefinition{
			"comp1": {Type: "compNode1"},
			"comp2": {Type: "compNode2"},
		},
		FailurePolicy: &engine.FailurePolicy{
			OnCriticalFailure: "compensate",
		},
	}

	comp1Executed := false
	comp2Executed := false

	env.RegisterActivityWithOptions(
		func(_ context.Context, input orchestrator.NodeActivityInput) (*orchestrator.NodeActivityResult, error) {
			if input.NodeID == "N3" {
				return nil, orchestrator.WrapTemporalError(engine.NewCriticalError(errors.New("n3 failed")))
			}
			if input.NodeType == "compNode1" {
				require.True(t, comp2Executed, "comp1 should execute AFTER comp2")
				comp1Executed = true
				return &orchestrator.NodeActivityResult{NodeID: input.NodeID}, nil
			}
			if input.NodeType == "compNode2" {
				require.False(t, comp1Executed, "comp2 should execute BEFORE comp1")
				comp2Executed = true
				return &orchestrator.NodeActivityResult{NodeID: input.NodeID}, nil
			}
			return &orchestrator.NodeActivityResult{NodeID: input.NodeID}, nil
		},
		activity.RegisterOptions{Name: orchestrator.ExecuteNodeActivityName},
	)

	env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflow, orchestrator.GraphWorkflowInput{
		ExecutionID: execID,
		Definition:  def,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var res orchestrator.GraphWorkflowResult
	err := env.GetWorkflowResult(&res)
	require.NoError(t, err)

	assert.Equal(t, engine.StatusCompensated, res.Status)
	assert.True(t, comp1Executed)
	assert.True(t, comp2Executed)

	ns1 := res.FinalState.NodeState("N1")
	assert.Equal(t, engine.StatusCompensated, ns1.Status())
	ns2 := res.FinalState.NodeState("N2")
	assert.Equal(t, engine.StatusCompensated, ns2.Status())
	ns3 := res.FinalState.NodeState("N3")
	assert.Equal(t, engine.StatusFailed, ns3.Status())
}

func TestSagaCompensation_ContinueOnFailure(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	execID, _ := engine.NewExecutionID()

	def := &engine.GraphDefinition{
		ID: "g1",
		Nodes: []engine.NodeDefinition{
			{ID: "N1", Type: "pass", Compensation: "comp1"},
			{ID: "N2", Type: "pass", Compensation: "comp2"},
			{ID: "N3", Type: "fail"},
		},
		Edges: []engine.EdgeDefinition{
			{From: "N1", To: "N2"},
			{From: "N2", To: "N3"},
		},
		Compensations: map[string]engine.CompensationDefinition{
			"comp1": {Type: "compNode1"},
			"comp2": {Type: "compNode2"},
		},
		FailurePolicy: &engine.FailurePolicy{
			OnCriticalFailure: "compensate",
		},
	}

	comp1Executed := false
	comp2Executed := false

	env.RegisterActivityWithOptions(
		func(_ context.Context, input orchestrator.NodeActivityInput) (*orchestrator.NodeActivityResult, error) {
			if input.NodeID == "N3" {
				return nil, orchestrator.WrapTemporalError(engine.NewCriticalError(errors.New("n3 failed")))
			}
			if input.NodeType == "compNode1" {
				require.True(t, comp2Executed, "comp1 should execute AFTER comp2")
				comp1Executed = true
				return &orchestrator.NodeActivityResult{NodeID: input.NodeID}, nil
			}
			if input.NodeType == "compNode2" {
				comp2Executed = true
				return nil, errors.New("comp2 failed")
			}
			return &orchestrator.NodeActivityResult{NodeID: input.NodeID}, nil
		},
		activity.RegisterOptions{Name: orchestrator.ExecuteNodeActivityName},
	)

	env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflow, orchestrator.GraphWorkflowInput{
		ExecutionID: execID,
		Definition:  def,
	})

	require.True(t, env.IsWorkflowCompleted())

	var res orchestrator.GraphWorkflowResult
	err := env.GetWorkflowResult(&res)
	require.NoError(t, err)

	assert.Equal(t, engine.StatusCompensated, res.Status)
	assert.Contains(t, res.Error, "comp2 failed")
	assert.True(t, comp1Executed)
	assert.True(t, comp2Executed)

	ns1 := res.FinalState.NodeState("N1")
	assert.Equal(t, engine.StatusCompensated, ns1.Status())
	ns2 := res.FinalState.NodeState("N2")
	assert.Equal(t, engine.StatusCompensated, ns2.Status())
	assert.Contains(t, ns2.ErrorMessage(), "comp2 failed")
}
