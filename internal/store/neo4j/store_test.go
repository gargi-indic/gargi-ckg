package neo4j_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	neo4jstore "github.com/gargi-indic/gargi-flow/internal/store/neo4j"
	"github.com/gargi-indic/gargi-flow/internal/store/storetest"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestStore(t *testing.T) *neo4jstore.Store {
	t.Helper()
	if uri := os.Getenv("GARGI_NEO4J_URI"); uri != "" {
		user := os.Getenv("GARGI_NEO4J_USER")
		pass := os.Getenv("GARGI_NEO4J_PASSWORD")
		s, err := neo4jstore.New(context.Background(), neo4jstore.Config{
			URI:      uri,
			User:     user,
			Password: pass,
		})
		require.NoError(t, err)
		return s
	}

	exec := neo4jstore.NewInMemoryGraphExecutor()
	return neo4jstore.NewWithExecutor(exec)
}

func TestConformance(t *testing.T) {
	storetest.Run(t, storetest.Config{
		NewStore: func(t *testing.T) engine.StateStore {
			return createTestStore(t)
		},
		Teardown: func(_ *testing.T, s engine.StateStore) {
			if n, ok := s.(*neo4jstore.Store); ok {
				_ = n.Close(context.Background())
			}
		},
	})
}

func TestExampleCypher_FailureBlastRadius(t *testing.T) {
	ctx := context.Background()
	store := createTestStore(t)
	defer func() { _ = store.Close(ctx) }()

	// Build a diamond graph:
	//       n1 (Root)
	//      /  \
	//     v    v
	//    n2    n3
	//     \    /
	//      v  v
	//       n4
	//       |
	//       v
	//       n5 (Leaf)
	n1, err := engine.NewNode("n1", "passthrough", nil)
	require.NoError(t, err)
	n2, err := engine.NewNode("n2", "http_call", nil)
	require.NoError(t, err)
	n3, err := engine.NewNode("n3", "transform", nil)
	require.NoError(t, err)
	n4, err := engine.NewNode("n4", "conditional_branch", nil)
	require.NoError(t, err)
	n5, err := engine.NewNode("n5", "passthrough", nil)
	require.NoError(t, err)

	e1, _ := engine.NewEdge("n1", "n2", "", nil)
	e2, _ := engine.NewEdge("n1", "n3", "", nil)
	e3, _ := engine.NewEdge("n2", "n4", "", nil)
	e4, _ := engine.NewEdge("n3", "n4", "", nil)
	e5, _ := engine.NewEdge("n4", "n5", "", nil)

	g, err := engine.NewGraph("diamond-dag", []*engine.Node{n1, n2, n3, n4, n5}, []*engine.Edge{e1, e2, e3, e4, e5})
	require.NoError(t, err)

	execID, err := engine.NewExecutionID()
	require.NoError(t, err)

	state, err := engine.NewGraphState(execID, g)
	require.NoError(t, err)

	err = store.SetState(ctx, execID, state, 0)
	require.NoError(t, err)

	// Persist the graph topology in Neo4j ([:DEPENDS_ON] relationships)
	err = store.PersistGraphStructure(ctx, execID, g)
	require.NoError(t, err)

	// Before any failure, blast radius must be empty
	blastRadius, err := store.FailureBlastRadius(ctx, execID)
	require.NoError(t, err)
	assert.Empty(t, blastRadius)

	// Transition n2: Pending -> Ready -> Running -> Failed
	readyStatus := engine.StatusReady
	mReady := engine.NewStateMutation("n2", 0, &readyStatus, nil, nil, nil)
	_, err = store.PatchState(ctx, execID, &mReady, 0)
	require.NoError(t, err)

	runningStatus := engine.StatusRunning
	mRunning := engine.NewStateMutation("n2", 1, &runningStatus, nil, nil, nil)
	_, err = store.PatchState(ctx, execID, &mRunning, 1)
	require.NoError(t, err)

	failedStatus := engine.StatusFailed
	errMsg := "HTTP 500 Internal Server Error"
	mFailed := engine.NewStateMutation("n2", 2, &failedStatus, nil, nil, &errMsg)
	patchedState, err := store.PatchState(ctx, execID, &mFailed, 2)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), patchedState.Version())
	assert.Equal(t, engine.StatusFailed, patchedState.NodeState("n2").Status())

	// Query failure blast radius:
	// Because n2 failed, n4 (which depends on n2) and n5 (which depends on n4) must be impacted.
	// n1 (upstream of n2) and n3 (independent sibling) must NOT be in the blast radius!
	blastRadius, err = store.FailureBlastRadius(ctx, execID)
	require.NoError(t, err)
	require.Len(t, blastRadius, 2)

	impactedIDs := map[engine.NodeID]bool{
		blastRadius[0].NodeID: true,
		blastRadius[1].NodeID: true,
	}
	assert.True(t, impactedIDs["n4"], "n4 should be in failure blast radius")
	assert.True(t, impactedIDs["n5"], "n5 should be in failure blast radius")
	assert.False(t, impactedIDs["n1"], "n1 is upstream and should NOT be impacted")
	assert.False(t, impactedIDs["n3"], "n3 is independent sibling and should NOT be impacted")
}

func TestExampleCypher_CriticalPath(t *testing.T) {
	ctx := context.Background()
	store := createTestStore(t)
	defer func() { _ = store.Close(ctx) }()

	// Graph with two paths:
	// Path 1 (length 4): a -> b -> c -> d
	// Path 2 (length 2): a -> e
	a, _ := engine.NewNode("a", "step", nil)
	b, _ := engine.NewNode("b", "step", nil)
	c, _ := engine.NewNode("c", "step", nil)
	d, _ := engine.NewNode("d", "step", nil)
	e, _ := engine.NewNode("e", "step", nil)

	e1, _ := engine.NewEdge("a", "b", "", nil)
	e2, _ := engine.NewEdge("b", "c", "", nil)
	e3, _ := engine.NewEdge("c", "d", "", nil)
	e4, _ := engine.NewEdge("a", "e", "", nil)

	g, err := engine.NewGraph("critical-dag", []*engine.Node{a, b, c, d, e}, []*engine.Edge{e1, e2, e3, e4})
	require.NoError(t, err)

	execID, err := engine.NewExecutionID()
	require.NoError(t, err)

	state, err := engine.NewGraphState(execID, g)
	require.NoError(t, err)

	err = store.SetState(ctx, execID, state, 0)
	require.NoError(t, err)

	err = store.PersistGraphStructure(ctx, execID, g)
	require.NoError(t, err)

	critPath, err := store.CriticalPath(ctx, execID)
	require.NoError(t, err)
	require.NotEmpty(t, critPath)

	// Critical path from leaf to root should be length 4: d, c, b, a
	assert.Len(t, critPath, 4)
	assert.Equal(t, "d", critPath[0])
	assert.Equal(t, "c", critPath[1])
	assert.Equal(t, "b", critPath[2])
	assert.Equal(t, "a", critPath[3])
}

func TestUpstreamDependencies(t *testing.T) {
	ctx := context.Background()
	store := createTestStore(t)
	defer func() { _ = store.Close(ctx) }()

	n1, _ := engine.NewNode("n1", "step", nil)
	n2, _ := engine.NewNode("n2", "step", nil)
	n3, _ := engine.NewNode("n3", "step", nil)

	e1, _ := engine.NewEdge("n1", "n2", "", nil)
	e2, _ := engine.NewEdge("n2", "n3", "", nil)

	g, err := engine.NewGraph("linear-dag", []*engine.Node{n1, n2, n3}, []*engine.Edge{e1, e2})
	require.NoError(t, err)

	execID, err := engine.NewExecutionID()
	require.NoError(t, err)

	state, err := engine.NewGraphState(execID, g)
	require.NoError(t, err)

	err = store.SetState(ctx, execID, state, 0)
	require.NoError(t, err)

	err = store.PersistGraphStructure(ctx, execID, g)
	require.NoError(t, err)

	// Upstream of n3 should be n1 and n2
	upstream, err := store.UpstreamDependencies(ctx, execID, "n3")
	require.NoError(t, err)
	require.Len(t, upstream, 2)
	assert.Equal(t, engine.NodeID("n1"), upstream[0].NodeID)
	assert.Equal(t, engine.NodeID("n2"), upstream[1].NodeID)
}

func TestStore_ErrorClassifications(t *testing.T) {
	ctx := context.Background()
	store := createTestStore(t)
	defer func() { _ = store.Close(ctx) }()

	execID, _ := engine.NewExecutionID()

	// 1. StoreNotFoundError
	_, err := store.GetState(ctx, execID)
	require.Error(t, err)
	assert.True(t, engine.IsStoreNotFound(err))
	assert.True(t, errors.Is(err, engine.ErrStoreNotFound))

	var notFound *engine.StoreNotFoundError
	require.True(t, errors.As(err, &notFound))
	assert.Equal(t, execID, notFound.ExecutionID)

	// 2. StoreConflictError
	n1, _ := engine.NewNode("n1", "step", nil)
	g, _ := engine.NewGraph("g1", []*engine.Node{n1}, nil)
	initState, _ := engine.NewGraphState(execID, g)

	// Initial SetState with expectedVersion != 0 must conflict
	err = store.SetState(ctx, execID, initState, 5)
	require.Error(t, err)
	assert.True(t, engine.IsStoreConflict(err))
	assert.True(t, errors.Is(err, engine.ErrStoreConflict))

	var conflict *engine.StoreConflictError
	require.True(t, errors.As(err, &conflict))
	assert.Equal(t, uint64(5), conflict.ExpectedVersion)
	assert.Equal(t, uint64(0), conflict.ActualVersion)

	// 3. StoreUnavailableError on closed store
	_ = store.Close(ctx)
	_, err = store.GetState(ctx, execID)
	require.Error(t, err)
	assert.True(t, engine.IsStoreUnavailable(err))
	assert.True(t, errors.Is(err, engine.ErrStoreUnavailable))
}

func TestStore_ExecutionLog(t *testing.T) {
	ctx := context.Background()
	store := createTestStore(t)
	defer func() { _ = store.Close(ctx) }()

	execID, _ := engine.NewExecutionID()
	n1, _ := engine.NewNode("n1", "step", nil)
	g, _ := engine.NewGraph("g1", []*engine.Node{n1}, nil)
	st, _ := engine.NewGraphState(execID, g)
	require.NoError(t, store.SetState(ctx, execID, st, 0))

	// Append logs
	e1 := &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    1,
		Timestamp:   time.Now().Add(-2 * time.Second),
		Message:     "Execution initiated",
		Details:     map[string]any{"trigger": "api"},
	}
	e2 := &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    2,
		Timestamp:   time.Now().Add(-1 * time.Second),
		Message:     "Node n1 started",
	}
	e3 := &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    3,
		Timestamp:   time.Now(),
		Message:     "Node n1 finished",
	}

	require.NoError(t, store.AppendLog(ctx, e1))
	require.NoError(t, store.AppendLog(ctx, e2))
	require.NoError(t, store.AppendLog(ctx, e3))

	// Get all logs
	logs, err := store.GetLogs(ctx, execID, 1, 10)
	require.NoError(t, err)
	require.Len(t, logs, 3)
	assert.Equal(t, uint64(1), logs[0].Sequence)
	assert.Equal(t, "Execution initiated", logs[0].Message)
	assert.Equal(t, "api", logs[0].Details["trigger"])

	// Filter from sequence 2
	logs2, err := store.GetLogs(ctx, execID, 2, 10)
	require.NoError(t, err)
	require.Len(t, logs2, 2)
	assert.Equal(t, uint64(2), logs2[0].Sequence)
	assert.Equal(t, uint64(3), logs2[1].Sequence)

	// Limit 1
	logsLimit, err := store.GetLogs(ctx, execID, 1, 1)
	require.NoError(t, err)
	require.Len(t, logsLimit, 1)
	assert.Equal(t, uint64(1), logsLimit[0].Sequence)
}

func TestStore_ConstructorErrors(t *testing.T) {
	ctx := context.Background()

	// Empty URI without executor should return error
	_, err := neo4jstore.New(ctx, neo4jstore.Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection URI is required")

	// Invalid URI should wrap as StoreUnavailableError
	_, err = neo4jstore.New(ctx, neo4jstore.Config{
		URI: "bolt://127.0.0.1:59999",
	})
	require.Error(t, err)
	assert.True(t, engine.IsStoreUnavailable(err))
}
