package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

// StoreFactory creates a fresh, isolated engine.StateStore instance for a test case.
type StoreFactory func(t *testing.T) engine.StateStore

// AdapterFactory is an alias for StoreFactory for semantic flexibility.
type AdapterFactory = StoreFactory

// Config specifies the configuration options for the conformance test suite runner.
type Config struct {
	// NewStore creates a fresh, isolated engine.StateStore instance for each test.
	// Required.
	NewStore StoreFactory

	// Teardown is called after each test finishes, passing the store created by NewStore (optional).
	Teardown func(t *testing.T, store engine.StateStore)

	// NewLockManager optionally creates an engine.LockManager instance for testing.
	// If nil, the suite checks if the store returned by NewStore implements engine.LockManager.
	NewLockManager func(t *testing.T) engine.LockManager

	// NewEventPublisher optionally creates an engine.EventPublisher instance for testing.
	// If nil, the suite checks if the store returned by NewStore implements engine.EventPublisher.
	NewEventPublisher func(t *testing.T) engine.EventPublisher

	// NewExecutionLog optionally creates an engine.ExecutionLog instance for testing.
	// If nil, the suite checks if the store returned by NewStore implements engine.ExecutionLog.
	NewExecutionLog func(t *testing.T) engine.ExecutionLog
}

// Run executes the complete state store conformance test suite.
// target may be a Config, *Config, StoreFactory, or func(t *testing.T) engine.StateStore.
func Run(t *testing.T, target any) {
	t.Helper()
	var cfg Config
	switch v := target.(type) {
	case Config:
		cfg = v
	case *Config:
		if v == nil {
			t.Fatal("storetest.Run: Config cannot be nil")
			return
		}
		cfg = *v
	case StoreFactory:
		cfg = Config{NewStore: v}
	case func(t *testing.T) engine.StateStore:
		cfg = Config{NewStore: v}
	default:
		t.Fatalf("storetest.Run: unsupported target type %T; expected Config, StoreFactory, or func(t *testing.T) engine.StateStore", target)
		return
	}
	RunSuite(t, cfg)
}

// RunFactory runs the conformance test suite using a StoreFactory.
func RunFactory(t *testing.T, factory StoreFactory) {
	t.Helper()
	RunSuite(t, Config{NewStore: factory})
}

// RunSuite runs the conformance test suite using an explicit Config.
func RunSuite(t *testing.T, cfg Config) {
	t.Helper()
	if cfg.NewStore == nil {
		t.Fatal("storetest: Config.NewStore factory cannot be nil")
		return
	}

	t.Run("CRUD & Lifecycle", func(t *testing.T) {
		RunCRUD(t, cfg)
	})

	t.Run("CAS Conflicts", func(t *testing.T) {
		RunCAS(t, cfg)
	})

	t.Run("Concurrency & Parallel CAS", func(t *testing.T) {
		RunConcurrency(t, cfg)
	})

	t.Run("Capabilities", func(t *testing.T) {
		RunCapabilities(t, cfg)
	})
}

func createStore(t *testing.T, cfg Config) engine.StateStore {
	t.Helper()
	store := cfg.NewStore(t)
	if store == nil {
		t.Fatal("storetest: NewStore returned nil")
	}
	if cfg.Teardown != nil {
		t.Cleanup(func() {
			cfg.Teardown(t, store)
		})
	}
	return store
}

func createTestGraph(t *testing.T) *engine.Graph {
	t.Helper()
	n1, err := engine.NewNode("node1", "test_worker", nil)
	if err != nil {
		t.Fatalf("failed to create node1: %v", err)
	}
	n2, err := engine.NewNode("node2", "test_worker", nil)
	if err != nil {
		t.Fatalf("failed to create node2: %v", err)
	}
	e1, err := engine.NewEdge("node1", "node2", "", nil)
	if err != nil {
		t.Fatalf("failed to create edge: %v", err)
	}
	g, err := engine.NewGraph("conformance-graph", []*engine.Node{n1, n2}, []*engine.Edge{e1})
	if err != nil {
		t.Fatalf("failed to create test graph: %v", err)
	}
	return g
}

func createTestState(t *testing.T, execID engine.ExecutionID, g *engine.Graph) *engine.GraphState {
	t.Helper()
	state, err := engine.NewGraphState(execID, g)
	if err != nil {
		t.Fatalf("failed to create graph state: %v", err)
	}
	return state
}

// RunCRUD runs all CRUD and Lifecycle conformance tests.
func RunCRUD(t *testing.T, cfg Config) {
	t.Run("GetState_NotFound", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}

		state, err := store.GetState(context.Background(), execID)
		if err == nil {
			t.Fatalf("expected error for non-existent execution ID, got state: %v", state)
		}
		if !engine.IsStoreNotFound(err) {
			t.Errorf("expected engine.IsStoreNotFound(err) to be true, got %v", err)
		}
		if !errors.Is(err, engine.ErrStoreNotFound) {
			t.Errorf("expected errors.Is(err, engine.ErrStoreNotFound) to be true, got %v", err)
		}

		var notFoundErr *engine.StoreNotFoundError
		if !errors.As(err, &notFoundErr) {
			t.Errorf("expected errors.As to match *engine.StoreNotFoundError, got %T (%v)", err, err)
		} else {
			if notFoundErr.ExecutionID != execID {
				t.Errorf("expected ExecutionID %s in StoreNotFoundError, got %s", execID, notFoundErr.ExecutionID)
			}
			if notFoundErr.Error() == "" {
				t.Errorf("expected non-empty error string from StoreNotFoundError")
			}
		}
	})

	t.Run("SetState_InitialAndGet", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)
		ctx := context.Background()

		err = store.SetState(ctx, execID, initState, 0)
		if err != nil {
			t.Fatalf("failed to set initial state: %v", err)
		}

		fetched, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state after initial SetState: %v", err)
		}
		if fetched.ExecutionID() != execID {
			t.Errorf("expected ExecutionID %s, got %s", execID, fetched.ExecutionID())
		}
		if fetched.GraphID() != graph.ID() {
			t.Errorf("expected GraphID %s, got %s", graph.ID(), fetched.GraphID())
		}
		if fetched.Version() != 0 {
			t.Errorf("expected version 0, got %d", fetched.Version())
		}
		if fetched.Status() != engine.StatusPending {
			t.Errorf("expected status %s, got %s", engine.StatusPending, fetched.Status())
		}

		for _, n := range graph.Nodes() {
			ns := fetched.NodeState(n.ID())
			if ns == nil {
				t.Fatalf("expected node state for node %s, got nil", n.ID())
			}
			if ns.NodeID() != n.ID() {
				t.Errorf("expected node ID %s, got %s", n.ID(), ns.NodeID())
			}
			if ns.Status() != engine.StatusPending {
				t.Errorf("node %s: expected status %s, got %s", n.ID(), engine.StatusPending, ns.Status())
			}
			if ns.Version() != 0 {
				t.Errorf("node %s: expected version 0, got %d", n.ID(), ns.Version())
			}
			if len(ns.Output()) != 0 {
				t.Errorf("node %s: expected empty output, got %v", n.ID(), ns.Output())
			}
		}
	})

	t.Run("SetState_OverwriteWithExpectedVersion", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)
		ctx := context.Background()

		if err := store.SetState(ctx, execID, initState, 0); err != nil {
			t.Fatalf("failed to set initial state: %v", err)
		}

		// Advance state to version 1 (Pending -> Ready)
		s1, err := initState.WithStatus(0, engine.StatusReady)
		if err != nil {
			t.Fatalf("failed to transition status to Ready: %v", err)
		}
		if s1.Version() != 1 {
			t.Fatalf("expected s1 version 1, got %d", s1.Version())
		}

		err = store.SetState(ctx, execID, s1, 0)
		if err != nil {
			t.Fatalf("failed to update state with expectedVersion 0: %v", err)
		}

		fetched1, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state at version 1: %v", err)
		}
		if fetched1.Version() != 1 {
			t.Errorf("expected version 1, got %d", fetched1.Version())
		}
		if fetched1.Status() != engine.StatusReady {
			t.Errorf("expected status Ready, got %s", fetched1.Status())
		}

		// Advance state to version 2 (Ready -> Running)
		s2, err := fetched1.WithStatus(1, engine.StatusRunning)
		if err != nil {
			t.Fatalf("failed to transition status to Running: %v", err)
		}
		if s2.Version() != 2 {
			t.Fatalf("expected s2 version 2, got %d", s2.Version())
		}

		err = store.SetState(ctx, execID, s2, 1)
		if err != nil {
			t.Fatalf("failed to update state with expectedVersion 1: %v", err)
		}

		fetched2, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state at version 2: %v", err)
		}
		if fetched2.Version() != 2 {
			t.Errorf("expected version 2, got %d", fetched2.Version())
		}
		if fetched2.Status() != engine.StatusRunning {
			t.Errorf("expected status Running, got %s", fetched2.Status())
		}
	})

	t.Run("DeepImmutability", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)
		ctx := context.Background()

		readyStatus := engine.StatusReady
		initData := map[string]any{
			"key1": "original_val",
			"nested": map[string]any{
				"inner": "inner_val",
				"count": 42,
			},
		}
		mutation := engine.NewStateMutation("node1", 0, &readyStatus, initData, nil, nil)
		stateWithData, err := initState.Apply(mutation)
		if err != nil {
			t.Fatalf("failed to apply mutation: %v", err)
		}

		// Mutate original initData map locally before SetState to verify input isolation
		initData["key1"] = "mutated_locally_before_save"
		if err := store.SetState(ctx, execID, stateWithData, 0); err != nil {
			t.Fatalf("failed to set state with data: %v", err)
		}

		// 1. Fetch read1 and verify clean data
		read1, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state: %v", err)
		}
		out1 := read1.NodeState("node1").Output()
		if out1["key1"] != "original_val" {
			t.Errorf("expected 'original_val', got %v", out1["key1"])
		}

		// 2. Corrupt read1's returned map
		out1["key1"] = "corrupted_val"
		if nested, ok := out1["nested"].(map[string]any); ok {
			nested["inner"] = "tampered_inner"
			nested["count"] = 9999
		}
		out1["injected"] = "malicious_payload"

		// 3. Fetch read2 and verify subsequent GetState was completely uncorrupted
		read2, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state second time: %v", err)
		}
		out2 := read2.NodeState("node1").Output()
		if out2["key1"] != "original_val" {
			t.Errorf("store state was corrupted by caller mutating previous read! got %v", out2["key1"])
		}
		nested2, ok := out2["nested"].(map[string]any)
		if !ok {
			t.Fatalf("expected nested map in node output")
		}
		if nested2["inner"] != "inner_val" {
			t.Errorf("nested value was corrupted! got %v", nested2["inner"])
		}
		if _, exists := out2["injected"]; exists {
			t.Errorf("injected key leaked into store state!")
		}
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)

		ctxCanceled, cancel := context.WithCancel(context.Background())
		cancel()

		// GetState with cancelled context
		_, err = store.GetState(ctxCanceled, execID)
		if err == nil {
			t.Errorf("expected error calling GetState with cancelled context, got nil")
		} else if !errors.Is(err, context.Canceled) {
			t.Logf("GetState returned non-context.Canceled error on canceled ctx: %v", err)
		}

		// SetState with cancelled context
		err = store.SetState(ctxCanceled, execID, initState, 0)
		if err == nil {
			t.Errorf("expected error calling SetState with cancelled context, got nil")
		}

		// PatchState with cancelled context
		readyStatus := engine.StatusReady
		mutation := engine.NewStateMutation("node1", 0, &readyStatus, nil, nil, nil)
		_, err = store.PatchState(ctxCanceled, execID, &mutation, 0)
		if err == nil {
			t.Errorf("expected error calling PatchState with cancelled context, got nil")
		}
	})
}

// RunCAS runs all Optimistic Concurrency Control (CAS) and conflict tests.
func RunCAS(t *testing.T, cfg Config) {
	t.Run("SetState_InitialVersionMismatch", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)
		ctx := context.Background()

		// Calling SetState for non-existent execution with expectedVersion != 0 must fail
		err = store.SetState(ctx, execID, initState, 1)
		if err == nil {
			t.Fatalf("expected conflict error for initial SetState with expectedVersion 1, got nil")
		}
		if !engine.IsStoreConflict(err) {
			t.Errorf("expected engine.IsStoreConflict(err) to be true, got %v", err)
		}
		if !errors.Is(err, engine.ErrStoreConflict) {
			t.Errorf("expected errors.Is(err, engine.ErrStoreConflict) to be true, got %v", err)
		}

		var confErr *engine.StoreConflictError
		if !errors.As(err, &confErr) {
			t.Errorf("expected errors.As to match *engine.StoreConflictError, got %T (%v)", err, err)
		} else {
			if confErr.ExpectedVersion != 1 {
				t.Errorf("expected ExpectedVersion 1, got %d", confErr.ExpectedVersion)
			}
			if confErr.ActualVersion != 0 {
				t.Errorf("expected ActualVersion 0, got %d", confErr.ActualVersion)
			}
			if confErr.ExecutionID != execID {
				t.Errorf("expected ExecutionID %s, got %s", execID, confErr.ExecutionID)
			}
			if confErr.Error() == "" {
				t.Errorf("expected non-empty error message from StoreConflictError")
			}
		}

		// Ensure nothing was saved
		_, err = store.GetState(ctx, execID)
		if !engine.IsStoreNotFound(err) {
			t.Errorf("expected store not found after rejected initial SetState, got %v", err)
		}
	})

	t.Run("SetState_UpdateVersionMismatch", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)
		ctx := context.Background()

		if err := store.SetState(ctx, execID, initState, 0); err != nil {
			t.Fatalf("failed to set initial state: %v", err)
		}

		s1, err := initState.WithStatus(0, engine.StatusReady)
		if err != nil {
			t.Fatalf("failed to advance state: %v", err)
		}

		// Attempt SetState with stale/wrong expectedVersion 5
		err = store.SetState(ctx, execID, s1, 5)
		if err == nil {
			t.Fatalf("expected conflict error when expectedVersion 5 mismatches stored version 0, got nil")
		}
		if !engine.IsStoreConflict(err) {
			t.Errorf("expected engine.IsStoreConflict(err) to be true, got %v", err)
		}

		var confErr *engine.StoreConflictError
		if errors.As(err, &confErr) {
			if confErr.ExpectedVersion != 5 {
				t.Errorf("expected ExpectedVersion 5, got %d", confErr.ExpectedVersion)
			}
			if confErr.ActualVersion != 0 {
				t.Errorf("expected ActualVersion 0, got %d", confErr.ActualVersion)
			}
		} else {
			t.Errorf("expected *engine.StoreConflictError, got %T", err)
		}

		// Stored state must remain at version 0
		current, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state: %v", err)
		}
		if current.Version() != 0 {
			t.Errorf("expected stored version to remain 0, got %d", current.Version())
		}
	})

	t.Run("PatchState_NotFound", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		ctx := context.Background()

		readyStatus := engine.StatusReady
		mutation := engine.NewStateMutation("node1", 0, &readyStatus, nil, nil, nil)

		_, err = store.PatchState(ctx, execID, &mutation, 0)
		if err == nil {
			t.Fatalf("expected error patching non-existent execution, got nil")
		}
		if !engine.IsStoreNotFound(err) {
			t.Errorf("expected engine.IsStoreNotFound(err) to be true, got %v", err)
		}
		if !errors.Is(err, engine.ErrStoreNotFound) {
			t.Errorf("expected errors.Is(err, engine.ErrStoreNotFound) to be true, got %v", err)
		}

		var notFoundErr *engine.StoreNotFoundError
		if errors.As(err, &notFoundErr) {
			if notFoundErr.ExecutionID != execID {
				t.Errorf("expected ExecutionID %s, got %s", execID, notFoundErr.ExecutionID)
			}
		} else {
			t.Errorf("expected *engine.StoreNotFoundError, got %T", err)
		}
	})

	t.Run("PatchState_VersionMismatch", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)
		ctx := context.Background()

		if err := store.SetState(ctx, execID, initState, 0); err != nil {
			t.Fatalf("failed to set initial state: %v", err)
		}

		readyStatus := engine.StatusReady
		mutation := engine.NewStateMutation("node1", 0, &readyStatus, nil, nil, nil)

		_, err = store.PatchState(ctx, execID, &mutation, 99)
		if err == nil {
			t.Fatalf("expected conflict error when PatchState expectedVersion mismatches, got nil")
		}
		if !engine.IsStoreConflict(err) {
			t.Errorf("expected engine.IsStoreConflict(err) to be true, got %v", err)
		}

		var confErr *engine.StoreConflictError
		if errors.As(err, &confErr) {
			if confErr.ExpectedVersion != 99 {
				t.Errorf("expected ExpectedVersion 99, got %d", confErr.ExpectedVersion)
			}
			if confErr.ActualVersion != 0 {
				t.Errorf("expected ActualVersion 0, got %d", confErr.ActualVersion)
			}
		} else {
			t.Errorf("expected *engine.StoreConflictError, got %T", err)
		}

		// Verify stored state is still at version 0
		st, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state: %v", err)
		}
		if st.Version() != 0 {
			t.Errorf("expected stored state to remain version 0, got %d", st.Version())
		}
	})

	t.Run("PatchState_ValidMutationAndVersionIncrement", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)
		ctx := context.Background()

		if err := store.SetState(ctx, execID, initState, 0); err != nil {
			t.Fatalf("failed to set initial state: %v", err)
		}

		// 1. Patch from version 0 -> 1: Set StatusReady, output keyA and num
		readyStatus := engine.StatusReady
		m1 := engine.NewStateMutation("node1", 0, &readyStatus, map[string]any{"keyA": "valA", "num": 100}, nil, nil)

		patched1, err := store.PatchState(ctx, execID, &m1, 0)
		if err != nil {
			t.Fatalf("failed PatchState 1: %v", err)
		}
		if patched1.Version() != 1 {
			t.Errorf("expected patched1 version 1, got %d", patched1.Version())
		}
		n1State := patched1.NodeState("node1")
		if n1State == nil {
			t.Fatalf("expected node1 state, got nil")
		}
		if n1State.Status() != engine.StatusReady {
			t.Errorf("expected StatusReady, got %s", n1State.Status())
		}
		if n1State.Version() != 1 {
			t.Errorf("expected node1 version 1, got %d", n1State.Version())
		}
		if n1State.Output()["keyA"] != "valA" {
			t.Errorf("expected keyA valA, got %v", n1State.Output()["keyA"])
		}
		numVal := n1State.Output()["num"]
		switch v := numVal.(type) {
		case int:
			if v != 100 {
				t.Errorf("expected num 100, got %d", v)
			}
		case float64:
			if v != 100.0 {
				t.Errorf("expected num 100, got %f", v)
			}
		default:
			t.Errorf("expected num 100, got %v (%T)", v, v)
		}

		// Verify GetState matches
		fetched1, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state: %v", err)
		}
		if fetched1.Version() != 1 {
			t.Errorf("expected stored version 1, got %d", fetched1.Version())
		}

		// 2. Patch from version 1 -> 2: Set StatusRunning, set keyB
		runningStatus := engine.StatusRunning
		m2 := engine.NewStateMutation("node1", 1, &runningStatus, map[string]any{"keyB": "valB"}, nil, nil)

		patched2, err := store.PatchState(ctx, execID, &m2, 1)
		if err != nil {
			t.Fatalf("failed PatchState 2: %v", err)
		}
		if patched2.Version() != 2 {
			t.Errorf("expected patched2 version 2, got %d", patched2.Version())
		}
		n1State2 := patched2.NodeState("node1")
		if n1State2.Status() != engine.StatusRunning {
			t.Errorf("expected StatusRunning, got %s", n1State2.Status())
		}
		if n1State2.Version() != 2 {
			t.Errorf("expected node1 version 2, got %d", n1State2.Version())
		}
		if n1State2.Output()["keyA"] != "valA" {
			t.Errorf("keyA was not retained across patch! got %v", n1State2.Output()["keyA"])
		}
		if n1State2.Output()["keyB"] != "valB" {
			t.Errorf("expected keyB valB, got %v", n1State2.Output()["keyB"])
		}

		// 3. Patch from version 2 -> 3: Unset keyA
		m3 := engine.NewStateMutation("node1", 2, nil, nil, []string{"keyA"}, nil)
		patched3, err := store.PatchState(ctx, execID, &m3, 2)
		if err != nil {
			t.Fatalf("failed PatchState 3: %v", err)
		}
		if patched3.Version() != 3 {
			t.Errorf("expected patched3 version 3, got %d", patched3.Version())
		}
		n1State3 := patched3.NodeState("node1")
		if _, exists := n1State3.Output()["keyA"]; exists {
			t.Errorf("expected keyA to be removed by unset")
		}
		if n1State3.Output()["keyB"] != "valB" {
			t.Errorf("expected keyB to remain valB, got %v", n1State3.Output()["keyB"])
		}

		// Verify store GetState matches version 3
		fetched3, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state: %v", err)
		}
		if fetched3.Version() != 3 {
			t.Errorf("expected stored version 3, got %d", fetched3.Version())
		}
	})
}

// RunConcurrency runs parallel and concurrency stress tests for StateStore.
func RunConcurrency(t *testing.T, cfg Config) {
	t.Run("ConcurrentCompetingWrites_SameExecutionID", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)
		ctx := context.Background()

		if err := store.SetState(ctx, execID, initState, 0); err != nil {
			t.Fatalf("failed to set initial state: %v", err)
		}

		s1, err := initState.WithStatus(0, engine.StatusReady)
		if err != nil {
			t.Fatalf("failed to transition state: %v", err)
		}

		const numWorkers = 25
		var successCount int32
		var conflictCount int32
		var otherErrCount int32

		startChan := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(numWorkers)

		for i := 0; i < numWorkers; i++ {
			go func() {
				defer wg.Done()
				<-startChan

				err := store.SetState(ctx, execID, s1, 0)
				if err == nil {
					atomic.AddInt32(&successCount, 1)
				} else if engine.IsStoreConflict(err) {
					atomic.AddInt32(&conflictCount, 1)
				} else {
					atomic.AddInt32(&otherErrCount, 1)
				}
			}()
		}

		close(startChan)
		wg.Wait()

		if successCount != 1 {
			t.Errorf("expected exactly 1 successful CAS write, got %d", successCount)
		}
		if conflictCount != int32(numWorkers-1) {
			t.Errorf("expected %d conflict errors, got %d", numWorkers-1, conflictCount)
		}
		if otherErrCount != 0 {
			t.Errorf("expected 0 unexpected errors, got %d", otherErrCount)
		}

		finalState, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state after race: %v", err)
		}
		if finalState.Version() != 1 {
			t.Errorf("expected final version 1, got %d", finalState.Version())
		}
	})

	t.Run("ConcurrentCompetingPatches_SameExecutionID", func(t *testing.T) {
		store := createStore(t, cfg)
		execID, err := engine.NewExecutionID()
		if err != nil {
			t.Fatalf("failed to generate execution ID: %v", err)
		}
		graph := createTestGraph(t)
		initState := createTestState(t, execID, graph)
		ctx := context.Background()

		if err := store.SetState(ctx, execID, initState, 0); err != nil {
			t.Fatalf("failed to set initial state: %v", err)
		}

		const numWorkers = 25
		var successCount int32
		var conflictCount int32
		var otherErrCount int32

		startChan := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(numWorkers)

		for i := 0; i < numWorkers; i++ {
			workerID := i
			go func() {
				defer wg.Done()
				<-startChan

				readyStatus := engine.StatusReady
				m := engine.NewStateMutation("node1", 0, &readyStatus, map[string]any{"worker": workerID}, nil, nil)
				_, err := store.PatchState(ctx, execID, &m, 0)
				if err == nil {
					atomic.AddInt32(&successCount, 1)
				} else if engine.IsStoreConflict(err) {
					atomic.AddInt32(&conflictCount, 1)
				} else {
					atomic.AddInt32(&otherErrCount, 1)
				}
			}()
		}

		close(startChan)
		wg.Wait()

		if successCount != 1 {
			t.Errorf("expected exactly 1 successful PatchState CAS write, got %d", successCount)
		}
		if conflictCount != int32(numWorkers-1) {
			t.Errorf("expected %d conflict errors, got %d", numWorkers-1, conflictCount)
		}
		if otherErrCount != 0 {
			t.Errorf("expected 0 unexpected errors, got %d", otherErrCount)
		}

		finalState, err := store.GetState(ctx, execID)
		if err != nil {
			t.Fatalf("failed to get state: %v", err)
		}
		if finalState.Version() != 1 {
			t.Errorf("expected version 1, got %d", finalState.Version())
		}
	})

	t.Run("ConcurrentIndependentExecutions", func(t *testing.T) {
		store := createStore(t, cfg)
		ctx := context.Background()
		graph := createTestGraph(t)

		const numExecutions = 20
		errChan := make(chan error, numExecutions)
		var wg sync.WaitGroup
		wg.Add(numExecutions)

		for i := 0; i < numExecutions; i++ {
			execNum := i
			go func() {
				defer wg.Done()

				execID, err := engine.NewExecutionID()
				if err != nil {
					errChan <- fmt.Errorf("failed to generate execID: %w", err)
					return
				}

				initState := createTestState(t, execID, graph)
				if err := store.SetState(ctx, execID, initState, 0); err != nil {
					errChan <- fmt.Errorf("worker %d: SetState failed: %w", execNum, err)
					return
				}

				// Verify version 0
				s0, err := store.GetState(ctx, execID)
				if err != nil || s0.Version() != 0 {
					errChan <- fmt.Errorf("worker %d: GetState v0 failed: %v", execNum, err)
					return
				}

				// Patch to version 1
				readyStatus := engine.StatusReady
				m1 := engine.NewStateMutation("node1", 0, &readyStatus, map[string]any{"worker": execNum}, nil, nil)
				p1, err := store.PatchState(ctx, execID, &m1, 0)
				if err != nil || p1.Version() != 1 {
					errChan <- fmt.Errorf("worker %d: PatchState v1 failed: %v", execNum, err)
					return
				}

				// Patch to version 2
				runningStatus := engine.StatusRunning
				m2 := engine.NewStateMutation("node1", 1, &runningStatus, map[string]any{"step": "running"}, nil, nil)
				p2, err := store.PatchState(ctx, execID, &m2, 1)
				if err != nil || p2.Version() != 2 {
					errChan <- fmt.Errorf("worker %d: PatchState v2 failed: %v", execNum, err)
					return
				}

				// Final read verification
				final, err := store.GetState(ctx, execID)
				if err != nil || final.Version() != 2 {
					errChan <- fmt.Errorf("worker %d: final read failed: %v", execNum, err)
					return
				}
				workerVal := final.NodeState("node1").Output()["worker"]
				var workerMatched bool
				switch v := workerVal.(type) {
				case int:
					workerMatched = (v == execNum)
				case float64:
					workerMatched = (v == float64(execNum))
				}
				if !workerMatched {
					errChan <- fmt.Errorf("worker %d: corrupted worker output: %v (%T)", execNum, workerVal, workerVal)
					return
				}
			}()
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			t.Errorf("independent execution concurrency error: %v", err)
		}
	})
}

// RunCapabilities runs tests for optional interfaces implemented by the store.
func RunCapabilities(t *testing.T, cfg Config) {
	t.Run("LockManager", func(t *testing.T) {
		var lockMgr engine.LockManager
		if cfg.NewLockManager != nil {
			lockMgr = cfg.NewLockManager(t)
		} else {
			store := createStore(t, cfg)
			if lm, ok := store.(engine.LockManager); ok {
				lockMgr = lm
			}
		}

		if lockMgr == nil {
			t.Skip("Store does not implement engine.LockManager; skipping LockManager capability tests")
			return
		}
		RunLockManager(t, lockMgr)
	})

	t.Run("EventPublisher", func(t *testing.T) {
		var publisher engine.EventPublisher
		if cfg.NewEventPublisher != nil {
			publisher = cfg.NewEventPublisher(t)
		} else {
			store := createStore(t, cfg)
			if ep, ok := store.(engine.EventPublisher); ok {
				publisher = ep
			}
		}

		if publisher == nil {
			t.Skip("Store does not implement engine.EventPublisher; skipping EventPublisher capability tests")
			return
		}
		RunEventPublisher(t, publisher)
	})

	t.Run("ExecutionLog", func(t *testing.T) {
		var execLog engine.ExecutionLog
		if cfg.NewExecutionLog != nil {
			execLog = cfg.NewExecutionLog(t)
		} else {
			store := createStore(t, cfg)
			if el, ok := store.(engine.ExecutionLog); ok {
				execLog = el
			}
		}

		if execLog == nil {
			t.Skip("Store does not implement engine.ExecutionLog; skipping ExecutionLog capability tests")
			return
		}
		RunExecutionLog(t, execLog)
	})
}

// RunLockManager runs comprehensive tests for engine.LockManager implementations.
func RunLockManager(t *testing.T, lockMgr engine.LockManager) {
	ctx := context.Background()
	resource := fmt.Sprintf("resource-lock-%d", time.Now().UnixNano())

	// 1. AcquireLock
	handle, err := lockMgr.AcquireLock(ctx, resource, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	if handle == nil {
		t.Fatalf("expected non-nil LockHandle")
	}
	if handle.Resource != resource {
		t.Errorf("expected resource %s, got %s", resource, handle.Resource)
	}
	if handle.Token == "" {
		t.Errorf("expected non-empty Token")
	}
	if handle.LeaseDuration != 5*time.Second {
		t.Errorf("expected LeaseDuration 5s, got %v", handle.LeaseDuration)
	}

	// 2. Conflicting AcquireLock on same resource
	ctxTimeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, errConflict := lockMgr.AcquireLock(ctxTimeout, resource, 5*time.Second)
	if errConflict == nil {
		t.Fatalf("expected error acquiring already locked resource %s, got nil", resource)
	}

	// 3. ExtendLock
	err = lockMgr.ExtendLock(ctx, handle, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to extend lock: %v", err)
	}
	if handle.LeaseDuration < 8*time.Second {
		t.Errorf("expected LeaseDuration to be at least 8s, got %v", handle.LeaseDuration)
	}

	// ExtendLock with invalid handle
	invalidHandle := &engine.LockHandle{
		Resource:      resource,
		Token:         "invalid-token-9999",
		LeaseDuration: 1 * time.Second,
	}
	err = lockMgr.ExtendLock(ctx, invalidHandle, 1*time.Second)
	if err == nil {
		t.Errorf("expected error extending lock with invalid token, got nil")
	}

	// 4. ReleaseLock
	err = lockMgr.ReleaseLock(ctx, handle)
	if err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}

	// Re-acquire after release must succeed
	handle2, err := lockMgr.AcquireLock(ctx, resource, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to re-acquire lock after release: %v", err)
	}
	if handle2 == nil || handle2.Token == "" {
		t.Errorf("expected valid handle on re-acquire")
	}
	_ = lockMgr.ReleaseLock(ctx, handle2)

	// Releasing already released lock handle should fail
	err = lockMgr.ReleaseLock(ctx, handle)
	if err == nil {
		t.Errorf("expected error releasing already released lock, got nil")
	}
}

// RunEventPublisher runs comprehensive tests for engine.EventPublisher implementations.
func RunEventPublisher(t *testing.T, pub engine.EventPublisher) {
	ctx := context.Background()
	execID, err := engine.NewExecutionID()
	if err != nil {
		t.Fatalf("failed to generate execution ID: %v", err)
	}

	// 1. PublishEvent single event
	ev1, err := engine.NewEvent(execID, 1, time.Now(), engine.EventTypeNodeStateChanged, "node1", engine.NodeStateChangedPayload{
		OldStatus: engine.StatusPending,
		NewStatus: engine.StatusRunning,
	})
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	err = pub.PublishEvent(ctx, ev1)
	if err != nil {
		t.Fatalf("failed to publish single event: %v", err)
	}

	// 2. PublishBatch
	ev2, err := engine.NewEvent(execID, 2, time.Now(), engine.EventTypeNodeCompleted, "node1", engine.NodeCompletedPayload{})
	if err != nil {
		t.Fatalf("failed to create event 2: %v", err)
	}
	ev3, err := engine.NewEvent(execID, 3, time.Now(), engine.EventTypeGraphCompleted, "", engine.GraphCompletedPayload{
		FinalStatus: engine.StatusSucceeded,
		Version:     1,
	})
	if err != nil {
		t.Fatalf("failed to create event 3: %v", err)
	}

	err = pub.PublishBatch(ctx, []*engine.Event{ev2, ev3})
	if err != nil {
		t.Fatalf("failed to publish event batch: %v", err)
	}

	// Publish empty batch should succeed without error
	err = pub.PublishBatch(ctx, []*engine.Event{})
	if err != nil {
		t.Fatalf("failed to publish empty batch: %v", err)
	}
}

// RunExecutionLog runs comprehensive tests for engine.ExecutionLog implementations.
func RunExecutionLog(t *testing.T, execLog engine.ExecutionLog) {
	ctx := context.Background()
	execID, err := engine.NewExecutionID()
	if err != nil {
		t.Fatalf("failed to generate execution ID: %v", err)
	}

	// 1. AppendLog ordered entries
	e1 := &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    1,
		Timestamp:   time.Now().Add(-2 * time.Second),
		Message:     "Execution started",
		Details:     map[string]any{"source": "test"},
	}
	e2 := &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    2,
		Timestamp:   time.Now().Add(-1 * time.Second),
		Message:     "Node node1 running",
	}
	e3 := &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    3,
		Timestamp:   time.Now(),
		Message:     "Node node1 completed",
	}

	for _, e := range []*engine.ExecutionLogEntry{e1, e2, e3} {
		if err := execLog.AppendLog(ctx, e); err != nil {
			t.Fatalf("failed to append log sequence %d: %v", e.Sequence, err)
		}
	}

	// 2. GetLogs retrieving all monotonic sequence entries
	logs, err := execLog.GetLogs(ctx, execID, 1, 10)
	if err != nil {
		t.Fatalf("failed to get logs: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}
	for i, expectedSeq := range []uint64{1, 2, 3} {
		if logs[i].Sequence != expectedSeq {
			t.Errorf("log %d: expected sequence %d, got %d", i, expectedSeq, logs[i].Sequence)
		}
	}
	if logs[0].Message != "Execution started" {
		t.Errorf("unexpected message in log 0: %s", logs[0].Message)
	}

	// 3. GetLogs with fromSequence filter
	logsFrom2, err := execLog.GetLogs(ctx, execID, 2, 10)
	if err != nil {
		t.Fatalf("failed to get logs from seq 2: %v", err)
	}
	if len(logsFrom2) != 2 {
		t.Fatalf("expected 2 logs from seq 2, got %d", len(logsFrom2))
	}
	if logsFrom2[0].Sequence != 2 || logsFrom2[1].Sequence != 3 {
		t.Errorf("expected sequences [2, 3], got [%d, %d]", logsFrom2[0].Sequence, logsFrom2[1].Sequence)
	}

	// 4. GetLogs with limit filter
	logsLimit1, err := execLog.GetLogs(ctx, execID, 1, 1)
	if err != nil {
		t.Fatalf("failed to get logs with limit 1: %v", err)
	}
	if len(logsLimit1) != 1 {
		t.Fatalf("expected 1 log with limit 1, got %d", len(logsLimit1))
	}
	if logsLimit1[0].Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", logsLimit1[0].Sequence)
	}

	// 5. Query non-existent execution ID returns empty slice without error
	nonExistentID, _ := engine.NewExecutionID()
	emptyLogs, err := execLog.GetLogs(ctx, nonExistentID, 1, 10)
	if err != nil {
		t.Fatalf("expected no error querying non-existent execution log, got %v", err)
	}
	if len(emptyLogs) != 0 {
		t.Errorf("expected 0 logs for non-existent execution, got %d", len(emptyLogs))
	}
}
