package nodes_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/nodes"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func TestWaitSignalNode(t *testing.T) {
	fakeSignals := &fakeSignalSource{}
	deps := nodes.Dependencies{
		Signals: fakeSignals,
	}
	reg := engine.NewRegistry()
	_ = nodes.RegisterDefaults(reg, deps)

	t.Run("successful signal", func(t *testing.T) {
		blockCh := make(chan struct{})
		fakeSignals.waitFunc = func(_ context.Context, _ engine.ExecutionID, _ engine.NodeID, _ string) (map[string]any, error) {
			<-blockCh
			return map[string]any{"data": "value"}, nil
		}

		node, _ := reg.Instantiate("wait_for_signal", map[string]any{
			"signal": "sig1",
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		errCh := make(chan error, 1)
		mutCh := make(chan *engine.StateMutation, 1)

		go func() {
			mut, err := node.Execute(context.Background(), nc)
			errCh <- err
			mutCh <- mut
		}()

		// Ensure it blocks
		select {
		case <-mutCh:
			t.Fatal("execute should block until signal arrives")
		case <-time.After(5 * time.Millisecond):
			// Unblock it
			close(blockCh)
		}

		execErr := <-errCh
		if execErr != nil {
			t.Fatalf("Execute failed: %v", execErr)
		}

		mut := <-mutCh
		if mut.Set["signal"] != "sig1" {
			t.Errorf("expected signal sig1, got %v", mut.Set["signal"])
		}

		payload, ok := mut.Set["payload"].(map[string]any)
		if !ok || payload["data"] != "value" {
			t.Errorf("expected payload with data=value, got %v", mut.Set["payload"])
		}
	})

	t.Run("node timeout", func(t *testing.T) {
		fakeSignals.waitFunc = func(ctx context.Context, _ engine.ExecutionID, _ engine.NodeID, _ string) (map[string]any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}

		node, _ := reg.Instantiate("wait_for_signal", map[string]any{
			"signal":          "sig1",
			"timeout_seconds": float64(1), // test uses 1s
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		ctx := context.Background() // Do NOT cancel this parent context

		// But we want to simulate the 1s timeout without waiting 1s.
		// Since we cannot easily fake time inside context.WithTimeout in stdlib,
		// we'll just wait 1s. We must test the node's timeout.
		_, err := node.Execute(ctx, nc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if engine.ClassOf(err) != engine.ClassNonRetryable {
			t.Errorf("expected non-retryable error, got %v", engine.ClassOf(err))
		}
		if err.Error() != "non-retryable error: wait_for_signal timeout exceeded" && err.Error() != "wait_for_signal timeout exceeded" {
			t.Errorf("expected timeout message, got %v", err)
		}
	})

	t.Run("parent cancel", func(t *testing.T) {
		fakeSignals.waitFunc = func(_ context.Context, _ engine.ExecutionID, _ engine.NodeID, _ string) (map[string]any, error) {
			return nil, context.Canceled
		}

		node, _ := reg.Instantiate("wait_for_signal", map[string]any{
			"signal": "sig1",
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := node.Execute(ctx, nc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected unwrapped context.Canceled, got %v", err)
		}
	})
}
