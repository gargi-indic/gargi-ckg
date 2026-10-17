package orchestrator_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/nodes"
	"github.com/gargi-indic/gargi-ckg/internal/orchestrator"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestGraphExecutionWorkflow_Linear_Diamond(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	reg := engine.NewRegistry()
	_ = nodes.RegisterDefaults(reg, nodes.Dependencies{})

	na := orchestrator.NewNodeActivities(reg)
	env.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{Name: orchestrator.ExecuteNodeActivityName})

	def := &engine.GraphDefinition{
		ID:         "diamond_test",
		APIVersion: "v1",
		Nodes: []engine.NodeDefinition{
			{ID: "A", Type: "transform", Config: map[string]any{"set": map[string]any{"valA": "a"}}},
			{ID: "B", Type: "transform", Config: map[string]any{"set": map[string]any{"valB": "b"}}},
			{ID: "C", Type: "transform", Config: map[string]any{"set": map[string]any{"valC": "c"}}},
			{ID: "D", Type: "passthrough", JoinMode: engine.JoinModeAllOf},
		},
		Edges: []engine.EdgeDefinition{
			{From: "A", To: "B", DataMapping: map[string]string{"valA": "valA"}},
			{From: "A", To: "C", DataMapping: map[string]string{"valA": "valA"}},
			{From: "B", To: "D", DataMapping: map[string]string{"valB": "valB"}},
			{From: "C", To: "D", DataMapping: map[string]string{"valC": "valC"}},
		},
	}

	execID := engine.ExecutionID("exec-1")
	input := orchestrator.GraphWorkflowInput{
		ExecutionID: execID,
		Definition:  def,
	}

	runner := orchestrator.NewWorkflowRunner(reg)
	env.RegisterWorkflow(runner.GraphExecutionWorkflow)

	env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, input)

	if !env.IsWorkflowCompleted() {
		t.Fatalf("Workflow did not complete")
	}

	err := env.GetWorkflowError()
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	var result orchestrator.GraphWorkflowResult
	err = env.GetWorkflowResult(&result)
	if err != nil {
		t.Fatalf("Failed to get workflow result: %v", err)
	}

	if result.Status != engine.StatusSucceeded {
		t.Errorf("Expected status Succeeded, got %s", result.Status)
	}
	if result.FinalState == nil {
		t.Fatalf("Final state is nil")
	}

	nodeDState := result.FinalState.NodeState("D")
	if nodeDState == nil {
		t.Fatalf("Node D state not found")
	}
	if nodeDState.Status() != engine.StatusSucceeded {
		t.Errorf("Expected Node D status Succeeded, got %s", nodeDState.Status())
	}
}

func TestGraphExecutionWorkflow_Conditional_Branch(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	reg := engine.NewRegistry()
	_ = nodes.RegisterDefaults(reg, nodes.Dependencies{})

	na := orchestrator.NewNodeActivities(reg)
	env.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{Name: orchestrator.ExecuteNodeActivityName})

	def := &engine.GraphDefinition{
		ID:         "conditional_test",
		APIVersion: "v1",
		Nodes: []engine.NodeDefinition{
			{ID: "Root", Type: "transform", Config: map[string]any{"set": map[string]any{"choice": "true_branch"}}},
			{ID: "BranchTrue", Type: "passthrough"},
			{ID: "BranchFalse", Type: "passthrough"},
		},
		Edges: []engine.EdgeDefinition{
			{From: "Root", To: "BranchTrue", Condition: "upstream.output.choice == 'true_branch'"},
			{From: "Root", To: "BranchFalse", Condition: "upstream.output.choice == 'false_branch'"},
		},
	}

	input := orchestrator.GraphWorkflowInput{
		ExecutionID: engine.ExecutionID("exec-2"),
		Definition:  def,
	}

	runner := orchestrator.NewWorkflowRunner(reg)
	env.RegisterWorkflow(runner.GraphExecutionWorkflow)

	env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, input)

	if !env.IsWorkflowCompleted() {
		t.Fatalf("Workflow did not complete")
	}

	var result orchestrator.GraphWorkflowResult
	_ = env.GetWorkflowResult(&result)

	if result.Status != engine.StatusSucceeded {
		t.Errorf("Expected status Succeeded, got %s", result.Status)
	}
	if result.FinalState.NodeState("BranchTrue").Status() != engine.StatusSucceeded {
		t.Errorf("Expected BranchTrue to succeed, got %s", result.FinalState.NodeState("BranchTrue").Status())
	}
	if result.FinalState.NodeState("BranchFalse").Status() != engine.StatusSkipped {
		t.Errorf("Expected BranchFalse to be skipped, got %s", result.FinalState.NodeState("BranchFalse").Status())
	}
}

func TestGraphExecutionWorkflow_InitialState(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	reg := engine.NewRegistry()
	_ = nodes.RegisterDefaults(reg, nodes.Dependencies{})

	na := orchestrator.NewNodeActivities(reg)
	env.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{Name: orchestrator.ExecuteNodeActivityName})

	def := &engine.GraphDefinition{
		ID:         "initial_state_test",
		APIVersion: "v1",
		Nodes: []engine.NodeDefinition{
			{ID: "Root", Type: "passthrough"},
		},
	}

	input := orchestrator.GraphWorkflowInput{
		ExecutionID:  engine.ExecutionID("exec-3"),
		Definition:   def,
		InitialState: map[string]any{"injected_key": "injected_val"},
	}

	runner := orchestrator.NewWorkflowRunner(reg)
	env.RegisterWorkflow(runner.GraphExecutionWorkflow)

	env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, input)

	var result orchestrator.GraphWorkflowResult
	_ = env.GetWorkflowResult(&result)

	nodeRootState := result.FinalState.NodeState("Root")
	out := nodeRootState.Output()
	if out["injected_key"] != "injected_val" {
		t.Errorf("Expected injected_val, got %v", out["injected_key"])
	}
}

func TestGraphExecutionWorkflow_FailurePolicy_Compensate(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	reg := engine.NewRegistry()
	_ = nodes.RegisterDefaults(reg, nodes.Dependencies{})

	_ = reg.Register("critical_fail_node", func(_ any) (engine.NodeInterface, error) {
		return &mockCriticalFailNode{}, nil
	}, nil)
	_ = reg.Register("comp_node", func(_ any) (engine.NodeInterface, error) {
		return &mockCompNode{}, nil
	}, nil)

	na := orchestrator.NewNodeActivities(reg)
	env.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{Name: orchestrator.ExecuteNodeActivityName})

	def := &engine.GraphDefinition{
		ID:         "failure_policy_test",
		APIVersion: "v1",
		Nodes: []engine.NodeDefinition{
			{ID: "Failer", Type: "critical_fail_node", Compensation: "comp_node"},
		},
		Compensations: map[string]engine.CompensationDefinition{
			"comp_node": {Type: "comp_node"},
		},
		FailurePolicy: &engine.FailurePolicy{
			OnCriticalFailure: "compensate",
		},
	}

	input := orchestrator.GraphWorkflowInput{
		ExecutionID: engine.ExecutionID("exec-4"),
		Definition:  def,
	}

	runner := orchestrator.NewWorkflowRunner(reg)
	env.RegisterWorkflow(runner.GraphExecutionWorkflow)

	env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, input)

	var result orchestrator.GraphWorkflowResult
	err := env.GetWorkflowResult(&result)
	if err != nil {
		t.Fatalf("Workflow failed unexpectedly: %v", err)
	}

	if result.Status != engine.StatusCompensating {
		t.Errorf("Expected status Compensating, got %s", result.Status)
	}
}

type mockCriticalFailNode struct{}

func (m *mockCriticalFailNode) OnInit(_ context.Context, _ engine.NodeContext) error { return nil }
func (m *mockCriticalFailNode) EvaluateReactivity(_ context.Context, _ engine.NodeContext) (bool, error) {
	return true, nil
}

func (m *mockCriticalFailNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	return nil, engine.NewCriticalError(fmt.Errorf("boom"))
}

func (m *mockCriticalFailNode) OnSuccess(_ context.Context, _ engine.NodeContext, _ *engine.StateMutation) error {
	return nil
}

func (m *mockCriticalFailNode) OnFailure(_ context.Context, _ engine.NodeContext, _ error) error {
	return nil
}

type mockCompNode struct{}

func (m *mockCompNode) OnInit(_ context.Context, _ engine.NodeContext) error { return nil }
func (m *mockCompNode) EvaluateReactivity(_ context.Context, _ engine.NodeContext) (bool, error) {
	return true, nil
}

func (m *mockCompNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	return nil, nil
}

func (m *mockCompNode) OnSuccess(_ context.Context, _ engine.NodeContext, _ *engine.StateMutation) error {
	return nil
}

func (m *mockCompNode) OnFailure(_ context.Context, _ engine.NodeContext, _ error) error {
	return nil
}
