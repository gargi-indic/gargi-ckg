package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/config"
	"github.com/gargi-indic/gargi-flow/internal/store"
	"github.com/gargi-indic/gargi-flow/internal/store/memory"
	"github.com/gargi-indic/gargi-flow/internal/store/storetest"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

func TestCompositeStore_Conformance(t *testing.T) {
	// Verify that composite store running on memory adapter satisfies the complete conformance suite.
	storetest.Run(t, func(t *testing.T) engine.StateStore {
		cfg := config.StoreConfig{
			Driver: "memory",
		}
		st, err := store.New(context.Background(), cfg)
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}
		t.Cleanup(func() {
			_ = st.Close()
		})
		return st
	})
}

func TestCompositeStore_SelectiveCapabilities(t *testing.T) {
	ctx := context.Background()

	t.Run("state only", func(t *testing.T) {
		memStore := memory.NewStore()
		st, err := store.NewComposite(memStore, nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := st.(engine.StateStore); !ok {
			t.Errorf("expected engine.StateStore implementation")
		}
		if _, ok := st.(engine.LockManager); ok {
			t.Errorf("expected engine.LockManager to NOT be implemented")
		}
		if _, ok := st.(engine.EventPublisher); ok {
			t.Errorf("expected engine.EventPublisher to NOT be implemented")
		}
		if _, ok := st.(engine.ExecutionLog); ok {
			t.Errorf("expected engine.ExecutionLog to NOT be implemented")
		}
		if st.Lock() != nil {
			t.Errorf("expected st.Lock() to be nil")
		}
		if st.Events() != nil {
			t.Errorf("expected st.Events() to be nil")
		}
		if st.Log() != nil {
			t.Errorf("expected st.Log() to be nil")
		}
	})

	t.Run("state and lock only", func(t *testing.T) {
		memStore := memory.NewStore()
		st, err := store.NewComposite(memStore, memStore, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := st.(engine.StateStore); !ok {
			t.Errorf("expected engine.StateStore implementation")
		}
		lm, ok := st.(engine.LockManager)
		if !ok {
			t.Errorf("expected engine.LockManager implementation")
		}
		if _, ok := st.(engine.EventPublisher); ok {
			t.Errorf("expected engine.EventPublisher to NOT be implemented")
		}
		if _, ok := st.(engine.ExecutionLog); ok {
			t.Errorf("expected engine.ExecutionLog to NOT be implemented")
		}

		// Test lock functionality
		handle, err := lm.AcquireLock(ctx, "res1", 5*time.Second)
		if err != nil {
			t.Fatalf("failed to acquire lock: %v", err)
		}
		if err := lm.ReleaseLock(ctx, handle); err != nil {
			t.Fatalf("failed to release lock: %v", err)
		}
	})

	t.Run("state and log only", func(t *testing.T) {
		memStore := memory.NewStore()
		st, err := store.NewComposite(memStore, nil, nil, memStore)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := st.(engine.StateStore); !ok {
			t.Errorf("expected engine.StateStore implementation")
		}
		if _, ok := st.(engine.LockManager); ok {
			t.Errorf("expected engine.LockManager to NOT be implemented")
		}
		if _, ok := st.(engine.EventPublisher); ok {
			t.Errorf("expected engine.EventPublisher to NOT be implemented")
		}
		el, ok := st.(engine.ExecutionLog)
		if !ok {
			t.Errorf("expected engine.ExecutionLog implementation")
		}

		entry := &engine.ExecutionLogEntry{
			ExecutionID: "exec-1",
			Sequence:    1,
			Timestamp:   time.Now(),
			Message:     "started",
		}
		if err := el.AppendLog(ctx, entry); err != nil {
			t.Fatalf("failed to append log: %v", err)
		}

		logs, err := el.GetLogs(ctx, "exec-1", 1, 10)
		if err != nil {
			t.Fatalf("failed to get logs: %v", err)
		}
		if len(logs) != 1 || logs[0].Message != "started" {
			t.Errorf("unexpected logs returned: %v", logs)
		}
	})

	t.Run("state and events only", func(t *testing.T) {
		memStore := memory.NewStore()
		st, err := store.NewComposite(memStore, nil, memStore, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := st.(engine.StateStore); !ok {
			t.Errorf("expected engine.StateStore implementation")
		}
		if _, ok := st.(engine.LockManager); ok {
			t.Errorf("expected engine.LockManager to NOT be implemented")
		}
		if _, ok := st.(engine.EventPublisher); !ok {
			t.Errorf("expected engine.EventPublisher implementation")
		}
		if _, ok := st.(engine.ExecutionLog); ok {
			t.Errorf("expected engine.ExecutionLog to NOT be implemented")
		}
	})

	t.Run("all capabilities", func(t *testing.T) {
		memStore := memory.NewStore()
		st, err := store.NewComposite(memStore, memStore, memStore, memStore)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := st.(engine.StateStore); !ok {
			t.Errorf("expected engine.StateStore implementation")
		}
		if _, ok := st.(engine.LockManager); !ok {
			t.Errorf("expected engine.LockManager implementation")
		}
		if _, ok := st.(engine.EventPublisher); !ok {
			t.Errorf("expected engine.EventPublisher implementation")
		}
		if _, ok := st.(engine.ExecutionLog); !ok {
			t.Errorf("expected engine.ExecutionLog implementation")
		}
	})
}

func TestBuilder_DI(t *testing.T) {
	memStore := memory.NewStore()
	closerCalled := false

	comp, err := store.NewBuilder().
		WithStateStore(memStore).
		WithLockManager(memStore).
		WithExecutionLog(memStore).
		WithCloser(func() error {
			closerCalled = true
			return nil
		}).
		Build()
	if err != nil {
		t.Fatalf("builder failed: %v", err)
	}

	if comp.State() != memStore {
		t.Errorf("expected state store to be memStore")
	}
	if comp.Lock() != memStore {
		t.Errorf("expected lock manager to be memStore")
	}
	if comp.Log() != memStore {
		t.Errorf("expected log to be memStore")
	}
	if comp.Events() != nil {
		t.Errorf("expected events to be nil")
	}

	if err := comp.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if !closerCalled {
		t.Errorf("expected custom closer to be invoked")
	}
}

func TestCompositeStore_CloseIdempotencyAndErrors(t *testing.T) {
	memStore := memory.NewStore()
	callCount := 0
	errDummy := errors.New("closer error")

	comp, err := store.NewComposite(memStore, nil, nil, nil, func() error {
		callCount++
		return errDummy
	})
	if err != nil {
		t.Fatalf("failed to create composite: %v", err)
	}

	// First close should return error
	err1 := comp.Close()
	if !errors.Is(err1, errDummy) {
		t.Errorf("expected errDummy, got %v", err1)
	}
	if callCount != 1 {
		t.Errorf("expected callCount 1, got %d", callCount)
	}

	// Second close should be a no-op
	err2 := comp.Close()
	if err2 != nil {
		t.Errorf("expected nil error on repeated close, got %v", err2)
	}
	if callCount != 1 {
		t.Errorf("expected callCount still 1, got %d", callCount)
	}
}

func TestFactory_NewFailFast(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid state driver", func(t *testing.T) {
		cfg := config.StoreConfig{
			Capabilities: config.StoreCapabilitiesConfig{
				State: "unknown_db",
			},
		}
		_, err := store.New(ctx, cfg)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("neo4j for lock fails fast", func(t *testing.T) {
		cfg := config.StoreConfig{
			Driver: "memory",
			Capabilities: config.StoreCapabilitiesConfig{
				Lock: "neo4j",
			},
		}
		_, err := store.New(ctx, cfg)
		if err == nil {
			t.Fatal("expected error for neo4j lock, got nil")
		}
	})

	t.Run("neo4j for events fails fast", func(t *testing.T) {
		cfg := config.StoreConfig{
			Driver: "memory",
			Capabilities: config.StoreCapabilitiesConfig{
				Events: "neo4j",
			},
		}
		_, err := store.New(ctx, cfg)
		if err == nil {
			t.Fatal("expected error for neo4j events, got nil")
		}
	})

	t.Run("missing redis addr fails fast", func(t *testing.T) {
		cfg := config.StoreConfig{
			Capabilities: config.StoreCapabilitiesConfig{
				State: "redis",
			},
		}
		_, err := store.New(ctx, cfg)
		if err == nil {
			t.Fatal("expected error for missing redis addr, got nil")
		}
	})

	t.Run("missing postgres dsn fails fast", func(t *testing.T) {
		cfg := config.StoreConfig{
			Capabilities: config.StoreCapabilitiesConfig{
				State: "postgres",
			},
		}
		_, err := store.New(ctx, cfg)
		if err == nil {
			t.Fatal("expected error for missing postgres dsn, got nil")
		}
	})

	t.Run("missing neo4j config fails fast", func(t *testing.T) {
		cfg := config.StoreConfig{
			Capabilities: config.StoreCapabilitiesConfig{
				State: "neo4j",
			},
		}
		_, err := store.New(ctx, cfg)
		if err == nil {
			t.Fatal("expected error for missing neo4j settings, got nil")
		}
	})
}

func TestCompositeStore_Experiments(t *testing.T) {
	ctx := context.Background()

	t.Run("NewComposite defaults to an in-memory experiment store", func(t *testing.T) {
		st, err := store.NewComposite(memory.NewStore(), nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := st.Experiments().(*memory.ExperimentStore); !ok {
			t.Fatalf("Experiments() = %T, want *memory.ExperimentStore", st.Experiments())
		}
	})

	t.Run("builder injects the experiment store", func(t *testing.T) {
		exps := memory.NewExperimentStore()
		st, err := store.NewBuilder().WithStateStore(memory.NewStore()).WithExperimentStore(exps).Build()
		if err != nil {
			t.Fatalf("builder failed: %v", err)
		}
		if st.Experiments() != exps {
			t.Fatal("Experiments() is not the injected store")
		}
		st2, err := store.NewBuilder().WithStateStore(memory.NewStore()).Build()
		if err != nil {
			t.Fatalf("builder failed: %v", err)
		}
		if st2.Experiments() == nil {
			t.Fatal("Experiments() is nil without an injected store")
		}
	})

	t.Run("factory selects memory by default", func(t *testing.T) {
		for _, cfg := range []config.StoreConfig{
			{},
			{Driver: "memory"},
			{Driver: "memory", Capabilities: config.StoreCapabilitiesConfig{Experiments: "memory"}},
		} {
			st, err := store.New(ctx, cfg)
			if err != nil {
				t.Fatalf("store.New(%+v): %v", cfg, err)
			}
			if _, ok := st.Experiments().(*memory.ExperimentStore); !ok {
				t.Fatalf("store.New(%+v).Experiments() = %T, want *memory.ExperimentStore", cfg, st.Experiments())
			}
			if _, err := st.Experiments().SaveModel(ctx, sim.ModelBundle{ID: "m"}); err != nil {
				t.Fatalf("SaveModel: %v", err)
			}
			_ = st.Close()
		}
	})

	t.Run("factory rejects unsupported experiment drivers", func(t *testing.T) {
		for driver, want := range map[string]string{
			"redis":     "redis does not support capability 'experiments' (allowed: memory, postgres)",
			"neo4j":     "neo4j does not support capability 'experiments' (allowed: memory, postgres)",
			"cassandra": "invalid store experiments capability driver: cassandra",
		} {
			_, err := store.New(ctx, config.StoreConfig{Driver: "memory", Capabilities: config.StoreCapabilitiesConfig{Experiments: driver}})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("experiments %q: error = %v, want %q", driver, err, want)
			}
		}
	})

	t.Run("factory postgres experiments require a dsn", func(t *testing.T) {
		_, err := store.New(ctx, config.StoreConfig{Driver: "memory", Capabilities: config.StoreCapabilitiesConfig{Experiments: "postgres"}})
		if err == nil || !strings.Contains(err.Error(), "postgres configuration requires dsn") {
			t.Fatalf("error = %v, want the missing dsn error", err)
		}
	})
}
