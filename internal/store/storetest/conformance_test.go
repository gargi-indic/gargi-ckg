package storetest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/store/storetest"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

func cloneGraphState(s *engine.GraphState) *engine.GraphState {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal state for cloning: %v", err))
	}
	var clone engine.GraphState
	if err := json.Unmarshal(data, &clone); err != nil {
		panic(fmt.Sprintf("failed to unmarshal state for cloning: %v", err))
	}
	return &clone
}

// referenceStateStore implements engine.StateStore with deep immutability and thread safety.
type referenceStateStore struct {
	mu     sync.RWMutex
	states map[engine.ExecutionID]*engine.GraphState
}

func newReferenceStateStore() *referenceStateStore {
	return &referenceStateStore{
		states: make(map[engine.ExecutionID]*engine.GraphState),
	}
}

func (s *referenceStateStore) GetState(ctx context.Context, executionID engine.ExecutionID) (*engine.GraphState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	st, exists := s.states[executionID]
	if !exists {
		return nil, &engine.StoreNotFoundError{
			ExecutionID: executionID,
			Entity:      "GraphState",
			Err:         engine.ErrStoreNotFound,
		}
	}
	return cloneGraphState(st), nil
}

func (s *referenceStateStore) SetState(ctx context.Context, executionID engine.ExecutionID, state *engine.GraphState, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.states[executionID]
	if !exists {
		if expectedVersion != 0 {
			return &engine.StoreConflictError{
				ExecutionID:     executionID,
				ExpectedVersion: expectedVersion,
				ActualVersion:   0,
				Err:             engine.ErrStoreConflict,
			}
		}
		s.states[executionID] = cloneGraphState(state)
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

	s.states[executionID] = cloneGraphState(state)
	return nil
}

func (s *referenceStateStore) PatchState(ctx context.Context, executionID engine.ExecutionID, mutation *engine.StateMutation, expectedVersion uint64) (*engine.GraphState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.states[executionID]
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

	s.states[executionID] = cloneGraphState(newState)
	return cloneGraphState(newState), nil
}

// referenceLockManager implements engine.LockManager.
type referenceLockManager struct {
	mu    sync.Mutex
	locks map[string]*engine.LockHandle
}

func newReferenceLockManager() *referenceLockManager {
	return &referenceLockManager{
		locks: make(map[string]*engine.LockHandle),
	}
}

func (m *referenceLockManager) AcquireLock(ctx context.Context, resource string, ttl time.Duration) (*engine.LockHandle, error) {
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
		Token:         fmt.Sprintf("token-%d-%d", now.UnixNano(), time.Now().Nanosecond()),
		LeaseDuration: ttl,
		AcquiredAt:    now,
	}
	m.locks[resource] = handle
	return handle, nil
}

func (m *referenceLockManager) ReleaseLock(ctx context.Context, handle *engine.LockHandle) error {
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

func (m *referenceLockManager) ExtendLock(ctx context.Context, handle *engine.LockHandle, ttl time.Duration) error {
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

// referenceEventPublisher implements engine.EventPublisher.
type referenceEventPublisher struct {
	mu     sync.Mutex
	events []*engine.Event
}

func newReferenceEventPublisher() *referenceEventPublisher {
	return &referenceEventPublisher{
		events: make([]*engine.Event, 0),
	}
}

func (p *referenceEventPublisher) PublishEvent(ctx context.Context, event *engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if event == nil {
		return errors.New("event cannot be nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
	return nil
}

func (p *referenceEventPublisher) PublishBatch(ctx context.Context, events []*engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range events {
		if e == nil {
			return errors.New("batch event cannot be nil")
		}
		p.events = append(p.events, e)
	}
	return nil
}

// referenceExecutionLog implements engine.ExecutionLog.
type referenceExecutionLog struct {
	mu   sync.RWMutex
	logs map[engine.ExecutionID][]*engine.ExecutionLogEntry
}

func newReferenceExecutionLog() *referenceExecutionLog {
	return &referenceExecutionLog{
		logs: make(map[engine.ExecutionID][]*engine.ExecutionLogEntry),
	}
}

func (l *referenceExecutionLog) AppendLog(ctx context.Context, entry *engine.ExecutionLogEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry == nil {
		return errors.New("log entry cannot be nil")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs[entry.ExecutionID] = append(l.logs[entry.ExecutionID], entry)
	return nil
}

func (l *referenceExecutionLog) GetLogs(ctx context.Context, executionID engine.ExecutionID, fromSequence uint64, limit int) ([]*engine.ExecutionLogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.RLock()
	defer l.mu.RUnlock()

	entries, ok := l.logs[executionID]
	if !ok {
		return []*engine.ExecutionLogEntry{}, nil
	}

	var res []*engine.ExecutionLogEntry
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

// fullMockStore implements engine.StateStore, engine.LockManager, engine.EventPublisher, and engine.ExecutionLog.
type fullMockStore struct {
	*referenceStateStore
	*referenceLockManager
	*referenceEventPublisher
	*referenceExecutionLog
}

func newFullMockStore() *fullMockStore {
	return &fullMockStore{
		referenceStateStore:     newReferenceStateStore(),
		referenceLockManager:    newReferenceLockManager(),
		referenceEventPublisher: newReferenceEventPublisher(),
		referenceExecutionLog:   newReferenceExecutionLog(),
	}
}

// TestConformanceSuite_AllCapabilities tests storetest.Run with a store that implements all capabilities.
func TestConformanceSuite_AllCapabilities(t *testing.T) {
	storetest.Run(t, func(_ *testing.T) engine.StateStore {
		return newFullMockStore()
	})
}

// TestConformanceSuite_StoreFactoryType tests storetest.Run passing a typed StoreFactory.
func TestConformanceSuite_StoreFactoryType(t *testing.T) {
	var factory storetest.StoreFactory = func(_ *testing.T) engine.StateStore {
		return newFullMockStore()
	}
	storetest.Run(t, factory)
}

// TestConformanceSuite_ConfigPointer tests storetest.Run passing *storetest.Config.
func TestConformanceSuite_ConfigPointer(t *testing.T) {
	cfg := &storetest.Config{
		NewStore: func(_ *testing.T) engine.StateStore {
			return newFullMockStore()
		},
	}
	storetest.Run(t, cfg)
}

// TestConformanceSuite_RunSuite tests storetest.RunSuite.
func TestConformanceSuite_RunSuite(t *testing.T) {
	cfg := storetest.Config{
		NewStore: func(_ *testing.T) engine.StateStore {
			return newFullMockStore()
		},
	}
	storetest.RunSuite(t, cfg)
}

// TestConformanceSuite_RunFactory tests storetest.RunFactory.
func TestConformanceSuite_RunFactory(t *testing.T) {
	storetest.RunFactory(t, func(_ *testing.T) engine.StateStore {
		return newFullMockStore()
	})
}

// TestConformanceSuite_StoreOnly verifies that stores not implementing capability interfaces gracefully skip them.
func TestConformanceSuite_StoreOnly(t *testing.T) {
	storetest.Run(t, func(_ *testing.T) engine.StateStore {
		return newReferenceStateStore()
	})
}

// TestConformanceSuite_ExplicitCapabilityFactories tests providing separate capability factories in Config.
func TestConformanceSuite_ExplicitCapabilityFactories(t *testing.T) {
	cfg := storetest.Config{
		NewStore: func(_ *testing.T) engine.StateStore {
			return newReferenceStateStore()
		},
		NewLockManager: func(_ *testing.T) engine.LockManager {
			return newReferenceLockManager()
		},
		NewEventPublisher: func(_ *testing.T) engine.EventPublisher {
			return newReferenceEventPublisher()
		},
		NewExecutionLog: func(_ *testing.T) engine.ExecutionLog {
			return newReferenceExecutionLog()
		},
	}
	storetest.Run(t, cfg)
}

// TestConformanceSuite_TeardownCallback verifies that Teardown is called after each test.
func TestConformanceSuite_TeardownCallback(t *testing.T) {
	var teardownCount int32
	cfg := storetest.Config{
		NewStore: func(_ *testing.T) engine.StateStore {
			return newReferenceStateStore()
		},
		Teardown: func(t *testing.T, store engine.StateStore) {
			if store == nil {
				t.Errorf("expected non-nil store in Teardown")
			}
			atomic.AddInt32(&teardownCount, 1)
		},
	}
	storetest.RunCRUD(t, cfg)

	if atomic.LoadInt32(&teardownCount) == 0 {
		t.Errorf("expected Teardown to be called at least once, got %d", teardownCount)
	}
}
