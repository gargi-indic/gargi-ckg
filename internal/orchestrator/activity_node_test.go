package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

type mockNode struct {
	onInitErr    error
	evalTrue     bool
	evalErr      error
	executeMut   *engine.StateMutation
	executeErr   error
	onSuccessErr error
	onFailureErr error

	onInitCalled    bool
	executeCalled   bool
	onSuccessCalled bool
	onFailureCalled bool
}

func (m *mockNode) OnInit(_ context.Context, _ engine.NodeContext) error {
	m.onInitCalled = true
	return m.onInitErr
}

func (m *mockNode) EvaluateReactivity(_ context.Context, _ engine.NodeContext) (bool, error) {
	return m.evalTrue, m.evalErr
}

func (m *mockNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	m.executeCalled = true
	return m.executeMut, m.executeErr
}

func (m *mockNode) OnSuccess(_ context.Context, _ engine.NodeContext, _ *engine.StateMutation) error {
	m.onSuccessCalled = true
	return m.onSuccessErr
}

func (m *mockNode) OnFailure(_ context.Context, _ engine.NodeContext, _ error) error {
	m.onFailureCalled = true
	return m.onFailureErr
}

func TestExecuteNodeActivity_Success(t *testing.T) {
	registry := engine.NewRegistry()

	mockMut := &engine.StateMutation{
		NodeID: "node1",
	}

	var mNode *mockNode

	factory := func(_ any) (engine.NodeInterface, error) {
		mNode = &mockNode{
			evalTrue:   true,
			executeMut: mockMut,
		}
		return mNode, nil
	}

	err := registry.Register("mockType", factory, nil)
	require.NoError(t, err)

	am := NewNodeActivities(registry)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(am.ExecuteNodeActivity)

	input := NodeActivityInput{
		ExecutionID: "exec-1",
		NodeID:      "node1",
		NodeType:    "mockType",
		Config:      map[string]any{},
		Inputs:      map[string]any{},
		GraphState:  nil,
	}

	val, err := env.ExecuteActivity(am.ExecuteNodeActivity, input)
	require.NoError(t, err)

	var output NodeActivityResult
	err = val.Get(&output)
	require.NoError(t, err)

	assert.Equal(t, mockMut, output.Mutation)
	assert.True(t, mNode.onInitCalled)
	assert.True(t, mNode.executeCalled)
	assert.True(t, mNode.onSuccessCalled)
	assert.False(t, mNode.onFailureCalled)
}

func TestExecuteNodeActivity_OnInitFailure(t *testing.T) {
	registry := engine.NewRegistry()

	myErr := errors.New("init failed")
	var mNode *mockNode

	factory := func(_ any) (engine.NodeInterface, error) {
		mNode = &mockNode{
			onInitErr: myErr,
		}
		return mNode, nil
	}

	err := registry.Register("mockType", factory, nil)
	require.NoError(t, err)

	am := NewNodeActivities(registry)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(am.ExecuteNodeActivity)

	input := NodeActivityInput{
		ExecutionID: "exec-1",
		NodeID:      "node1",
		NodeType:    "mockType",
		Config:      map[string]any{},
		Inputs:      map[string]any{},
		Attempt:     1,
		MaxAttempts: 1,
	}

	_, err = env.ExecuteActivity(am.ExecuteNodeActivity, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), myErr.Error())
	assert.True(t, mNode.onInitCalled)
	assert.False(t, mNode.executeCalled)
	assert.True(t, mNode.onFailureCalled)
}

func TestExecuteNodeActivity_ExecuteFailure(t *testing.T) {
	registry := engine.NewRegistry()

	myErr := errors.New("exec failed")
	var mNode *mockNode

	factory := func(_ any) (engine.NodeInterface, error) {
		mNode = &mockNode{
			executeErr: myErr,
		}
		return mNode, nil
	}

	err := registry.Register("mockType", factory, nil)
	require.NoError(t, err)

	am := NewNodeActivities(registry)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(am.ExecuteNodeActivity)

	input := NodeActivityInput{
		ExecutionID: "exec-1",
		NodeID:      "node1",
		NodeType:    "mockType",
		Config:      map[string]any{},
		Inputs:      map[string]any{},
		Attempt:     1,
		MaxAttempts: 1,
	}

	_, err = env.ExecuteActivity(am.ExecuteNodeActivity, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), myErr.Error())
	assert.True(t, mNode.onInitCalled)
	assert.True(t, mNode.executeCalled)
	assert.False(t, mNode.onSuccessCalled)
	assert.True(t, mNode.onFailureCalled)
}

func TestExecuteNodeActivity_InvalidMutation(t *testing.T) {
	registry := engine.NewRegistry()

	status := engine.StatusSucceeded
	mockMut := &engine.StateMutation{
		NodeID: "node1",
		Status: &status,
	}

	var mNode *mockNode

	factory := func(_ any) (engine.NodeInterface, error) {
		mNode = &mockNode{
			evalTrue:   true,
			executeMut: mockMut,
		}
		return mNode, nil
	}

	err := registry.Register("mockType", factory, nil)
	require.NoError(t, err)

	am := NewNodeActivities(registry)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(am.ExecuteNodeActivity)

	input := NodeActivityInput{
		ExecutionID: "exec-1",
		NodeID:      "node1",
		NodeType:    "mockType",
		Config:      map[string]any{},
		Inputs:      map[string]any{},
	}

	_, err = env.ExecuteActivity(am.ExecuteNodeActivity, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "may not set Status directly")
	assert.True(t, mNode.onFailureCalled)
}

func TestBuildNodeActivityOptions(t *testing.T) {
	base := workflow.ActivityOptions{
		StartToCloseTimeout: 1 * time.Minute,
	}

	def := &engine.NodeDefinition{}
	opts := BuildNodeActivityOptions(base, def)
	assert.Equal(t, 1*time.Minute, opts.StartToCloseTimeout)
	assert.Equal(t, DefaultHeartbeatTimeout, opts.HeartbeatTimeout)

	stc := 2000
	stc2 := 3000
	def2 := &engine.NodeDefinition{
		StartToCloseTimeoutMs:    &stc,
		ScheduleToCloseTimeoutMs: &stc2,
	}

	opts2 := BuildNodeActivityOptions(base, def2)
	assert.Equal(t, 2*time.Second, opts2.StartToCloseTimeout)
	assert.Equal(t, 3*time.Second, opts2.ScheduleToCloseTimeout)

	baseEmpty := workflow.ActivityOptions{}
	opts3 := BuildNodeActivityOptions(baseEmpty, nil)
	assert.Equal(t, DefaultStartToCloseTimeout, opts3.StartToCloseTimeout)
}

type heartbeatingNode struct {
	mockNode
}

func (h *heartbeatingNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	h.executeCalled = true
	// Wait a bit so heartbeats have a chance to fire
	time.Sleep(20 * time.Millisecond)
	return h.executeMut, h.executeErr
}

func TestExecuteNodeActivity_Heartbeating(t *testing.T) {
	registry := engine.NewRegistry()

	mockMut := &engine.StateMutation{
		NodeID: "node1",
	}

	var mNode *heartbeatingNode

	factory := func(_ any) (engine.NodeInterface, error) {
		mNode = &heartbeatingNode{
			mockNode: mockNode{
				evalTrue:   true,
				executeMut: mockMut,
			},
		}
		return mNode, nil
	}

	err := registry.Register("mockType", factory, nil)
	require.NoError(t, err)

	am := NewNodeActivities(registry)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivityWithOptions(am.ExecuteNodeActivity, activity.RegisterOptions{})

	heartbeats := 0
	env.SetOnActivityHeartbeatListener(func(_ *activity.Info, _ converter.EncodedValues) {
		heartbeats++
	})

	input := NodeActivityInput{
		ExecutionID:       "exec-1",
		NodeID:            "node1",
		NodeType:          "mockType",
		Config:            map[string]any{},
		Inputs:            map[string]any{},
		GraphState:        nil,
		HeartbeatInterval: 5 * time.Millisecond,
	}

	val, err := env.ExecuteActivity(am.ExecuteNodeActivity, input)
	require.NoError(t, err)

	var output NodeActivityResult
	err = val.Get(&output)
	require.NoError(t, err)

	assert.Equal(t, mockMut, output.Mutation)
	assert.True(t, mNode.onInitCalled)
	assert.True(t, mNode.executeCalled)
	assert.True(t, mNode.onSuccessCalled)
	assert.Greater(t, heartbeats, 0, "expected at least one heartbeat")
}
