package engine_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

// mockStateStore is an in-memory implementation of engine.StateStore for testing.
type mockStateStore struct {
	mu     sync.RWMutex
	states map[engine.ExecutionID]*engine.GraphState
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{
		states: make(map[engine.ExecutionID]*engine.GraphState),
	}
}

func (m *mockStateStore) GetState(ctx context.Context, executionID engine.ExecutionID) (*engine.GraphState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.states[executionID]
	if !ok {
		return nil, &engine.StoreNotFoundError{
			ExecutionID: executionID,
			Entity:      "GraphState",
			Err:         engine.ErrStoreNotFound,
		}
	}
	return state, nil
}

func (m *mockStateStore) SetState(ctx context.Context, executionID engine.ExecutionID, state *engine.GraphState, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

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

func (m *mockStateStore) PatchState(ctx context.Context, executionID engine.ExecutionID, mutation *engine.StateMutation, expectedVersion uint64) (*engine.GraphState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

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

	// Apply mutation to state
	newState, err := current.Apply(*mutation)
	if err != nil {
		return nil, err
	}

	m.states[executionID] = newState
	return newState, nil
}

// mockLockManager is an in-memory implementation of engine.LockManager for testing.
type mockLockManager struct {
	mu    sync.Mutex
	locks map[string]*engine.LockHandle
}

func newMockLockManager() *mockLockManager {
	return &mockLockManager{
		locks: make(map[string]*engine.LockHandle),
	}
}

func (m *mockLockManager) AcquireLock(ctx context.Context, resource string, ttl time.Duration) (*engine.LockHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	existing, locked := m.locks[resource]
	if locked && now.Before(existing.AcquiredAt.Add(existing.LeaseDuration)) {
		return nil, fmt.Errorf("resource %q is already locked", resource)
	}

	handle := &engine.LockHandle{
		Resource:      resource,
		Token:         fmt.Sprintf("token-%d", now.UnixNano()),
		LeaseDuration: ttl,
		AcquiredAt:    now,
	}
	m.locks[resource] = handle
	return handle, nil
}

func (m *mockLockManager) ReleaseLock(ctx context.Context, handle *engine.LockHandle) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if handle == nil {
		return errors.New("lock handle cannot be nil")
	}

	current, exists := m.locks[handle.Resource]
	if !exists || current.Token != handle.Token {
		return errors.New("lock handle is invalid or not owner")
	}

	delete(m.locks, handle.Resource)
	return nil
}

func (m *mockLockManager) ExtendLock(ctx context.Context, handle *engine.LockHandle, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if handle == nil {
		return errors.New("lock handle cannot be nil")
	}

	current, exists := m.locks[handle.Resource]
	if !exists || current.Token != handle.Token {
		return errors.New("lock handle is invalid or not owner")
	}

	handle.LeaseDuration += ttl
	current.LeaseDuration = handle.LeaseDuration
	return nil
}

// mockEventPublisher is an in-memory implementation of engine.EventPublisher for testing.
type mockEventPublisher struct {
	mu     sync.Mutex
	events []*engine.Event
}

func newMockEventPublisher() *mockEventPublisher {
	return &mockEventPublisher{
		events: make([]*engine.Event, 0),
	}
}

func (m *mockEventPublisher) PublishEvent(ctx context.Context, event *engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if event == nil {
		return errors.New("event cannot be nil")
	}
	m.events = append(m.events, event)
	return nil
}

func (m *mockEventPublisher) PublishBatch(ctx context.Context, events []*engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, e := range events {
		if e == nil {
			return errors.New("batch event cannot be nil")
		}
		m.events = append(m.events, e)
	}
	return nil
}

// mockExecutionLog is an in-memory implementation of engine.ExecutionLog for testing.
type mockExecutionLog struct {
	mu   sync.RWMutex
	logs map[engine.ExecutionID][]*engine.ExecutionLogEntry
}

func newMockExecutionLog() *mockExecutionLog {
	return &mockExecutionLog{
		logs: make(map[engine.ExecutionID][]*engine.ExecutionLogEntry),
	}
}

func (m *mockExecutionLog) AppendLog(ctx context.Context, entry *engine.ExecutionLogEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if entry == nil {
		return errors.New("log entry cannot be nil")
	}

	m.logs[entry.ExecutionID] = append(m.logs[entry.ExecutionID], entry)
	return nil
}

func (m *mockExecutionLog) GetLogs(ctx context.Context, executionID engine.ExecutionID, fromSequence uint64, limit int) ([]*engine.ExecutionLogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, ok := m.logs[executionID]
	if !ok {
		return []*engine.ExecutionLogEntry{}, nil
	}

	res := make([]*engine.ExecutionLogEntry, 0)
	for _, e := range entries {
		if e.Sequence >= fromSequence {
			res = append(res, e)
			if limit > 0 && len(res) >= limit {
				break
			}
		}
	}
	return res, nil
}

func helperCreateGraph(t *testing.T) *engine.Graph {
	t.Helper()
	n1, _ := engine.NewNode("n1", "typeA", nil)
	n2, _ := engine.NewNode("n2", "typeB", nil)
	e1, _ := engine.NewEdge("n1", "n2", "", nil)
	g, err := engine.NewGraph("test-graph", []*engine.Node{n1, n2}, []*engine.Edge{e1})
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	return g
}

func TestStateStore_GetAndSetState(t *testing.T) {
	ctx := context.Background()
	store := newMockStateStore()
	var _ engine.StateStore = store

	execID, _ := engine.NewExecutionID()
	g := helperCreateGraph(t)
	initState, err := engine.NewGraphState(execID, g)
	if err != nil {
		t.Fatalf("failed to create graph state: %v", err)
	}

	// 1. GetState not found
	_, err = store.GetState(ctx, execID)
	if err == nil {
		t.Fatalf("expected error for non-existent executionID, got nil")
	}
	if !engine.IsStoreNotFound(err) {
		t.Errorf("expected IsStoreNotFound to be true, got %v", err)
	}
	if !errors.Is(err, engine.ErrStoreNotFound) {
		t.Errorf("expected errors.Is(err, ErrStoreNotFound) to be true, got %v", err)
	}
	var notFoundErr *engine.StoreNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Errorf("expected errors.As to match *StoreNotFoundError, got %v", err)
	} else {
		if notFoundErr.ExecutionID != execID {
			t.Errorf("expected executionID %s, got %s", execID, notFoundErr.ExecutionID)
		}
	}

	// 2. SetState initial state with expectedVersion != 0 should fail
	err = store.SetState(ctx, execID, initState, 1)
	if err == nil {
		t.Fatalf("expected conflict error when initial state expectedVersion != 0")
	}
	if !engine.IsStoreConflict(err) {
		t.Errorf("expected IsStoreConflict to be true, got %v", err)
	}

	// 3. SetState initial state with expectedVersion 0 should succeed
	err = store.SetState(ctx, execID, initState, 0)
	if err != nil {
		t.Fatalf("unexpected error setting initial state: %v", err)
	}

	// 4. GetState should now succeed
	fetched, err := store.GetState(ctx, execID)
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	if fetched.ExecutionID() != execID {
		t.Errorf("expected executionID %s, got %s", execID, fetched.ExecutionID())
	}
	if fetched.Version() != 0 {
		t.Errorf("expected version 0, got %d", fetched.Version())
	}

	// 5. Update state with stale version should fail with StoreConflictError
	statusReady := engine.StatusReady
	updatedState, err := fetched.WithStatus(0, statusReady)
	if err != nil {
		t.Fatalf("failed to update status: %v", err)
	}
	if updatedState.Version() != 1 {
		t.Fatalf("expected updated state version 1, got %d", updatedState.Version())
	}

	err = store.SetState(ctx, execID, updatedState, 5) // Stale version
	if err == nil {
		t.Fatalf("expected error setting state with wrong expectedVersion, got nil")
	}
	if !engine.IsStoreConflict(err) {
		t.Errorf("expected IsStoreConflict to be true, got %v", err)
	}
	if !errors.Is(err, engine.ErrStoreConflict) {
		t.Errorf("expected errors.Is(err, ErrStoreConflict), got %v", err)
	}
	var conflictErr *engine.StoreConflictError
	if !errors.As(err, &conflictErr) {
		t.Errorf("expected errors.As to match *StoreConflictError, got %v", err)
	} else {
		if conflictErr.ExpectedVersion != 5 {
			t.Errorf("expected ExpectedVersion 5, got %d", conflictErr.ExpectedVersion)
		}
		if conflictErr.ActualVersion != 0 {
			t.Errorf("expected ActualVersion 0, got %d", conflictErr.ActualVersion)
		}
	}

	// 6. Update state with correct version should succeed
	err = store.SetState(ctx, execID, updatedState, 0)
	if err != nil {
		t.Fatalf("failed to update state with correct expectedVersion: %v", err)
	}

	// 7. Verify updated state version
	fetchedAfterUpdate, err := store.GetState(ctx, execID)
	if err != nil {
		t.Fatalf("failed to fetch updated state: %v", err)
	}
	if fetchedAfterUpdate.Version() != 1 {
		t.Errorf("expected stored version 1, got %d", fetchedAfterUpdate.Version())
	}
}

func TestStateStore_PatchState(t *testing.T) {
	ctx := context.Background()
	store := newMockStateStore()

	execID, _ := engine.NewExecutionID()
	g := helperCreateGraph(t)
	initState, err := engine.NewGraphState(execID, g)
	if err != nil {
		t.Fatalf("failed to create graph state: %v", err)
	}

	statusReady := engine.StatusReady
	mutation := engine.NewStateMutation("n1", 0, &statusReady, map[string]any{"key1": "val1"}, nil, nil)

	// 1. Patch on non-existent executionID should return not found
	_, err = store.PatchState(ctx, execID, &mutation, 0)
	if err == nil {
		t.Fatalf("expected error on patching non-existent executionID")
	}
	if !engine.IsStoreNotFound(err) {
		t.Errorf("expected IsStoreNotFound to be true, got %v", err)
	}

	// Store initial state
	if err := store.SetState(ctx, execID, initState, 0); err != nil {
		t.Fatalf("failed to set initial state: %v", err)
	}

	// 2. Patch with mismatched expectedVersion should return StoreConflictError
	_, err = store.PatchState(ctx, execID, &mutation, 99)
	if err == nil {
		t.Fatalf("expected conflict error when expectedVersion mismatches")
	}
	if !engine.IsStoreConflict(err) {
		t.Errorf("expected IsStoreConflict to be true, got %v", err)
	}
	var confErr *engine.StoreConflictError
	if errors.As(err, &confErr) {
		if confErr.ExpectedVersion != 99 || confErr.ActualVersion != 0 {
			t.Errorf("expected (expected=99, actual=0), got (expected=%d, actual=%d)", confErr.ExpectedVersion, confErr.ActualVersion)
		}
	} else {
		t.Errorf("expected *StoreConflictError, got %T", err)
	}

	// 3. Patch with matching expectedVersion should succeed and increment version
	patchedState, err := store.PatchState(ctx, execID, &mutation, 0)
	if err != nil {
		t.Fatalf("failed to patch state: %v", err)
	}
	if patchedState.Version() != 1 {
		t.Errorf("expected patched state version 1, got %d", patchedState.Version())
	}
	n1State := patchedState.NodeState("n1")
	if n1State == nil || n1State.Status() != engine.StatusReady {
		t.Errorf("expected n1 to be ready, got %v", n1State)
	}

	// State in store should also be version 1
	fetched, err := store.GetState(ctx, execID)
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	if fetched.Version() != 1 {
		t.Errorf("expected stored state version 1, got %d", fetched.Version())
	}
}

func TestCapabilityInterfaces(t *testing.T) {
	ctx := context.Background()

	// 1. LockManager
	var lockMgr engine.LockManager = newMockLockManager()
	handle, err := lockMgr.AcquireLock(ctx, "exec-lock-1", 5*time.Second)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	if handle.Resource != "exec-lock-1" {
		t.Errorf("expected resource 'exec-lock-1', got %s", handle.Resource)
	}
	if handle.Token == "" {
		t.Errorf("expected non-empty token")
	}

	// Re-acquire while locked should fail
	_, err = lockMgr.AcquireLock(ctx, "exec-lock-1", 5*time.Second)
	if err == nil {
		t.Fatalf("expected error acquiring already locked resource")
	}

	// Extend lock
	err = lockMgr.ExtendLock(ctx, handle, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to extend lock: %v", err)
	}
	if handle.LeaseDuration != 7*time.Second {
		t.Errorf("expected 7s lease duration, got %v", handle.LeaseDuration)
	}

	// Release lock
	err = lockMgr.ReleaseLock(ctx, handle)
	if err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}

	// Acquire again after release should succeed
	handle2, err := lockMgr.AcquireLock(ctx, "exec-lock-1", 5*time.Second)
	if err != nil {
		t.Fatalf("failed to re-acquire lock after release: %v", err)
	}
	if handle2.Token == "" {
		t.Errorf("expected valid token")
	}

	// 2. EventPublisher
	var pub engine.EventPublisher = newMockEventPublisher()
	execID, _ := engine.NewExecutionID()
	ev1, err := engine.NewEvent(execID, 1, time.Now(), engine.EventTypeNodeStateChanged, "n1", engine.NodeStateChangedPayload{
		OldStatus: engine.StatusPending,
		NewStatus: engine.StatusRunning,
	})
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	if err := pub.PublishEvent(ctx, ev1); err != nil {
		t.Fatalf("failed to publish event: %v", err)
	}

	ev2, err := engine.NewEvent(execID, 2, time.Now(), engine.EventTypeNodeCompleted, "n1", engine.NodeCompletedPayload{})
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	if err := pub.PublishBatch(ctx, []*engine.Event{ev2}); err != nil {
		t.Fatalf("failed to publish batch: %v", err)
	}

	// 3. ExecutionLog
	var execLog engine.ExecutionLog = newMockExecutionLog()
	entry1 := &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    1,
		Timestamp:   time.Now(),
		Message:     "Execution started",
		Details:     map[string]any{"source": "test"},
	}
	entry2 := &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    2,
		Timestamp:   time.Now(),
		Message:     "Node n1 running",
	}

	if err := execLog.AppendLog(ctx, entry1); err != nil {
		t.Fatalf("failed to append log: %v", err)
	}
	if err := execLog.AppendLog(ctx, entry2); err != nil {
		t.Fatalf("failed to append log: %v", err)
	}

	logs, err := execLog.GetLogs(ctx, execID, 1, 10)
	if err != nil {
		t.Fatalf("failed to get logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	if logs[0].Message != "Execution started" {
		t.Errorf("unexpected log message: %s", logs[0].Message)
	}

	// Query with sequence filter
	logsFrom2, err := execLog.GetLogs(ctx, execID, 2, 10)
	if err != nil {
		t.Fatalf("failed to get logs from seq 2: %v", err)
	}
	if len(logsFrom2) != 1 || logsFrom2[0].Sequence != 2 {
		t.Errorf("expected 1 log with sequence 2, got %v", logsFrom2)
	}
}

func TestStoreErrors(t *testing.T) {
	execID, _ := engine.NewExecutionID()

	// 1. StoreNotFoundError
	errNF := &engine.StoreNotFoundError{
		ExecutionID: execID,
		Entity:      "GraphState",
		Err:         errors.New("root cause"),
	}
	if !errors.Is(errNF, engine.ErrStoreNotFound) {
		t.Errorf("expected errors.Is(errNF, ErrStoreNotFound)")
	}
	if !engine.IsStoreNotFound(errNF) {
		t.Errorf("expected IsStoreNotFound(errNF)")
	}
	if errNF.Error() == "" {
		t.Errorf("expected non-empty Error()")
	}

	// Wrapped StoreNotFoundError
	wrappedNF := fmt.Errorf("wrap: %w", errNF)
	if !errors.Is(wrappedNF, engine.ErrStoreNotFound) {
		t.Errorf("expected errors.Is(wrappedNF, ErrStoreNotFound)")
	}
	if !engine.IsStoreNotFound(wrappedNF) {
		t.Errorf("expected IsStoreNotFound(wrappedNF)")
	}

	// 2. StoreConflictError
	errConflict := &engine.StoreConflictError{
		ExecutionID:     execID,
		ExpectedVersion: 1,
		ActualVersion:   2,
		Err:             errors.New("root cause"),
	}
	if !errors.Is(errConflict, engine.ErrStoreConflict) {
		t.Errorf("expected errors.Is(errConflict, ErrStoreConflict)")
	}
	if !engine.IsStoreConflict(errConflict) {
		t.Errorf("expected IsStoreConflict(errConflict)")
	}
	if errConflict.Error() == "" {
		t.Errorf("expected non-empty Error()")
	}

	// Wrapped StoreConflictError
	wrappedConflict := fmt.Errorf("wrap: %w", errConflict)
	if !errors.Is(wrappedConflict, engine.ErrStoreConflict) {
		t.Errorf("expected errors.Is(wrappedConflict, ErrStoreConflict)")
	}
	if !engine.IsStoreConflict(wrappedConflict) {
		t.Errorf("expected IsStoreConflict(wrappedConflict)")
	}

	// 3. StoreUnavailableError
	errUnavail := &engine.StoreUnavailableError{
		Backend: "redis",
		Err:     errors.New("connection timeout"),
	}
	if !errors.Is(errUnavail, engine.ErrStoreUnavailable) {
		t.Errorf("expected errors.Is(errUnavail, ErrStoreUnavailable)")
	}
	if !engine.IsStoreUnavailable(errUnavail) {
		t.Errorf("expected IsStoreUnavailable(errUnavail)")
	}
	if errUnavail.Error() == "" {
		t.Errorf("expected non-empty Error()")
	}

	// Wrapped StoreUnavailableError
	wrappedUnavail := fmt.Errorf("wrap: %w", errUnavail)
	if !errors.Is(wrappedUnavail, engine.ErrStoreUnavailable) {
		t.Errorf("expected errors.Is(wrappedUnavail, ErrStoreUnavailable)")
	}
	if !engine.IsStoreUnavailable(wrappedUnavail) {
		t.Errorf("expected IsStoreUnavailable(wrappedUnavail)")
	}

	// Nil error checks
	if engine.IsStoreNotFound(nil) {
		t.Errorf("IsStoreNotFound(nil) should be false")
	}
	if engine.IsStoreConflict(nil) {
		t.Errorf("IsStoreConflict(nil) should be false")
	}
	if engine.IsStoreUnavailable(nil) {
		t.Errorf("IsStoreUnavailable(nil) should be false")
	}

	// Typed nil error checks
	var nilNF *engine.StoreNotFoundError
	if nilNF.Error() != "store: entity not found" {
		t.Errorf("unexpected nilNF.Error(): %s", nilNF.Error())
	}
	if nilNF.Unwrap() != nil {
		t.Errorf("expected nilNF.Unwrap() == nil")
	}

	var nilConflict *engine.StoreConflictError
	if nilConflict.Error() != "store: optimistic concurrency conflict" {
		t.Errorf("unexpected nilConflict.Error(): %s", nilConflict.Error())
	}
	if nilConflict.Unwrap() != nil {
		t.Errorf("expected nilConflict.Unwrap() == nil")
	}

	var nilUnavail *engine.StoreUnavailableError
	if nilUnavail.Error() != "store: storage backend unavailable" {
		t.Errorf("unexpected nilUnavail.Error(): %s", nilUnavail.Error())
	}
	if nilUnavail.Unwrap() != nil {
		t.Errorf("expected nilUnavail.Unwrap() == nil")
	}

	// Direct sentinel matches
	if !engine.IsStoreNotFound(engine.ErrStoreNotFound) {
		t.Errorf("expected IsStoreNotFound(ErrStoreNotFound) == true")
	}
	if !engine.IsStoreConflict(engine.ErrStoreConflict) {
		t.Errorf("expected IsStoreConflict(ErrStoreConflict) == true")
	}
	if !engine.IsStoreUnavailable(engine.ErrStoreUnavailable) {
		t.Errorf("expected IsStoreUnavailable(ErrStoreUnavailable) == true")
	}

	// Other errors should return false
	unrelatedErr := errors.New("unrelated error")
	if engine.IsStoreNotFound(unrelatedErr) {
		t.Errorf("IsStoreNotFound(unrelatedErr) should be false")
	}
	if engine.IsStoreConflict(unrelatedErr) {
		t.Errorf("IsStoreConflict(unrelatedErr) should be false")
	}
	if engine.IsStoreUnavailable(unrelatedErr) {
		t.Errorf("IsStoreUnavailable(unrelatedErr) should be false")
	}
}
