package memory_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/store/memory"
	"github.com/gargi-indic/gargi-ckg/internal/store/storetest"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) engine.StateStore {
		return memory.New()
	})
}

func TestStore_ConstructorAliases(t *testing.T) {
	s1 := memory.New()
	if s1 == nil {
		t.Fatal("expected non-nil Store from New()")
	}
	s2 := memory.NewStore()
	if s2 == nil {
		t.Fatal("expected non-nil Store from NewStore()")
	}
}

func TestPubSub_SingleSubscriber(t *testing.T) {
	s := memory.New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	execID1, err := engine.NewExecutionID()
	if err != nil {
		t.Fatalf("failed to create execID1: %v", err)
	}
	execID2, err := engine.NewExecutionID()
	if err != nil {
		t.Fatalf("failed to create execID2: %v", err)
	}

	sub1, err := s.Subscribe(ctx, execID1)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// 1. Publish event for execID1
	ev1, err := engine.NewEvent(execID1, 1, time.Now(), engine.EventTypeNodeCompleted, "nodeA", engine.NodeCompletedPayload{})
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	if err := s.PublishEvent(ctx, ev1); err != nil {
		t.Fatalf("failed to publish event: %v", err)
	}

	select {
	case received := <-sub1:
		if received.ExecutionID() != execID1 {
			t.Errorf("expected execID %s, got %s", execID1, received.ExecutionID())
		}
		if received.Sequence() != 1 {
			t.Errorf("expected sequence 1, got %d", received.Sequence())
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for published event on subscriber channel")
	}

	// 2. Publish event for execID2 (should NOT be received by sub1)
	ev2, err := engine.NewEvent(execID2, 1, time.Now(), engine.EventTypeNodeCompleted, "nodeB", engine.NodeCompletedPayload{})
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	if err := s.PublishEvent(ctx, ev2); err != nil {
		t.Fatalf("failed to publish event for execID2: %v", err)
	}

	select {
	case unexpected := <-sub1:
		t.Fatalf("unexpected event received on filtered subscriber: %v", unexpected)
	case <-time.After(100 * time.Millisecond):
		// Expected: no event received
	}
}

func TestPubSub_SubscribeAll(t *testing.T) {
	s := memory.New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	execID1, _ := engine.NewExecutionID()
	execID2, _ := engine.NewExecutionID()

	subAll, err := s.SubscribeAll(ctx)
	if err != nil {
		t.Fatalf("failed to subscribe all: %v", err)
	}

	ev1, _ := engine.NewEvent(execID1, 1, time.Now(), engine.EventTypeNodeCompleted, "node1", engine.NodeCompletedPayload{})
	ev2, _ := engine.NewEvent(execID2, 2, time.Now(), engine.EventTypeNodeCompleted, "node2", engine.NodeCompletedPayload{})

	if err := s.PublishBatch(ctx, []*engine.Event{ev1, ev2}); err != nil {
		t.Fatalf("failed to publish batch: %v", err)
	}

	for _, expected := range []*engine.Event{ev1, ev2} {
		select {
		case received := <-subAll:
			if received.ExecutionID() != expected.ExecutionID() {
				t.Errorf("expected execution %s, got %s", expected.ExecutionID(), received.ExecutionID())
			}
			if received.Sequence() != expected.Sequence() {
				t.Errorf("expected sequence %d, got %d", expected.Sequence(), received.Sequence())
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timeout waiting for event in SubscribeAll")
		}
	}
}

func TestPubSub_ConcurrentSubscribers(t *testing.T) {
	s := memory.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	execIDA, _ := engine.NewExecutionID()
	execIDB, _ := engine.NewExecutionID()

	const numSubscribersPerType = 5
	const numEventsPerType = 20

	var subsA []<-chan *engine.Event
	var subsB []<-chan *engine.Event
	var subsAll []<-chan *engine.Event

	for i := 0; i < numSubscribersPerType; i++ {
		chA, err := s.Subscribe(ctx, execIDA)
		if err != nil {
			t.Fatalf("failed to subscribe A: %v", err)
		}
		subsA = append(subsA, chA)

		chB, err := s.Subscribe(ctx, execIDB)
		if err != nil {
			t.Fatalf("failed to subscribe B: %v", err)
		}
		subsB = append(subsB, chB)

		chAll, err := s.SubscribeAll(ctx)
		if err != nil {
			t.Fatalf("failed to subscribe All: %v", err)
		}
		subsAll = append(subsAll, chAll)
	}

	var wg sync.WaitGroup
	var countA, countB, countAll int64

	// Consume goroutines for SubsA
	for _, ch := range subsA {
		wg.Add(1)
		go func(c <-chan *engine.Event) {
			defer wg.Done()
			for i := 0; i < numEventsPerType; i++ {
				select {
				case ev := <-c:
					if ev.ExecutionID() == execIDA {
						atomic.AddInt64(&countA, 1)
					}
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	// Consume goroutines for SubsB
	for _, ch := range subsB {
		wg.Add(1)
		go func(c <-chan *engine.Event) {
			defer wg.Done()
			for i := 0; i < numEventsPerType; i++ {
				select {
				case ev := <-c:
					if ev.ExecutionID() == execIDB {
						atomic.AddInt64(&countB, 1)
					}
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	// Consume goroutines for SubsAll
	for _, ch := range subsAll {
		wg.Add(1)
		go func(c <-chan *engine.Event) {
			defer wg.Done()
			for i := 0; i < numEventsPerType*2; i++ {
				select {
				case <-c:
					atomic.AddInt64(&countAll, 1)
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	// Concurrently publish events
	var pubWg sync.WaitGroup
	pubWg.Add(2)

	go func() {
		defer pubWg.Done()
		for i := 1; i <= numEventsPerType; i++ {
			ev, _ := engine.NewEvent(execIDA, uint64(i), time.Now(), engine.EventTypeNodeCompleted, "node1", engine.NodeCompletedPayload{})
			if err := s.PublishEvent(ctx, ev); err != nil {
				t.Errorf("publish error A: %v", err)
			}
		}
	}()

	go func() {
		defer pubWg.Done()
		for i := 1; i <= numEventsPerType; i++ {
			ev, _ := engine.NewEvent(execIDB, uint64(i), time.Now(), engine.EventTypeNodeCompleted, "node2", engine.NodeCompletedPayload{})
			if err := s.PublishEvent(ctx, ev); err != nil {
				t.Errorf("publish error B: %v", err)
			}
		}
	}()

	pubWg.Wait()
	wg.Wait()

	expectedA := int64(numSubscribersPerType * numEventsPerType)
	expectedB := int64(numSubscribersPerType * numEventsPerType)
	expectedAll := int64(numSubscribersPerType * numEventsPerType * 2)

	if atomic.LoadInt64(&countA) != expectedA {
		t.Errorf("expected %d events for A, got %d", expectedA, countA)
	}
	if atomic.LoadInt64(&countB) != expectedB {
		t.Errorf("expected %d events for B, got %d", expectedB, countB)
	}
	if atomic.LoadInt64(&countAll) != expectedAll {
		t.Errorf("expected %d events for All, got %d", expectedAll, countAll)
	}
}

func TestPubSub_SubscriberCancellation(t *testing.T) {
	s := memory.New()
	subCtx, cancel := context.WithCancel(context.Background())

	execID, _ := engine.NewExecutionID()
	subCh, err := s.Subscribe(subCtx, execID)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	if s.Stats().SubscriberCount != 1 {
		t.Errorf("expected 1 subscriber, got %d", s.Stats().SubscriberCount)
	}

	// Cancel the subscriber context
	cancel()

	// Wait for the channel to close
	select {
	case ev, ok := <-subCh:
		if ok {
			t.Fatalf("expected closed channel, got event: %v", ev)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for subscriber channel to close on context cancellation")
	}

	// Verify subscriber count updated
	// Allow brief moment for cleanup goroutine
	for i := 0; i < 20; i++ {
		if s.Stats().SubscriberCount == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Stats().SubscriberCount != 0 {
		t.Errorf("expected 0 subscribers after cancel, got %d", s.Stats().SubscriberCount)
	}

	// Publishing after unsubscribe should not panic or fail
	ev, _ := engine.NewEvent(execID, 1, time.Now(), engine.EventTypeNodeCompleted, "node1", engine.NodeCompletedPayload{})
	if err := s.PublishEvent(context.Background(), ev); err != nil {
		t.Fatalf("unexpected error publishing after cancel: %v", err)
	}
}

func TestPubSub_BufferHandlingAndDrop(t *testing.T) {
	// Create store with a tiny buffer of 2
	s := memory.New(memory.WithSubscriptionBufferSize(2))
	ctx := context.Background()

	execID, _ := engine.NewExecutionID()
	subCh, err := s.Subscribe(ctx, execID)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// Publish 6 events without reading from subCh
	for i := 1; i <= 6; i++ {
		ev, _ := engine.NewEvent(execID, uint64(i), time.Now(), engine.EventTypeNodeCompleted, "node1", engine.NodeCompletedPayload{})
		if err := s.PublishEvent(ctx, ev); err != nil {
			t.Fatalf("publish %d error: %v", i, err)
		}
	}

	// 2 events should be in buffer, 4 dropped
	stats := s.Stats()
	if stats.DroppedEvents != 4 {
		t.Errorf("expected 4 dropped events, got %d", stats.DroppedEvents)
	}

	// Read the 2 buffered events
	e1 := <-subCh
	e2 := <-subCh
	if e1.Sequence() != 1 || e2.Sequence() != 2 {
		t.Errorf("expected sequences 1 and 2, got %d and %d", e1.Sequence(), e2.Sequence())
	}

	// Channel should now be empty (non-blocking select)
	select {
	case extra := <-subCh:
		t.Fatalf("unexpected extra event in channel: %v", extra)
	default:
		// Expected: channel is empty
	}
}

func TestStore_Reset(t *testing.T) {
	s := memory.New()
	ctx := context.Background()
	execID, _ := engine.NewExecutionID()

	// 1. Add state
	node, _ := engine.NewNode("n1", "worker", nil)
	graph, _ := engine.NewGraph("g1", []*engine.Node{node}, nil)
	st, _ := engine.NewGraphState(execID, graph)
	if err := s.SetState(ctx, execID, st, 0); err != nil {
		t.Fatalf("SetState failed: %v", err)
	}

	// 2. Acquire lock
	handle, err := s.AcquireLock(ctx, "res1", 10*time.Second)
	if err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}

	// 3. Append log
	if err := s.AppendLog(ctx, &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    1,
		Timestamp:   time.Now(),
		Message:     "msg",
	}); err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}

	// 4. Subscribe
	ch, err := s.Subscribe(ctx, execID)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	statsBefore := s.Stats()
	if statsBefore.StateCount != 1 || statsBefore.ActiveLockCount != 1 || statsBefore.SubscriberCount != 1 || statsBefore.ExecutionLogCount != 1 {
		t.Errorf("unexpected stats before reset: %+v", statsBefore)
	}

	// Reset store
	s.Reset()

	statsAfter := s.Stats()
	if statsAfter.StateCount != 0 || statsAfter.ActiveLockCount != 0 || statsAfter.SubscriberCount != 0 || statsAfter.ExecutionLogCount != 0 {
		t.Errorf("unexpected stats after reset: %+v", statsAfter)
	}

	// Verify subscriber channel is closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel after reset")
		}
	default:
		t.Fatal("expected closed channel after reset")
	}

	// Verify GetState returns not found
	_, err = s.GetState(ctx, execID)
	if !engine.IsStoreNotFound(err) {
		t.Errorf("expected ErrStoreNotFound after reset, got %v", err)
	}

	// Re-acquiring previously held lock should succeed immediately
	handle2, err := s.AcquireLock(ctx, "res1", 10*time.Second)
	if err != nil {
		t.Fatalf("failed to acquire lock after reset: %v", err)
	}
	_ = s.ReleaseLock(ctx, handle2)
	_ = handle
}

func TestStore_LockExpiry(t *testing.T) {
	s := memory.New()
	ctx := context.Background()

	// 1. Acquire with short TTL
	handle, err := s.AcquireLock(ctx, "res-short", 30*time.Millisecond)
	if err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Release after expiry should fail
	if err := s.ReleaseLock(ctx, handle); err == nil {
		t.Error("expected error releasing expired lock, got nil")
	}

	// Extend after expiry should fail
	if err := s.ExtendLock(ctx, handle, 100*time.Millisecond); err == nil {
		t.Error("expected error extending expired lock, got nil")
	}

	// Re-acquire on expired resource should succeed
	handle2, err := s.AcquireLock(ctx, "res-short", 1*time.Second)
	if err != nil {
		t.Fatalf("failed to re-acquire expired lock: %v", err)
	}
	if err := s.ReleaseLock(ctx, handle2); err != nil {
		t.Errorf("failed to release new lock: %v", err)
	}
}

func TestStore_ExecutionLog_DeepImmutability(t *testing.T) {
	s := memory.New()
	ctx := context.Background()
	execID, _ := engine.NewExecutionID()

	details := map[string]any{
		"step": "step1",
		"meta": map[string]any{
			"version": 1,
		},
	}

	entry := &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    1,
		Timestamp:   time.Now(),
		Message:     "init",
		Details:     details,
	}

	if err := s.AppendLog(ctx, entry); err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}

	// Mutate caller map
	details["step"] = "corrupted"
	details["meta"].(map[string]any)["version"] = 999

	logs, err := s.GetLogs(ctx, execID, 1, 10)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Details["step"] != "step1" {
		t.Errorf("stored details corrupted by caller mutation: %v", logs[0].Details["step"])
	}
	if logs[0].Details["meta"].(map[string]any)["version"] != 1 {
		t.Errorf("nested details corrupted: %v", logs[0].Details["meta"])
	}

	// Mutate returned logs
	logs[0].Details["step"] = "corrupted_read"
	logs[0].Details["meta"].(map[string]any)["version"] = 888

	// Re-fetch and verify store was untouched
	logs2, err := s.GetLogs(ctx, execID, 1, 10)
	if err != nil {
		t.Fatalf("GetLogs 2 failed: %v", err)
	}
	if logs2[0].Details["step"] != "step1" {
		t.Errorf("stored details corrupted by read mutation: %v", logs2[0].Details["step"])
	}
	if logs2[0].Details["meta"].(map[string]any)["version"] != 1 {
		t.Errorf("nested details corrupted by read mutation: %v", logs2[0].Details["meta"])
	}
}

func TestStore_Closed(t *testing.T) {
	s := memory.New()
	ctx := context.Background()
	execID, _ := engine.NewExecutionID()

	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Repeated close is idempotent
	if err := s.Close(); err != nil {
		t.Errorf("second Close failed: %v", err)
	}

	// Operations should return StoreUnavailableError
	_, err := s.GetState(ctx, execID)
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on GetState, got %v", err)
	}

	err = s.SetState(ctx, execID, nil, 0)
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on SetState, got %v", err)
	}

	_, err = s.PatchState(ctx, execID, nil, 0)
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on PatchState, got %v", err)
	}

	_, err = s.AcquireLock(ctx, "res", 1*time.Second)
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on AcquireLock, got %v", err)
	}

	err = s.ReleaseLock(ctx, &engine.LockHandle{Resource: "res"})
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on ReleaseLock, got %v", err)
	}

	err = s.ExtendLock(ctx, &engine.LockHandle{Resource: "res"}, 1*time.Second)
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on ExtendLock, got %v", err)
	}

	err = s.PublishEvent(ctx, nil)
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on PublishEvent, got %v", err)
	}

	err = s.PublishBatch(ctx, []*engine.Event{})
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on PublishBatch, got %v", err)
	}

	_, err = s.Subscribe(ctx, execID)
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on Subscribe, got %v", err)
	}

	err = s.AppendLog(ctx, &engine.ExecutionLogEntry{ExecutionID: execID})
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on AppendLog, got %v", err)
	}

	_, err = s.GetLogs(ctx, execID, 1, 10)
	if !engine.IsStoreUnavailable(err) {
		t.Errorf("expected StoreUnavailableError on GetLogs, got %v", err)
	}
}

func TestStore_InputValidation(t *testing.T) {
	s := memory.New()
	ctx := context.Background()

	// Empty executionID
	if _, err := s.GetState(ctx, ""); err == nil {
		t.Error("expected error on empty executionID in GetState")
	}
	if err := s.SetState(ctx, "", nil, 0); err == nil {
		t.Error("expected error on empty executionID in SetState")
	}
	if _, err := s.PatchState(ctx, "", nil, 0); err == nil {
		t.Error("expected error on empty executionID in PatchState")
	}
	if err := s.AppendLog(ctx, nil); err == nil {
		t.Error("expected error on nil log entry in AppendLog")
	}
	if err := s.AppendLog(ctx, &engine.ExecutionLogEntry{ExecutionID: ""}); err == nil {
		t.Error("expected error on empty executionID in AppendLog")
	}
	if _, err := s.GetLogs(ctx, "", 1, 10); err == nil {
		t.Error("expected error on empty executionID in GetLogs")
	}

	// Lock manager validations
	if _, err := s.AcquireLock(ctx, "", 1*time.Second); err == nil {
		t.Error("expected error on empty resource in AcquireLock")
	}
	if _, err := s.AcquireLock(ctx, "res", 0); err == nil {
		t.Error("expected error on zero ttl in AcquireLock")
	}
	if err := s.ReleaseLock(ctx, nil); err == nil {
		t.Error("expected error on nil handle in ReleaseLock")
	}
	if err := s.ReleaseLock(ctx, &engine.LockHandle{Resource: ""}); err == nil {
		t.Error("expected error on empty resource in ReleaseLock")
	}
	if err := s.ExtendLock(ctx, nil, 1*time.Second); err == nil {
		t.Error("expected error on nil handle in ExtendLock")
	}
	if err := s.ExtendLock(ctx, &engine.LockHandle{Resource: "res"}, 0); err == nil {
		t.Error("expected error on zero ttl in ExtendLock")
	}

	// PublishBatch validation
	if err := s.PublishBatch(ctx, []*engine.Event{nil}); err == nil {
		t.Error("expected error on nil event in PublishBatch")
	}
	if err := s.PublishEvent(ctx, nil); err == nil {
		t.Error("expected error on nil event in PublishEvent")
	}

	// Context cancellation
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Subscribe(canceledCtx, "exec"); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled on Subscribe, got %v", err)
	}
	if err := s.PublishEvent(canceledCtx, &engine.Event{}); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled on PublishEvent, got %v", err)
	}
}
