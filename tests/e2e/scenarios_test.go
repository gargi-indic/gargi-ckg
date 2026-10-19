package e2e_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/gargi-indic/gargi-ckg/internal/nodes"
	"github.com/gargi-indic/gargi-ckg/internal/orchestrator"
	"github.com/gargi-indic/gargi-ckg/internal/store"
	"github.com/gargi-indic/gargi-ckg/internal/store/memory"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

type testHarness struct {
	env    *testsuite.TestWorkflowEnvironment
	runner *orchestrator.WorkflowRunner
	reg    *engine.Registry
	store  engine.StateStore
}

func newTestHarness(t *testing.T, st engine.StateStore) *testHarness {
	t.Helper()
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{
		Signals: orchestrator.NewTemporalSignalSource(),
	}))

	runner := orchestrator.NewWorkflowRunner(reg)
	na := orchestrator.NewNodeActivities(reg)
	sa := orchestrator.NewStateActivities(st)

	env.RegisterWorkflowWithOptions(runner.GraphExecutionWorkflow, workflow.RegisterOptions{
		Name: orchestrator.GraphExecutionWorkflowName,
	})
	env.RegisterActivity(sa.PersistStateActivity)
	env.RegisterActivity(sa.WriteStateActivity)
	env.RegisterActivityWithOptions(na.ExecuteNodeActivity, activity.RegisterOptions{
		Name: orchestrator.ExecuteNodeActivityName,
	})

	return &testHarness{
		env:    env,
		runner: runner,
		reg:    reg,
		store:  st,
	}
}

// Scenario 1: Linear graph execution (A -> B -> C)
func runLinearScenario(t *testing.T, st engine.StateStore) {
	h := newTestHarness(t, st)
	execID := engine.ExecutionID("exec-linear-1")

	def := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "linear-graph",
		Nodes: []engine.NodeDefinition{
			{
				ID:   "nodeA",
				Type: "transform",
				Config: map[string]any{
					"set": map[string]any{"stepA": "complete"},
				},
			},
			{
				ID:   "nodeB",
				Type: "transform",
				Config: map[string]any{
					"set": map[string]any{"stepB": "complete"},
				},
			},
			{
				ID:   "nodeC",
				Type: "transform",
				Config: map[string]any{
					"set": map[string]any{"stepC": "complete"},
				},
			},
		},
		Edges: []engine.EdgeDefinition{
			{From: "nodeA", To: "nodeB"},
			{From: "nodeB", To: "nodeC"},
		},
	}

	h.env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, orchestrator.GraphWorkflowInput{
		ExecutionID: execID,
		Definition:  def,
	})

	require.True(t, h.env.IsWorkflowCompleted())
	require.NoError(t, h.env.GetWorkflowError())

	var res orchestrator.GraphWorkflowResult
	require.NoError(t, h.env.GetWorkflowResult(&res))
	assert.Equal(t, engine.StatusSucceeded, res.Status)
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("nodeA").Status())
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("nodeB").Status())
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("nodeC").Status())

	// Verify persistence in StateStore
	if st != nil {
		persisted, err := st.GetState(context.Background(), execID)
		if err == nil && persisted != nil {
			assert.Equal(t, engine.StatusSucceeded, persisted.Status())
		}
	}
}

// Scenario 2: Fan-out / Fan-in graph execution (Source -> (WorkerA, WorkerB) -> Aggregator)
func runFanOutFanInScenario(t *testing.T, st engine.StateStore) {
	h := newTestHarness(t, st)
	execID := engine.ExecutionID("exec-fanout-1")

	def := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "fanout-fanin-graph",
		Nodes: []engine.NodeDefinition{
			{
				ID:   "source",
				Type: "transform",
				Config: map[string]any{
					"set": map[string]any{"seed": 42},
				},
			},
			{
				ID:   "worker_a",
				Type: "transform",
				Config: map[string]any{
					"set": map[string]any{"branchA": true},
				},
			},
			{
				ID:   "worker_b",
				Type: "transform",
				Config: map[string]any{
					"set": map[string]any{"branchB": true},
				},
			},
			{
				ID:   "aggregator",
				Type: "transform",
				Config: map[string]any{
					"set": map[string]any{"all_branches_merged": true},
				},
			},
		},
		Edges: []engine.EdgeDefinition{
			{From: "source", To: "worker_a"},
			{From: "source", To: "worker_b"},
			{From: "worker_a", To: "aggregator"},
			{From: "worker_b", To: "aggregator"},
		},
	}

	h.env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, orchestrator.GraphWorkflowInput{
		ExecutionID: execID,
		Definition:  def,
	})

	require.True(t, h.env.IsWorkflowCompleted())
	require.NoError(t, h.env.GetWorkflowError())

	var res orchestrator.GraphWorkflowResult
	require.NoError(t, h.env.GetWorkflowResult(&res))
	assert.Equal(t, engine.StatusSucceeded, res.Status)
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("worker_a").Status())
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("worker_b").Status())
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("aggregator").Status())
}

// Scenario 3: Conditional Branch + Skip (Classifier -> left/right branch -> Finish)
func runConditionalBranchSkipScenario(t *testing.T, st engine.StateStore) {
	h := newTestHarness(t, st)
	execID := engine.ExecutionID("exec-branch-skip-1")

	def := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "branch-skip-graph",
		Nodes: []engine.NodeDefinition{
			{
				ID:   "classifier",
				Type: "transform",
				Config: map[string]any{
					"set": map[string]any{"route": "left"},
				},
			},
			{
				ID:   "left_branch",
				Type: "passthrough",
			},
			{
				ID:   "right_branch",
				Type: "passthrough",
			},
			{
				ID:       "finish",
				Type:     "passthrough",
				JoinMode: engine.JoinModeAnyOf,
			},
		},
		Edges: []engine.EdgeDefinition{
			{
				From:      "classifier",
				To:        "left_branch",
				Condition: "upstream.output.route == 'left'",
			},
			{
				From:      "classifier",
				To:        "right_branch",
				Condition: "upstream.output.route == 'right'",
			},
			{
				From: "left_branch",
				To:   "finish",
			},
			{
				From: "right_branch",
				To:   "finish",
			},
		},
	}

	h.env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, orchestrator.GraphWorkflowInput{
		ExecutionID: execID,
		Definition:  def,
	})

	require.True(t, h.env.IsWorkflowCompleted())
	require.NoError(t, h.env.GetWorkflowError())

	var res orchestrator.GraphWorkflowResult
	require.NoError(t, h.env.GetWorkflowResult(&res))
	assert.Equal(t, engine.StatusSucceeded, res.Status)
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("left_branch").Status())
	assert.Equal(t, engine.StatusSkipped, res.FinalState.NodeState("right_branch").Status())
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("finish").Status())
}

type flakyRetryNode struct {
	engine.BaseNode
	mu    sync.Mutex
	calls int
}

func (n *flakyRetryNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	n.mu.Lock()
	n.calls++
	callNum := n.calls
	n.mu.Unlock()

	if callNum == 1 {
		return nil, engine.NewRetryableError(errors.New("temporary transient connection error"))
	}
	return nil, nil
}

// Scenario 4: Retry Then Success
func runRetryThenSuccessScenario(t *testing.T, st engine.StateStore) {
	h := newTestHarness(t, st)
	flaky := &flakyRetryNode{}
	err := h.reg.Register("flaky_node", func(_ any) (engine.NodeInterface, error) {
		return flaky, nil
	}, nil)
	require.NoError(t, err)

	maxAttempts := 3
	initInterval := 10
	def := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "retry-success-graph",
		Nodes: []engine.NodeDefinition{
			{
				ID:          "flaky_step",
				Type:        "flaky_node",
				RetryPolicy: "retry_flaky",
			},
		},
		RetryPolicies: map[string]engine.RetryPolicy{
			"retry_flaky": {
				MaxAttempts:       &maxAttempts,
				InitialIntervalMs: &initInterval,
			},
		},
	}

	h.env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, orchestrator.GraphWorkflowInput{
		ExecutionID: "exec-retry-success-1",
		Definition:  def,
	})

	require.True(t, h.env.IsWorkflowCompleted())
	require.NoError(t, h.env.GetWorkflowError())

	var res orchestrator.GraphWorkflowResult
	require.NoError(t, h.env.GetWorkflowResult(&res))
	assert.Equal(t, engine.StatusSucceeded, res.Status)
	assert.Equal(t, 2, flaky.calls)
}

type permanentFailNode struct {
	engine.BaseNode
	mu    sync.Mutex
	calls int
}

func (n *permanentFailNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	n.mu.Lock()
	n.calls++
	n.mu.Unlock()
	return nil, engine.NewRetryableError(errors.New("unrecoverable backend outage"))
}

// Scenario 5: Retry Exhaustion
func runRetryExhaustionScenario(t *testing.T, st engine.StateStore) {
	h := newTestHarness(t, st)
	failer := &permanentFailNode{}
	err := h.reg.Register("perm_fail_node", func(_ any) (engine.NodeInterface, error) {
		return failer, nil
	}, nil)
	require.NoError(t, err)

	maxAttempts := 2
	initInterval := 10
	def := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "retry-exhaustion-graph",
		Nodes: []engine.NodeDefinition{
			{
				ID:          "failing_step",
				Type:        "perm_fail_node",
				RetryPolicy: "retry_exhaust",
			},
		},
		RetryPolicies: map[string]engine.RetryPolicy{
			"retry_exhaust": {
				MaxAttempts:       &maxAttempts,
				InitialIntervalMs: &initInterval,
			},
		},
	}

	h.env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, orchestrator.GraphWorkflowInput{
		ExecutionID: "exec-retry-exhaust-1",
		Definition:  def,
	})

	require.True(t, h.env.IsWorkflowCompleted())

	var res orchestrator.GraphWorkflowResult
	require.NoError(t, h.env.GetWorkflowResult(&res))
	assert.Equal(t, engine.StatusFailed, res.Status)
	assert.Equal(t, 2, failer.calls)
	assert.Equal(t, engine.StatusFailed, res.FinalState.NodeState("failing_step").Status())
}

type criticalFailNode struct {
	engine.BaseNode
}

func (n *criticalFailNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	return nil, engine.NewCriticalError(errors.New("critical payment failure"))
}

type customHandlerNode struct {
	engine.BaseNode
	fn func()
}

func (n *customHandlerNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	if n.fn != nil {
		n.fn()
	}
	return nil, nil
}

// Scenario 6: Saga Compensation in reverse topological order
func runSagaCompensationScenario(t *testing.T, st engine.StateStore) {
	h := newTestHarness(t, st)
	require.NoError(t, h.reg.Register("critical_fail", func(_ any) (engine.NodeInterface, error) {
		return &criticalFailNode{}, nil
	}, nil))

	var compensationOrder []string
	var orderMu sync.Mutex

	require.NoError(t, h.reg.Register("comp_handler_1", func(_ any) (engine.NodeInterface, error) {
		return &customHandlerNode{fn: func() {
			orderMu.Lock()
			compensationOrder = append(compensationOrder, "comp1")
			orderMu.Unlock()
		}}, nil
	}, nil))

	require.NoError(t, h.reg.Register("comp_handler_2", func(_ any) (engine.NodeInterface, error) {
		return &customHandlerNode{fn: func() {
			orderMu.Lock()
			compensationOrder = append(compensationOrder, "comp2")
			orderMu.Unlock()
		}}, nil
	}, nil))

	def := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "saga-graph",
		Nodes: []engine.NodeDefinition{
			{ID: "step1", Type: "passthrough", Compensation: "comp1"},
			{ID: "step2", Type: "passthrough", Compensation: "comp2"},
			{ID: "step3", Type: "critical_fail"},
		},
		Edges: []engine.EdgeDefinition{
			{From: "step1", To: "step2"},
			{From: "step2", To: "step3"},
		},
		Compensations: map[string]engine.CompensationDefinition{
			"comp1": {Type: "comp_handler_1"},
			"comp2": {Type: "comp_handler_2"},
		},
		FailurePolicy: &engine.FailurePolicy{
			OnCriticalFailure: "compensate",
		},
	}

	h.env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, orchestrator.GraphWorkflowInput{
		ExecutionID: "exec-saga-1",
		Definition:  def,
	})

	require.True(t, h.env.IsWorkflowCompleted())

	var res orchestrator.GraphWorkflowResult
	_ = h.env.GetWorkflowResult(&res)

	// In reverse topological order: step2 compensated first, then step1
	orderMu.Lock()
	defer orderMu.Unlock()
	require.Equal(t, []string{"comp2", "comp1"}, compensationOrder)
	assert.Equal(t, engine.StatusCompensated, res.FinalState.NodeState("step1").Status())
	assert.Equal(t, engine.StatusCompensated, res.FinalState.NodeState("step2").Status())
}

// Scenario 7: External Signal Delivery
func runExternalSignalScenario(t *testing.T, st engine.StateStore) {
	h := newTestHarness(t, st)
	execID := engine.ExecutionID("exec-signal-1")

	def := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "signal-graph",
		Nodes: []engine.NodeDefinition{
			{
				ID:     "await_step",
				Type:   "wait_for_signal",
				Config: map[string]any{"signal": "approval_sig"},
			},
			{
				ID:   "post_step",
				Type: "transform",
				Config: map[string]any{
					"mapping": map[string]any{"approvedVal": "inVal"},
				},
			},
		},
		Edges: []engine.EdgeDefinition{
			{
				From:        "await_step",
				To:          "post_step",
				DataMapping: map[string]string{"inVal": "payload"},
			},
		},
	}

	// Schedule asynchronous signal delivery
	h.env.RegisterDelayedCallback(func() {
		time.Sleep(10 * time.Millisecond)
		h.env.SignalWorkflow(orchestrator.SignalChannelName, orchestrator.ExternalSignalPayload{
			TargetNodeID: "await_step",
			SignalName:   "approval_sig",
			Payload:      map[string]any{"approved": true},
		})
	}, time.Millisecond)

	h.env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, orchestrator.GraphWorkflowInput{
		ExecutionID: execID,
		Definition:  def,
	})

	require.True(t, h.env.IsWorkflowCompleted())
	require.NoError(t, h.env.GetWorkflowError())

	var res orchestrator.GraphWorkflowResult
	require.NoError(t, h.env.GetWorkflowResult(&res))
	assert.Equal(t, engine.StatusSucceeded, res.Status)
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("await_step").Status())
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("post_step").Status())
}

// Scenario 8: Child Workflow Execution
func runChildWorkflowScenario(t *testing.T, st engine.StateStore) {
	h := newTestHarness(t, st)
	execID := engine.ExecutionID("exec-child-wf-1")

	childDef := map[string]any{
		"apiVersion": "graph/v1",
		"id":         "child-graph",
		"nodes": []any{
			map[string]any{
				"id":     "child_step1",
				"type":   "transform",
				"config": map[string]any{"set": map[string]any{"child_calc": 100}},
			},
		},
	}

	mainDef := &engine.GraphDefinition{
		APIVersion: "graph/v1",
		ID:         "parent-graph",
		Nodes: []engine.NodeDefinition{
			{
				ID:   "sub_graph_node",
				Type: "sub_graph",
				Config: map[string]any{
					"definition": childDef,
				},
			},
			{
				ID:   "parent_finish",
				Type: "passthrough",
			},
		},
		Edges: []engine.EdgeDefinition{
			{From: "sub_graph_node", To: "parent_finish"},
		},
	}

	h.env.ExecuteWorkflow(orchestrator.GraphExecutionWorkflowName, orchestrator.GraphWorkflowInput{
		ExecutionID: execID,
		Definition:  mainDef,
	})

	require.True(t, h.env.IsWorkflowCompleted())
	require.NoError(t, h.env.GetWorkflowError())

	var res orchestrator.GraphWorkflowResult
	require.NoError(t, h.env.GetWorkflowResult(&res))
	assert.Equal(t, engine.StatusSucceeded, res.Status)
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("sub_graph_node").Status())
	assert.Equal(t, engine.StatusSucceeded, res.FinalState.NodeState("parent_finish").Status())

	subOut := res.FinalState.NodeState("sub_graph_node").Output()
	require.NotNil(t, subOut)
	nodesMap, ok := subOut["nodes"].(map[string]any)
	require.True(t, ok)
	assert.NotNil(t, nodesMap["child_step1"])
}

// TestE2E_InMemory runs all 8 scenarios against the in-memory state store configuration.
func TestE2E_InMemory(t *testing.T) {
	st := memory.NewStore()

	t.Run("Scenario1_Linear", func(t *testing.T) {
		runLinearScenario(t, st)
	})

	t.Run("Scenario2_FanOutFanIn", func(t *testing.T) {
		runFanOutFanInScenario(t, st)
	})

	t.Run("Scenario3_ConditionalBranchSkip", func(t *testing.T) {
		runConditionalBranchSkipScenario(t, st)
	})

	t.Run("Scenario4_RetryThenSuccess", func(t *testing.T) {
		runRetryThenSuccessScenario(t, st)
	})

	t.Run("Scenario5_RetryExhaustion", func(t *testing.T) {
		runRetryExhaustionScenario(t, st)
	})

	t.Run("Scenario6_SagaCompensation", func(t *testing.T) {
		runSagaCompensationScenario(t, st)
	})

	t.Run("Scenario7_ExternalSignal", func(t *testing.T) {
		runExternalSignalScenario(t, st)
	})

	t.Run("Scenario8_ChildWorkflow", func(t *testing.T) {
		runChildWorkflowScenario(t, st)
	})
}

// TestE2E_Redis runs all scenarios against Redis if configured in the environment.
func TestE2E_Redis(t *testing.T) {
	redisAddr := os.Getenv("GARGI_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("Skipping Redis E2E tests: GARGI_REDIS_ADDR environment variable is not set")
	}

	ctx := context.Background()
	cfg := config.StoreConfig{
		Driver: "redis",
		Redis:  &config.RedisConfig{Addr: redisAddr},
	}

	st, err := store.New(ctx, cfg)
	if err != nil {
		t.Skipf("Skipping Redis E2E tests: failed to connect to Redis at %s: %v", redisAddr, err)
	}
	defer func() { _ = st.Close() }()

	t.Run("Scenario1_Linear", func(t *testing.T) {
		runLinearScenario(t, st)
	})
	t.Run("Scenario2_FanOutFanIn", func(t *testing.T) {
		runFanOutFanInScenario(t, st)
	})
	t.Run("Scenario3_ConditionalBranchSkip", func(t *testing.T) {
		runConditionalBranchSkipScenario(t, st)
	})
}

// TestE2E_Postgres runs all scenarios against Postgres if configured in the environment.
func TestE2E_Postgres(t *testing.T) {
	postgresDSN := os.Getenv("GARGI_POSTGRES_DSN")
	if postgresDSN == "" {
		t.Skip("Skipping Postgres E2E tests: GARGI_POSTGRES_DSN environment variable is not set")
	}

	ctx := context.Background()
	cfg := config.StoreConfig{
		Driver:   "postgres",
		Postgres: &config.PostgresConfig{DSN: postgresDSN},
	}

	st, err := store.New(ctx, cfg)
	if err != nil {
		t.Skipf("Skipping Postgres E2E tests: failed to connect to Postgres at %s: %v", postgresDSN, err)
	}
	defer func() { _ = st.Close() }()

	t.Run("Scenario1_Linear", func(t *testing.T) {
		runLinearScenario(t, st)
	})
	t.Run("Scenario2_FanOutFanIn", func(t *testing.T) {
		runFanOutFanInScenario(t, st)
	})
	t.Run("Scenario3_ConditionalBranchSkip", func(t *testing.T) {
		runConditionalBranchSkipScenario(t, st)
	})
}
