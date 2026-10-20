package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

// mockStateStore is a thread-safe mock implementation of engine.StateStore for testing.
type mockStateStore struct {
	mu            sync.RWMutex
	states        map[engine.ExecutionID]*engine.GraphState
	setStateErr   error
	patchStateErr error
	getStateErr   error
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{
		states: make(map[engine.ExecutionID]*engine.GraphState),
	}
}

func (m *mockStateStore) GetState(_ context.Context, executionID engine.ExecutionID) (*engine.GraphState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.getStateErr != nil {
		return nil, m.getStateErr
	}

	st, ok := m.states[executionID]
	if !ok {
		return nil, &engine.StoreNotFoundError{
			ExecutionID: executionID,
			Entity:      "GraphState",
			Err:         engine.ErrStoreNotFound,
		}
	}
	return st, nil
}

func (m *mockStateStore) SetState(_ context.Context, executionID engine.ExecutionID, state *engine.GraphState, expectedVersion uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.setStateErr != nil {
		return m.setStateErr
	}

	current, exists := m.states[executionID]
	if !exists {
		if expectedVersion != 0 {
			return &engine.StoreConflictError{
				ExecutionID:     executionID,
				ExpectedVersion: expectedVersion,
				ActualVersion:   0,
				Err:             engine.ErrStoreConflict,
			}
		}
		m.states[executionID] = state
		return nil
	}

	if current.Version() != expectedVersion {
		return &engine.StoreConflictError{
			ExecutionID:     executionID,
			ExpectedVersion: expectedVersion,
			ActualVersion:   current.Version(),
			Err:             engine.ErrStoreConflict,
		}
	}

	m.states[executionID] = state
	return nil
}

func (m *mockStateStore) PatchState(_ context.Context, executionID engine.ExecutionID, mutation *engine.StateMutation, expectedVersion uint64) (*engine.GraphState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.patchStateErr != nil {
		return nil, m.patchStateErr
	}

	current, exists := m.states[executionID]
	if !exists {
		return nil, &engine.StoreNotFoundError{
			ExecutionID: executionID,
			Entity:      "GraphState",
			Err:         engine.ErrStoreNotFound,
		}
	}

	if current.Version() != expectedVersion {
		return nil, &engine.StoreConflictError{
			ExecutionID:     executionID,
			ExpectedVersion: expectedVersion,
			ActualVersion:   current.Version(),
			Err:             engine.ErrStoreConflict,
		}
	}

	if mutation == nil {
		return nil, errors.New("mutation cannot be nil")
	}

	newState, err := current.Apply(*mutation)
	if err != nil {
		return nil, err
	}

	m.states[executionID] = newState
	return newState, nil
}

func helperCreateTestGraphState(t *testing.T, execID engine.ExecutionID) (*engine.GraphState, *engine.Graph) {
	t.Helper()
	n1, err := engine.NewNode("n1", "typeA", nil)
	require.NoError(t, err)
	n2, err := engine.NewNode("n2", "typeB", nil)
	require.NoError(t, err)
	e1, err := engine.NewEdge("n1", "n2", "", nil)
	require.NoError(t, err)

	g, err := engine.NewGraph("test-graph", []*engine.Node{n1, n2}, []*engine.Edge{e1})
	require.NoError(t, err)

	st, err := engine.NewGraphState(execID, g)
	require.NoError(t, err)
	return st, g
}

func TestPersistStateActivity_NilStore_Noop(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	activities := NewStateActivities(nil)
	assert.Nil(t, activities.Store())
	env.RegisterActivity(activities.PersistStateActivity)

	execID := engine.ExecutionID("exec-nil")
	initState, _ := helperCreateTestGraphState(t, execID)

	input := PersistStateInput{
		ExecutionID: execID,
		NodeID:      "n1",
		Version:     0,
		State:       initState,
	}

	val, err := env.ExecuteActivity(activities.PersistStateActivity, input)
	require.NoError(t, err)

	var res PersistStateResult
	require.NoError(t, val.Get(&res))

	assert.Equal(t, execID, res.ExecutionID)
	assert.Equal(t, uint64(0), res.Version)
	assert.True(t, res.IdempotentNoop)
	assert.False(t, res.IdempotentRecovery)
	assert.Equal(t, "exec-nil:n1:0", res.IdempotencyKey)
}

func TestPersistStateActivity_Success_VersionMatching(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	store := newMockStateStore()
	activities := NewStateActivities(store)
	assert.NotNil(t, activities.Store())
	env.RegisterActivity(activities.PersistStateActivity)

	execID := engine.ExecutionID("exec-success")
	initState, _ := helperCreateTestGraphState(t, execID)

	// 1. Initial state persist (version 0)
	input1 := PersistStateInput{
		ExecutionID: execID,
		Version:     0,
		State:       initState,
	}
	val1, err := env.ExecuteActivity(activities.PersistStateActivity, input1)
	require.NoError(t, err)

	var res1 PersistStateResult
	require.NoError(t, val1.Get(&res1))
	assert.Equal(t, uint64(0), res1.Version)
	assert.False(t, res1.IdempotentNoop)
	assert.False(t, res1.IdempotentRecovery)

	// 2. Advance state: apply mutation for node n1
	statusReady := engine.StatusReady
	mut1 := engine.NewStateMutation("n1", 0, &statusReady, map[string]any{"step": "init"}, nil, nil)
	stateV1, err := initState.Apply(mut1)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), stateV1.Version())

	// Persist state transition for n1 (expected version in store is 0)
	input2 := PersistStateInput{
		ExecutionID: execID,
		NodeID:      "n1",
		Version:     0,
		State:       stateV1,
	}
	val2, err := env.ExecuteActivity(activities.PersistStateActivity, input2)
	require.NoError(t, err)

	var res2 PersistStateResult
	require.NoError(t, val2.Get(&res2))
	assert.Equal(t, uint64(1), res2.Version)
	assert.False(t, res2.IdempotentRecovery)

	// 3. Verify directly in store
	stored, err := store.GetState(context.Background(), execID)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), stored.Version())
	assert.Equal(t, engine.StatusReady, stored.NodeState("n1").Status())
	assert.Equal(t, map[string]any{"step": "init"}, stored.NodeState("n1").Output())
}

func TestPersistStateActivity_Success_PatchMutation(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	store := newMockStateStore()
	activities := NewStateActivities(store)
	env.RegisterActivity(activities.PersistStateActivity)

	execID := engine.ExecutionID("exec-patch")
	initState, _ := helperCreateTestGraphState(t, execID)

	// Store initial state
	require.NoError(t, store.SetState(context.Background(), execID, initState, 0))

	// Persist mutation directly via PatchState
	statusReady := engine.StatusReady
	mut := engine.NewStateMutation("n1", 0, &statusReady, map[string]any{"data": 42}, nil, nil)

	input := PersistStateInput{
		ExecutionID: execID,
		NodeID:      "n1",
		Version:     0,
		Mutation:    &mut,
	}
	val, err := env.ExecuteActivity(activities.PersistStateActivity, input)
	require.NoError(t, err)

	var res PersistStateResult
	require.NoError(t, val.Get(&res))
	assert.Equal(t, uint64(1), res.Version)
	assert.False(t, res.IdempotentRecovery)

	stored, err := store.GetState(context.Background(), execID)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), stored.Version())
	assert.Equal(t, engine.StatusReady, stored.NodeState("n1").Status())
}

func TestPersistStateActivity_Idempotency_SameVersionRetry(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	store := newMockStateStore()
	activities := NewStateActivities(store)
	env.RegisterActivity(activities.PersistStateActivity)

	execID := engine.ExecutionID("exec-retry-same")
	initState, _ := helperCreateTestGraphState(t, execID)

	statusReady := engine.StatusReady
	mut := engine.NewStateMutation("n1", 0, &statusReady, map[string]any{"out": "done"}, nil, nil)
	stateV1, err := initState.Apply(mut)
	require.NoError(t, err)

	// Pre-populate store as if attempt 1 succeeded
	require.NoError(t, store.SetState(context.Background(), execID, initState, 0))
	require.NoError(t, store.SetState(context.Background(), execID, stateV1, 0))

	// Now execute retry with the exact same input (expectedVersion 0, target state version 1)
	input := PersistStateInput{
		ExecutionID:    execID,
		NodeID:         "n1",
		Version:        0,
		State:          stateV1,
		IdempotencyKey: "custom-idemp-key-1",
	}

	val, err := env.ExecuteActivity(activities.PersistStateActivity, input)
	require.NoError(t, err)

	var res PersistStateResult
	require.NoError(t, val.Get(&res))
	assert.Equal(t, execID, res.ExecutionID)
	assert.Equal(t, uint64(1), res.Version)
	assert.True(t, res.IdempotentRecovery)
	assert.Equal(t, "custom-idemp-key-1", res.IdempotencyKey)
}

func TestPersistStateActivity_Idempotency_HigherVersionStore(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	store := newMockStateStore()
	activities := NewStateActivities(store)
	env.RegisterActivity(activities.PersistStateActivity)

	execID := engine.ExecutionID("exec-retry-higher")
	initState, _ := helperCreateTestGraphState(t, execID)

	// Step 1: n1 becomes Ready -> version 1
	status1 := engine.StatusReady
	mut1 := engine.NewStateMutation("n1", 0, &status1, map[string]any{"step": 1}, nil, nil)
	stateV1, err := initState.Apply(mut1)
	require.NoError(t, err)

	// Step 2: n2 becomes Ready -> version 2
	status2 := engine.StatusReady
	mut2 := engine.NewStateMutation("n2", 1, &status2, map[string]any{"step": 2}, nil, nil)
	stateV2, err := stateV1.Apply(mut2)
	require.NoError(t, err)

	// Store has already advanced to version 2
	require.NoError(t, store.SetState(context.Background(), execID, initState, 0))
	require.NoError(t, store.SetState(context.Background(), execID, stateV2, 0))

	// Delayed retry for n1 arrives (which was targeting stateV1 at expectedVersion 0)
	input := PersistStateInput{
		ExecutionID: execID,
		NodeID:      "n1",
		Version:     0,
		State:       stateV1,
	}

	val, err := env.ExecuteActivity(activities.PersistStateActivity, input)
	require.NoError(t, err)

	var res PersistStateResult
	require.NoError(t, val.Get(&res))
	assert.Equal(t, uint64(2), res.Version)
	assert.True(t, res.IdempotentRecovery)
}

func TestPersistStateActivity_StoreUnavailable_RetryableError(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	store := newMockStateStore()
	unavailErr := &engine.StoreUnavailableError{
		Backend: "redis",
		Err:     errors.New("dial tcp: connection refused"),
	}
	store.setStateErr = unavailErr

	activities := NewStateActivities(store)
	env.RegisterActivity(activities.PersistStateActivity)

	execID := engine.ExecutionID("exec-unavail")
	initState, _ := helperCreateTestGraphState(t, execID)

	input := PersistStateInput{
		ExecutionID: execID,
		Version:     0,
		State:       initState,
	}

	_, err := env.ExecuteActivity(activities.PersistStateActivity, input)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr), "expected temporal.ApplicationError")
	assert.Equal(t, ErrorTypeRetryable, appErr.Type(), "expected retryable error type gargi.retryable")
	assert.False(t, appErr.NonRetryable(), "expected error to be retryable")
	assert.Contains(t, appErr.Error(), "storage backend unavailable")
}

func TestPersistStateActivity_UnrecoverableConflict_NonRetryableError(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	store := newMockStateStore()
	activities := NewStateActivities(store)
	env.RegisterActivity(activities.PersistStateActivity)

	execID := engine.ExecutionID("exec-conflict")
	initState, _ := helperCreateTestGraphState(t, execID)

	// Store initial state at version 0
	require.NoError(t, store.SetState(context.Background(), execID, initState, 0))

	// Input expects version 5 (future/stale version not present in store)
	input := PersistStateInput{
		ExecutionID: execID,
		NodeID:      "n1",
		Version:     5,
		State:       initState,
	}

	_, err := env.ExecuteActivity(activities.PersistStateActivity, input)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr), "expected temporal.ApplicationError")
	assert.Equal(t, ErrorTypeNonRetryable, appErr.Type(), "expected non-retryable error type gargi.non_retryable")
	assert.True(t, appErr.NonRetryable(), "expected error to be marked NonRetryable")
	assert.Contains(t, appErr.Error(), "unrecoverable state conflict")
}

func TestPersistStateActivity_ConflictCheck_GetStateUnavailable(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	store := newMockStateStore()
	activities := NewStateActivities(store)
	env.RegisterActivity(activities.PersistStateActivity)

	execID := engine.ExecutionID("exec-conflict-get-unavail")
	initState, _ := helperCreateTestGraphState(t, execID)

	// SetState will conflict because store is empty and expectedVersion is 2
	input := PersistStateInput{
		ExecutionID: execID,
		Version:     2,
		State:       initState,
	}

	// But GetState fails with StoreUnavailableError
	store.getStateErr = &engine.StoreUnavailableError{Backend: "postgres", Err: errors.New("timeout")}

	_, err := env.ExecuteActivity(activities.PersistStateActivity, input)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, ErrorTypeRetryable, appErr.Type())
	assert.False(t, appErr.NonRetryable())
}

func TestPersistStateActivity_InvalidInput(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	store := newMockStateStore()
	activities := NewStateActivities(store)
	env.RegisterActivity(activities.PersistStateActivity)

	// 1. Empty ExecutionID
	input1 := PersistStateInput{
		ExecutionID: "",
		Version:     0,
	}
	_, err := env.ExecuteActivity(activities.PersistStateActivity, input1)
	require.Error(t, err)
	var appErr1 *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr1))
	assert.True(t, appErr1.NonRetryable())

	// 2. Missing both State and Mutation
	input2 := PersistStateInput{
		ExecutionID: "exec-valid",
		Version:     0,
	}
	_, err = env.ExecuteActivity(activities.PersistStateActivity, input2)
	require.Error(t, err)
	var appErr2 *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr2))
	assert.True(t, appErr2.NonRetryable())
}

func TestPersistStateActivity_WriteStateActivityAlias(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()

	store := newMockStateStore()
	activities := NewStateActivities(store)
	env.RegisterActivity(activities.WriteStateActivity)

	execID := engine.ExecutionID("exec-alias")
	initState, _ := helperCreateTestGraphState(t, execID)

	input := PersistStateInput{
		ExecutionID: execID,
		Version:     0,
		State:       initState,
	}

	val, err := env.ExecuteActivity(activities.WriteStateActivity, input)
	require.NoError(t, err)

	var res PersistStateResult
	require.NoError(t, val.Get(&res))
	assert.Equal(t, uint64(0), res.Version)
}

func TestFetchStateWithFallback(t *testing.T) {
	ctx := context.Background()
	execID := engine.ExecutionID("exec-fallback")
	initState, _ := helperCreateTestGraphState(t, execID)

	// 1. Store is present and has state
	store := newMockStateStore()
	require.NoError(t, store.SetState(ctx, execID, initState, 0))

	st, err := FetchStateWithFallback(ctx, store, nil, string(execID), "")
	require.NoError(t, err)
	assert.Equal(t, execID, st.ExecutionID())
	assert.Equal(t, uint64(0), st.Version())

	// 2. Store is nil, no temporal client -> returns error
	_, err = FetchStateWithFallback(ctx, nil, nil, string(execID), "")
	require.Error(t, err)

	// 3. Store returns not found, no temporal client -> returns error
	emptyStore := newMockStateStore()
	_, err = FetchStateWithFallback(ctx, emptyStore, nil, "non-existent", "")
	require.Error(t, err)

	// 4. Store returns unavailable, no temporal client -> returns error
	unavailStore := newMockStateStore()
	unavailStore.getStateErr = &engine.StoreUnavailableError{Backend: "redis"}
	_, err = FetchStateWithFallback(ctx, unavailStore, nil, string(execID), "")
	require.Error(t, err)
}
