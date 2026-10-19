package store_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/gargi-indic/gargi-ckg/internal/store"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func BenchmarkStore_Memory(b *testing.B) {
	ctx := context.Background()
	cfg := config.StoreConfig{Driver: "memory"}
	st, err := store.New(ctx, cfg)
	if err != nil {
		b.Fatalf("failed to create memory store: %v", err)
	}
	defer st.Close()

	n1, _ := engine.NewNode("node1", "test", nil)
	g, _ := engine.NewGraph("bench_graph", []*engine.Node{n1}, nil)
	execID, _ := engine.NewExecutionID()
	initialState, _ := engine.NewGraphState(execID, g)

	b.Run("SetState", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			id, _ := engine.NewExecutionID()
			s, _ := engine.NewGraphState(id, g)
			if err := st.SetState(ctx, id, s, 0); err != nil {
				b.Fatalf("SetState failed: %v", err)
			}
		}
	})

	b.Run("GetState", func(b *testing.B) {
		_ = st.SetState(ctx, execID, initialState, 0)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := st.GetState(ctx, execID)
			if err != nil {
				b.Fatalf("GetState failed: %v", err)
			}
		}
	})

	b.Run("PatchState", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			id, _ := engine.NewExecutionID()
			s, _ := engine.NewGraphState(id, g)
			if err := st.SetState(ctx, id, s, 0); err != nil {
				b.Fatalf("SetState failed: %v", err)
			}
			mut := engine.StateMutation{
				NodeID:          "node1",
				ExpectedVersion: 0,
				Set:             map[string]any{"iteration": i},
			}
			_, err := st.PatchState(ctx, id, &mut, 0)
			if err != nil {
				b.Fatalf("PatchState failed: %v", err)
			}
		}
	})
}

func BenchmarkStore_Redis(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	ctx := context.Background()
	cfg := config.StoreConfig{
		Driver: "redis",
		Redis:  &config.RedisConfig{Addr: mr.Addr()},
	}
	st, err := store.New(ctx, cfg)
	if err != nil {
		b.Fatalf("failed to create redis store: %v", err)
	}
	defer st.Close()

	n1, _ := engine.NewNode("node1", "test", nil)
	g, _ := engine.NewGraph("bench_graph_redis", []*engine.Node{n1}, nil)
	execID, _ := engine.NewExecutionID()
	initialState, _ := engine.NewGraphState(execID, g)

	b.Run("SetState", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			id, _ := engine.NewExecutionID()
			s, _ := engine.NewGraphState(id, g)
			if err := st.SetState(ctx, id, s, 0); err != nil {
				b.Fatalf("SetState failed: %v", err)
			}
		}
	})

	b.Run("GetState", func(b *testing.B) {
		_ = st.SetState(ctx, execID, initialState, 0)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := st.GetState(ctx, execID)
			if err != nil {
				b.Fatalf("GetState failed: %v", err)
			}
		}
	})

	b.Run("PatchState", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			id, _ := engine.NewExecutionID()
			s, _ := engine.NewGraphState(id, g)
			if err := st.SetState(ctx, id, s, 0); err != nil {
				b.Fatalf("SetState failed: %v", err)
			}
			mut := engine.StateMutation{
				NodeID:          "node1",
				ExpectedVersion: 0,
				Set:             map[string]any{"iteration": i},
			}
			_, err := st.PatchState(ctx, id, &mut, 0)
			if err != nil {
				b.Fatalf("PatchState failed: %v", err)
			}
		}
	})
}
