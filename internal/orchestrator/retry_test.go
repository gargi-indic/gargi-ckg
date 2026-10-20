package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestBuildActivityRetryPolicy(t *testing.T) {
	t.Run("nil policies use defaults", func(t *testing.T) {
		rp := BuildActivityRetryPolicy(nil, nil)
		assert.Equal(t, DefaultRetryInitialInterval, rp.InitialInterval)
		assert.Equal(t, DefaultRetryBackoffCoefficient, rp.BackoffCoefficient)
		assert.Equal(t, DefaultRetryMaxInterval, rp.MaximumInterval)
		assert.Equal(t, int32(DefaultRetryMaxAttempts), rp.MaximumAttempts)
		assert.Equal(t, []string{ErrorTypeNonRetryable, ErrorTypeCritical}, rp.NonRetryableErrorTypes)
	})

	t.Run("default policy overrides defaults", func(t *testing.T) {
		initInterval := 500
		backoff := 3.0
		maxInterval := 10000
		maxAttempts := 5

		defPol := &engine.RetryPolicy{
			InitialIntervalMs:  &initInterval,
			BackoffCoefficient: &backoff,
			MaxIntervalMs:      &maxInterval,
			MaxAttempts:        &maxAttempts,
		}

		rp := BuildActivityRetryPolicy(nil, defPol)
		assert.Equal(t, 500*time.Millisecond, rp.InitialInterval)
		assert.Equal(t, 3.0, rp.BackoffCoefficient)
		assert.Equal(t, 10000*time.Millisecond, rp.MaximumInterval)
		assert.Equal(t, int32(5), rp.MaximumAttempts)
	})

	t.Run("node policy overrides default policy", func(t *testing.T) {
		defAttempts := 5
		defPol := &engine.RetryPolicy{
			MaxAttempts: &defAttempts,
		}

		nodeInit := 100
		nodeAttempts := 10
		nodePol := &engine.RetryPolicy{
			InitialIntervalMs: &nodeInit,
			MaxAttempts:       &nodeAttempts,
		}

		rp := BuildActivityRetryPolicy(nodePol, defPol)
		assert.Equal(t, 100*time.Millisecond, rp.InitialInterval)
		assert.Equal(t, DefaultRetryBackoffCoefficient, rp.BackoffCoefficient)
		assert.Equal(t, DefaultRetryMaxInterval, rp.MaximumInterval)
		assert.Equal(t, int32(10), rp.MaximumAttempts)
	})
}

func TestResolveNodeRetryPolicy(t *testing.T) {
	initInterval := 1000
	policies := map[string]engine.RetryPolicy{
		"default": {
			InitialIntervalMs: &initInterval,
		},
		"custom": {
			MaxAttempts: func() *int { i := 5; return &i }(),
		},
	}

	t.Run("no node def uses default only", func(t *testing.T) {
		rp := ResolveNodeRetryPolicy(nil, policies)
		assert.Equal(t, 1000*time.Millisecond, rp.InitialInterval)
		assert.Equal(t, int32(DefaultRetryMaxAttempts), rp.MaximumAttempts)
	})

	t.Run("node with no policy uses default only", func(t *testing.T) {
		rp := ResolveNodeRetryPolicy(&engine.NodeDefinition{}, policies)
		assert.Equal(t, 1000*time.Millisecond, rp.InitialInterval)
		assert.Equal(t, int32(DefaultRetryMaxAttempts), rp.MaximumAttempts)
	})

	t.Run("node with custom policy uses both", func(t *testing.T) {
		rp := ResolveNodeRetryPolicy(&engine.NodeDefinition{RetryPolicy: "custom"}, policies)
		assert.Equal(t, 1000*time.Millisecond, rp.InitialInterval)
		assert.Equal(t, int32(5), rp.MaximumAttempts)
	})
}

func TestWrapTemporalError(t *testing.T) {
	assert.Nil(t, WrapTemporalError(nil))

	baseErr := errors.New("base err")

	retryable := engine.NewRetryableError(baseErr)
	wRetryable := WrapTemporalError(retryable)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(wRetryable, &appErr))
	assert.Equal(t, ErrorTypeRetryable, appErr.Type())
	assert.False(t, appErr.NonRetryable())

	nonRetryable := engine.NewNonRetryableError(baseErr)
	wNonRetryable := WrapTemporalError(nonRetryable)
	require.True(t, errors.As(wNonRetryable, &appErr))
	assert.Equal(t, ErrorTypeNonRetryable, appErr.Type())
	assert.True(t, appErr.NonRetryable())

	critical := engine.NewCriticalError(baseErr)
	wCritical := WrapTemporalError(critical)
	require.True(t, errors.As(wCritical, &appErr))
	assert.Equal(t, ErrorTypeCritical, appErr.Type())
	assert.True(t, appErr.NonRetryable())

	unclass := errors.New("unclass")
	wUnclass := WrapTemporalError(unclass)
	require.True(t, errors.As(wUnclass, &appErr))
	assert.Equal(t, ErrorTypeNonRetryable, appErr.Type())
	assert.True(t, appErr.NonRetryable())

	// Already app err
	already := temporal.NewApplicationError("test", "test_type")
	wAlready := WrapTemporalError(already)
	assert.Equal(t, already, wAlready)
}

func TestIsCriticalError(t *testing.T) {
	assert.False(t, IsCriticalError(nil))
	assert.False(t, IsCriticalError(errors.New("base")))
	assert.False(t, IsCriticalError(engine.NewRetryableError(errors.New("base"))))
	assert.True(t, IsCriticalError(engine.NewCriticalError(errors.New("base"))))

	appErrCritical := temporal.NewNonRetryableApplicationError("err", ErrorTypeCritical, nil)
	assert.True(t, IsCriticalError(appErrCritical))

	appErrNon := temporal.NewNonRetryableApplicationError("err", ErrorTypeNonRetryable, nil)
	assert.False(t, IsCriticalError(appErrNon))

	// To test IsCriticalError checking wrapped temporal.ActivityError,
	// We simulate temporal returning an application error. IsCriticalError does errors.As(err, &appErr)
	// which correctly unwraps standard go errors, including ActivityError from the SDK if we could make one.
	// But since NewActivityError is not exported we just use appErr directly.
}

// Mini-workflow for testing retry exhaustion
func RetryExhaustionTestWorkflow(ctx workflow.Context, input NodeActivityInput) error {
	rp := &temporal.RetryPolicy{
		InitialInterval:        10 * time.Millisecond,
		BackoffCoefficient:     2.0,
		MaximumInterval:        50 * time.Millisecond,
		MaximumAttempts:        3,
		NonRetryableErrorTypes: []string{ErrorTypeNonRetryable, ErrorTypeCritical},
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         rp,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var res NodeActivityResult
	// We call ExecuteNodeActivity which acts as the activity
	return workflow.ExecuteActivity(ctx, "ExecuteNodeActivity", input).Get(ctx, &res)
}

type testNode struct {
	errToReturn error
	calls       int
}

func (n *testNode) OnInit(_ context.Context, _ engine.NodeContext) error {
	return nil
}

func (n *testNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	n.calls++
	return nil, n.errToReturn
}

func (n *testNode) OnSuccess(_ context.Context, _ engine.NodeContext, _ *engine.StateMutation) error {
	return nil
}

func (n *testNode) OnFailure(_ context.Context, _ engine.NodeContext, _ error) error {
	return nil
}

func (n *testNode) EvaluateReactivity(_ context.Context, _ engine.NodeContext) (bool, error) {
	return true, nil
}

func TestRetryExhaustionAndHalting(t *testing.T) {
	t.Run("Retry exhaustion -> node Failed", func(t *testing.T) {
		ts := &testsuite.WorkflowTestSuite{}
		env := ts.NewTestWorkflowEnvironment()

		node := &testNode{errToReturn: engine.NewRetryableError(errors.New("retry me"))}

		reg := engine.NewRegistry()
		err := reg.Register("test_type", func(_ any) (engine.NodeInterface, error) {
			return node, nil
		}, nil)
		require.NoError(t, err)
		na := NewNodeActivities(reg)

		env.RegisterWorkflow(RetryExhaustionTestWorkflow)
		env.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{Name: "ExecuteNodeActivity"})

		input := NodeActivityInput{
			ExecutionID: "exec-1",
			NodeID:      "node-1",
			NodeType:    "test_type",
			MaxAttempts: 3,
		}

		env.ExecuteWorkflow(RetryExhaustionTestWorkflow, input)

		require.True(t, env.IsWorkflowCompleted())
		err = env.GetWorkflowError()
		require.Error(t, err)

		// 3 attempts since MaxAttempts = 3
		assert.Equal(t, 3, node.calls)
	})

	t.Run("Non-retryable error honoured immediately", func(t *testing.T) {
		ts := &testsuite.WorkflowTestSuite{}
		env := ts.NewTestWorkflowEnvironment()

		node := &testNode{errToReturn: engine.NewNonRetryableError(errors.New("do not retry me"))}

		reg := engine.NewRegistry()
		err := reg.Register("test_type", func(_ any) (engine.NodeInterface, error) {
			return node, nil
		}, nil)
		require.NoError(t, err)
		na := NewNodeActivities(reg)

		env.RegisterWorkflow(RetryExhaustionTestWorkflow)
		env.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{Name: "ExecuteNodeActivity"})

		input := NodeActivityInput{
			ExecutionID: "exec-1",
			NodeID:      "node-1",
			NodeType:    "test_type",
			MaxAttempts: 3,
		}

		env.ExecuteWorkflow(RetryExhaustionTestWorkflow, input)

		require.True(t, env.IsWorkflowCompleted())
		err = env.GetWorkflowError()
		require.Error(t, err)

		// 1 attempt because it's non-retryable
		assert.Equal(t, 1, node.calls)
		assert.False(t, IsCriticalError(err))
	})

	t.Run("Critical error honoured immediately", func(t *testing.T) {
		ts := &testsuite.WorkflowTestSuite{}
		env := ts.NewTestWorkflowEnvironment()

		node := &testNode{errToReturn: engine.NewCriticalError(errors.New("critical error"))}

		reg := engine.NewRegistry()
		err := reg.Register("test_type", func(_ any) (engine.NodeInterface, error) {
			return node, nil
		}, nil)
		require.NoError(t, err)
		na := NewNodeActivities(reg)

		env.RegisterWorkflow(RetryExhaustionTestWorkflow)
		env.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{Name: "ExecuteNodeActivity"})

		input := NodeActivityInput{
			ExecutionID: "exec-1",
			NodeID:      "node-1",
			NodeType:    "test_type",
			MaxAttempts: 3,
		}

		env.ExecuteWorkflow(RetryExhaustionTestWorkflow, input)

		require.True(t, env.IsWorkflowCompleted())
		err = env.GetWorkflowError()
		require.Error(t, err)

		// 1 attempt because it's critical (which is also non-retryable)
		assert.Equal(t, 1, node.calls)
		assert.True(t, IsCriticalError(err))
	})
}
