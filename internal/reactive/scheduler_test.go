package reactive_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/reactive"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

// mockNode implements engine.NodeInterface for testing.
type mockNode struct {
	ready bool
	err   error
}

func (m *mockNode) OnInit(_ context.Context, _ engine.NodeContext) error { return nil }
func (m *mockNode) EvaluateReactivity(_ context.Context, _ engine.NodeContext) (bool, error) {
	return m.ready, m.err
}

func (m *mockNode) Execute(_ context.Context, _ engine.NodeContext) (*engine.StateMutation, error) {
	return nil, nil
}

func (m *mockNode) OnSuccess(_ context.Context, _ engine.NodeContext, _ *engine.StateMutation) error {
	return nil
}

func (m *mockNode) OnFailure(_ context.Context, _ engine.NodeContext, _ error) error {
	return nil
}

func TestScheduler_EvaluateStep_RootNodes(t *testing.T) {
	reg := engine.NewRegistry()
	_ = reg.Register("mock", func(_ any) (engine.NodeInterface, error) {
		return &mockNode{ready: true}, nil
	}, nil)

	nB, _ := engine.NewNode("B", "mock", nil)
	nA, _ := engine.NewNode("A", "mock", nil)
	nC, _ := engine.NewNode("C", "mock", nil)

	// No edges, all roots
	g, _ := engine.NewGraph("g1", []*engine.Node{nB, nA, nC}, []*engine.Edge{})

	sched := reactive.NewScheduler(g, reg, nil, nil)
	execID, _ := engine.NewExecutionID()
	state, _ := engine.NewGraphState(execID, g)

	ctx := context.Background()
	plan, newState, err := sched.EvaluateStep(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.ReadyNodes) != 3 {
		t.Fatalf("expected 3 ready nodes, got %d", len(plan.ReadyNodes))
	}
	// Expected deterministic lexicographical order
	expected := []engine.NodeID{"A", "B", "C"}
	for i, id := range expected {
		if plan.ReadyNodes[i] != id {
			t.Errorf("expected node at index %d to be %s, got %s", i, id, plan.ReadyNodes[i])
		}
	}
	if len(plan.SkippedNodes) != 0 {
		t.Errorf("expected 0 skipped nodes, got %d", len(plan.SkippedNodes))
	}

	for _, id := range expected {
		if newState.NodeState(id).Status() != engine.StatusReady {
			t.Errorf("expected node %s to be Ready, got %s", id, newState.NodeState(id).Status())
		}
	}
}

func TestScheduler_EvaluateStep_CascadingSkip(t *testing.T) {
	reg := engine.NewRegistry()
	_ = reg.Register("mock", func(_ any) (engine.NodeInterface, error) {
		return &mockNode{ready: true}, nil
	}, nil)

	nA, _ := engine.NewNode("A", "mock", nil)
	nB, _ := engine.NewNode("B", "mock", nil)
	nC, _ := engine.NewNode("C", "mock", nil)

	eAB, _ := engine.NewEdge("A", "B", "upstream.output.val == true", nil)
	eBC, _ := engine.NewEdge("B", "C", "", nil)

	g, _ := engine.NewGraph("g1", []*engine.Node{nA, nB, nC}, []*engine.Edge{eAB, eBC})

	sched := reactive.NewScheduler(g, reg, nil, nil)
	execID, _ := engine.NewExecutionID()
	state, _ := engine.NewGraphState(execID, g)

	// Transition A to Succeeded with val=false
	state, _ = state.Apply(engine.StateMutation{NodeID: "A", ExpectedVersion: 0, Status: schedulerStatusPtr(engine.StatusReady)})
	state, _ = state.Apply(engine.StateMutation{NodeID: "A", ExpectedVersion: 1, Status: schedulerStatusPtr(engine.StatusRunning)})
	state, _ = state.Apply(engine.StateMutation{NodeID: "A", ExpectedVersion: 2, Status: schedulerStatusPtr(engine.StatusSucceeded), Set: map[string]any{"val": false}})

	ctx := context.Background()
	plan, newState, err := sched.EvaluateStep(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.ReadyNodes) != 0 {
		t.Fatalf("expected 0 ready nodes, got %d", len(plan.ReadyNodes))
	}
	if len(plan.SkippedNodes) != 2 {
		t.Fatalf("expected 2 skipped nodes, got %d", len(plan.SkippedNodes))
	}
	expectedSkip := []engine.NodeID{"B", "C"}
	for i, id := range expectedSkip {
		if plan.SkippedNodes[i] != id {
			t.Errorf("expected skipped node at %d to be %s, got %s", i, id, plan.SkippedNodes[i])
		}
		if newState.NodeState(id).Status() != engine.StatusSkipped {
			t.Errorf("expected node %s to be Skipped, got %s", id, newState.NodeState(id).Status())
		}
	}
}

func TestScheduler_EvaluateStep_EvaluateReactivity_Error(t *testing.T) {
	reg := engine.NewRegistry()
	_ = reg.Register("mock_err", func(_ any) (engine.NodeInterface, error) {
		return &mockNode{ready: true, err: errors.New("eval error")}, nil
	}, nil)

	nA, _ := engine.NewNode("A", "mock_err", nil)
	g, _ := engine.NewGraph("g1", []*engine.Node{nA}, []*engine.Edge{})

	sched := reactive.NewScheduler(g, reg, nil, nil)
	execID, _ := engine.NewExecutionID()
	state, _ := engine.NewGraphState(execID, g)

	ctx := context.Background()
	plan, retState, err := sched.EvaluateStep(ctx, state)
	if err == nil || err.Error() != "eval error" {
		t.Fatalf("expected 'eval error', got: %v", err)
	}
	if plan != nil {
		t.Errorf("expected plan to be nil on error")
	}
	if retState != state {
		t.Errorf("expected original state to be returned on error")
	}
}

func TestScheduler_EvaluateStep_EvaluateReactivity_False(t *testing.T) {
	reg := engine.NewRegistry()
	_ = reg.Register("mock_false", func(_ any) (engine.NodeInterface, error) {
		return &mockNode{ready: false}, nil
	}, nil)

	nA, _ := engine.NewNode("A", "mock_false", nil)
	g, _ := engine.NewGraph("g1", []*engine.Node{nA}, []*engine.Edge{})

	sched := reactive.NewScheduler(g, reg, nil, nil)
	execID, _ := engine.NewExecutionID()
	state, _ := engine.NewGraphState(execID, g)

	ctx := context.Background()
	plan, newState, err := sched.EvaluateStep(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.ReadyNodes) != 0 {
		t.Errorf("expected 0 ready nodes, got %v", plan.ReadyNodes)
	}
	if newState.NodeState("A").Status() != engine.StatusPending {
		t.Errorf("expected node A to remain Pending, got %s", newState.NodeState("A").Status())
	}
}

func TestScheduler_DispatchReady_Parallel(t *testing.T) {
	reg := engine.NewRegistry()
	_ = reg.Register("mock", func(_ any) (engine.NodeInterface, error) { return &mockNode{ready: true}, nil }, nil)
	nA, _ := engine.NewNode("A", "mock", nil)
	nB, _ := engine.NewNode("B", "mock", nil)
	g, _ := engine.NewGraph("g1", []*engine.Node{nA, nB}, []*engine.Edge{})

	sched := reactive.NewScheduler(g, reg, nil, nil)
	execID, _ := engine.NewExecutionID()
	state, _ := engine.NewGraphState(execID, g)
	ctx := context.Background()

	plan, _, err := sched.EvaluateStep(ctx, state)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	dispatched := make([]engine.NodeID, 0)

	dispatcher := reactive.BatchDispatchFunc(func(_ context.Context, batch []reactive.DispatchItem) error {
		for _, item := range batch {
			wg.Add(1)
			go func(it reactive.DispatchItem) {
				defer wg.Done()
				// Simulate parallel work
				mu.Lock()
				dispatched = append(dispatched, it.NodeID)
				mu.Unlock()
			}(item)
		}
		wg.Wait()
		return nil
	})

	err = sched.DispatchReady(ctx, plan, dispatcher)
	if err != nil {
		t.Fatalf("unexpected dispatch err: %v", err)
	}

	if len(dispatched) != 2 {
		t.Errorf("expected 2 dispatched items, got %d", len(dispatched))
	}
}

func schedulerStatusPtr(s engine.Status) *engine.Status {
	return &s
}
