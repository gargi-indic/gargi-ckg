package nodes_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/nodes"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func TestTransformNode(t *testing.T) {
	reg := engine.NewRegistry()
	_ = nodes.RegisterDefaults(reg, nodes.Dependencies{})

	t.Run("missing input key", func(t *testing.T) {
		node, err := reg.Instantiate("transform", map[string]any{
			"mapping": map[string]string{
				"out1": "in1",
			},
		})
		if err != nil {
			t.Fatalf("failed to instantiate: %v", err)
		}

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			inputs: map[string]any{}, // missing "in1"
			state:  state,
		}

		mut, err := node.Execute(context.Background(), nc)
		if err == nil {
			t.Fatal("expected error for missing input key")
		}

		if engine.ClassOf(err) != engine.ClassNonRetryable {
			t.Errorf("expected non-retryable error, got %v", engine.ClassOf(err))
		}

		if mut != nil {
			t.Errorf("expected nil mutation, got %v", mut)
		}
	})

	t.Run("conflict in configuration", func(t *testing.T) {
		_, err := reg.Instantiate("transform", map[string]any{
			"mapping": map[string]string{
				"conflict_key": "in1",
			},
			"set": map[string]any{
				"conflict_key": "static_val",
			},
		})
		if err == nil {
			t.Fatal("expected instantiation to fail due to conflicting keys")
		}
	})

	t.Run("empty configuration", func(t *testing.T) {
		_, err := reg.Instantiate("transform", map[string]any{})
		if err == nil {
			t.Fatal("expected instantiation to fail due to empty mapping and set")
		}
	})

	t.Run("successful transform", func(t *testing.T) {
		node, err := reg.Instantiate("transform", map[string]any{
			"mapping": map[string]string{
				"out1": "in1",
				"out2": "in2",
			},
			"set": map[string]any{
				"static1": "val1",
				"static2": []any{"val2"},
			},
		})
		if err != nil {
			t.Fatalf("failed to instantiate: %v", err)
		}

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			inputs: map[string]any{
				"in1": "dynamic_val1",
				"in2": 42,
			},
			state: state,
		}

		mut, err := node.Execute(context.Background(), nc)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		expected := map[string]any{
			"out1":    "dynamic_val1",
			"out2":    42,
			"static1": "val1",
			"static2": []any{"val2"},
		}

		if !reflect.DeepEqual(mut.Set, expected) {
			t.Errorf("expected %v, got %v", expected, mut.Set)
		}
	})
}
