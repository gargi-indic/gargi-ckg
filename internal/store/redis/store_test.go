package redis_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisstore "github.com/gargi-indic/gargi-ckg/internal/store/redis"
	"github.com/gargi-indic/gargi-ckg/internal/store/storetest"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"github.com/stretchr/testify/require"
)

func getRedisAddr(t *testing.T) (string, func()) {
	t.Helper()
	if addr := os.Getenv("GARGI_REDIS_ADDR"); addr != "" {
		return addr, func() {}
	}
	mr := miniredis.RunT(t)
	return mr.Addr(), func() {
		mr.Close()
	}
}

func TestConformance(t *testing.T) {
	addr, cleanup := getRedisAddr(t)
	defer cleanup()

	storetest.Run(t, storetest.Config{
		NewStore: func(t *testing.T) engine.StateStore {
			prefix, err := engine.NewExecutionID()
			require.NoError(t, err)
			s, err := redisstore.New(redisstore.Config{
				Addr:      addr,
				KeyPrefix: string(prefix),
			})
			require.NoError(t, err)
			return s
		},
		Teardown: func(t *testing.T, s engine.StateStore) {
			if r, ok := s.(*redisstore.Store); ok {
				_ = r.Close()
			}
		},
	})
}

func TestStore_ConstructorAliases(t *testing.T) {
	addr, cleanup := getRedisAddr(t)
	defer cleanup()

	s1, err := redisstore.New(redisstore.Config{Addr: addr})
	require.NoError(t, err)
	require.NotNil(t, s1)
	defer s1.Close()

	s2, err := redisstore.NewStore(redisstore.Config{Addr: addr})
	require.NoError(t, err)
	require.NotNil(t, s2)
	defer s2.Close()

	require.NotNil(t, s1.Client())
}

func TestPubSub_MessagingAndFanout(t *testing.T) {
	addr, cleanup := getRedisAddr(t)
	defer cleanup()

	s, err := redisstore.New(redisstore.Config{Addr: addr})
	require.NoError(t, err)
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	execID1, err := engine.NewExecutionID()
	require.NoError(t, err)
	execID2, err := engine.NewExecutionID()
	require.NoError(t, err)

	// Subscribe to execID1
	ch1, err := s.Subscribe(ctx, execID1)
	require.NoError(t, err)

	// Subscribe to all
	chAll, err := s.SubscribeAll(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	ev1, err := engine.NewEvent(execID1, 1, time.Now(), engine.EventTypeNodeStateChanged, "nodeA", engine.NodeStateChangedPayload{
		OldStatus: engine.StatusPending,
		NewStatus: engine.StatusRunning,
		Version:   1,
	})
	require.NoError(t, err)

	err = s.PublishEvent(ctx, ev1)
	require.NoError(t, err)

	// Verify ch1 gets the event
	select {
	case received := <-ch1:
		require.Equal(t, ev1.ExecutionID(), received.ExecutionID())
		require.Equal(t, ev1.Type(), received.Type())
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event on ch1")
	}

	// Verify chAll gets the event
	select {
	case received := <-chAll:
		require.Equal(t, ev1.ExecutionID(), received.ExecutionID())
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event on chAll")
	}

	// Publish for execID2
	ev2, err := engine.NewEvent(execID2, 1, time.Now(), engine.EventTypeNodeStateChanged, "nodeB", engine.NodeStateChangedPayload{
		OldStatus: engine.StatusPending,
		NewStatus: engine.StatusRunning,
		Version:   1,
	})
	require.NoError(t, err)

	err = s.PublishEvent(ctx, ev2)
	require.NoError(t, err)

	// ch1 should NOT receive ev2
	select {
	case received := <-ch1:
		t.Fatalf("unexpected event on ch1: %+v", received)
	case <-time.After(100 * time.Millisecond):
		// Expected
	}

	// chAll SHOULD receive ev2
	select {
	case received := <-chAll:
		require.Equal(t, execID2, received.ExecutionID())
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event 2 on chAll")
	}
}

func TestStore_LockExpiryAndSafeRelease(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	s, err := redisstore.New(redisstore.Config{Addr: mr.Addr()})
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()

	// 1. Acquire with short TTL
	handle, err := s.AcquireLock(ctx, "res-short", 100*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, handle)

	// Fast-forward miniredis clock
	mr.FastForward(150 * time.Millisecond)

	// Release after expiry should fail
	err = s.ReleaseLock(ctx, handle)
	require.Error(t, err)

	// Extend after expiry should fail
	err = s.ExtendLock(ctx, handle, 100*time.Millisecond)
	require.Error(t, err)

	// Re-acquire on expired resource should succeed
	handle2, err := s.AcquireLock(ctx, "res-short", 1*time.Second)
	require.NoError(t, err)

	// Attempt release with old handle should fail
	err = s.ReleaseLock(ctx, handle)
	require.Error(t, err)

	// Release with valid handle succeeds
	err = s.ReleaseLock(ctx, handle2)
	require.NoError(t, err)
}

func TestStore_UnavailableWhenClosed(t *testing.T) {
	addr, cleanup := getRedisAddr(t)
	defer cleanup()

	s, err := redisstore.New(redisstore.Config{Addr: addr})
	require.NoError(t, err)

	ctx := context.Background()
	execID, _ := engine.NewExecutionID()

	require.NoError(t, s.Close())

	// Idempotent close
	require.NoError(t, s.Close())

	// Operations should return StoreUnavailableError
	_, err = s.GetState(ctx, execID)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	err = s.SetState(ctx, execID, nil, 0)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	_, err = s.PatchState(ctx, execID, nil, 0)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	_, err = s.AcquireLock(ctx, "res", 1*time.Second)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	err = s.ReleaseLock(ctx, &engine.LockHandle{Resource: "res"})
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	err = s.ExtendLock(ctx, &engine.LockHandle{Resource: "res"}, 1*time.Second)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	err = s.PublishEvent(ctx, nil)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	err = s.PublishBatch(ctx, []*engine.Event{})
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	_, err = s.Subscribe(ctx, execID)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	err = s.AppendLog(ctx, &engine.ExecutionLogEntry{ExecutionID: execID})
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)

	_, err = s.GetLogs(ctx, execID, 1, 10)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError, got %v", err)
}

func TestStore_UnavailableOnNetworkFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	s, err := redisstore.New(redisstore.Config{Addr: mr.Addr()})
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	execID, _ := engine.NewExecutionID()

	// Shutdown miniredis to simulate network failure
	mr.Close()

	_, err = s.GetState(ctx, execID)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError on network failure, got: %v", err)

	_, err = s.AcquireLock(ctx, "res", 1*time.Second)
	require.True(t, engine.IsStoreUnavailable(err), "expected StoreUnavailableError on AcquireLock network failure, got: %v", err)
}

func TestStore_ExecutionLog_OffsetsAndLimits(t *testing.T) {
	addr, cleanup := getRedisAddr(t)
	defer cleanup()

	s, err := redisstore.New(redisstore.Config{Addr: addr})
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	execID, err := engine.NewExecutionID()
	require.NoError(t, err)

	// Append 10 log entries with sequence 1..10
	for seq := uint64(1); seq <= 10; seq++ {
		entry := &engine.ExecutionLogEntry{
			ExecutionID: execID,
			Sequence:    seq,
			Timestamp:   time.Now(),
			Message:     "msg",
			Details:     map[string]any{"seq": seq},
		}
		require.NoError(t, s.AppendLog(ctx, entry))
	}

	// Query with fromSequence = 4, limit = 3
	logs, err := s.GetLogs(ctx, execID, 4, 3)
	require.NoError(t, err)
	require.Len(t, logs, 3)
	require.Equal(t, uint64(4), logs[0].Sequence)
	require.Equal(t, uint64(5), logs[1].Sequence)
	require.Equal(t, uint64(6), logs[2].Sequence)

	// Query with limit <= 0 returns all from sequence
	allLogs, err := s.GetLogs(ctx, execID, 8, 0)
	require.NoError(t, err)
	require.Len(t, allLogs, 3)
	require.Equal(t, uint64(8), allLogs[0].Sequence)
	require.Equal(t, uint64(9), allLogs[1].Sequence)
	require.Equal(t, uint64(10), allLogs[2].Sequence)
}

func TestStore_Reset(t *testing.T) {
	addr, cleanup := getRedisAddr(t)
	defer cleanup()

	s, err := redisstore.New(redisstore.Config{Addr: addr})
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	execID, _ := engine.NewExecutionID()

	// Append a log and acquire a lock
	err = s.AppendLog(ctx, &engine.ExecutionLogEntry{ExecutionID: execID, Sequence: 1, Message: "test"})
	require.NoError(t, err)
	handle, err := s.AcquireLock(ctx, "res1", 10*time.Second)
	require.NoError(t, err)
	require.NotNil(t, handle)

	// Reset
	require.NoError(t, s.Reset(ctx))

	// Verify logs are empty
	logs, err := s.GetLogs(ctx, execID, 1, 10)
	require.NoError(t, err)
	require.Empty(t, logs)

	// Verify lock is cleared and can be re-acquired
	handle2, err := s.AcquireLock(ctx, "res1", 10*time.Second)
	require.NoError(t, err)
	require.NotNil(t, handle2)
	_ = s.ReleaseLock(ctx, handle2)
}

func TestStore_InputValidation(t *testing.T) {
	addr, cleanup := getRedisAddr(t)
	defer cleanup()

	s, err := redisstore.New(redisstore.Config{Addr: addr})
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()

	// Empty executionID
	_, err = s.GetState(ctx, "")
	require.Error(t, err)
	err = s.SetState(ctx, "", nil, 0)
	require.Error(t, err)
	_, err = s.PatchState(ctx, "", nil, 0)
	require.Error(t, err)
	err = s.AppendLog(ctx, nil)
	require.Error(t, err)
	err = s.AppendLog(ctx, &engine.ExecutionLogEntry{ExecutionID: ""})
	require.Error(t, err)
	_, err = s.GetLogs(ctx, "", 1, 10)
	require.Error(t, err)

	// Lock manager validations
	_, err = s.AcquireLock(ctx, "", 1*time.Second)
	require.Error(t, err)
	_, err = s.AcquireLock(ctx, "res", 0)
	require.Error(t, err)
	err = s.ReleaseLock(ctx, nil)
	require.Error(t, err)
	err = s.ReleaseLock(ctx, &engine.LockHandle{Resource: ""})
	require.Error(t, err)
	err = s.ExtendLock(ctx, nil, 1*time.Second)
	require.Error(t, err)
	err = s.ExtendLock(ctx, &engine.LockHandle{Resource: "res"}, 0)
	require.Error(t, err)

	// Event publisher validations
	err = s.PublishEvent(ctx, nil)
	require.Error(t, err)
	err = s.PublishBatch(ctx, []*engine.Event{nil})
	require.Error(t, err)

	// Context cancellation
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = s.Subscribe(canceledCtx, "exec")
	require.True(t, errors.Is(err, context.Canceled))
	err = s.PublishEvent(canceledCtx, &engine.Event{})
	require.True(t, errors.Is(err, context.Canceled))
}
