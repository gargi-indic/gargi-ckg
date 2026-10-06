package nodes_test

import (
	"context"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/nodes"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func TestBranchNode(t *testing.T) {
	reg := engine.NewRegistry()
	_ = nodes.RegisterDefaults(reg, nodes.Dependencies{})

	cases := []struct {
		name      string
		config    map[string]any
		inputs    map[string]any
		expResult bool
		expBranch string
		expErr    bool
	}{
		{
			name:      "truthy (default) - true",
			config:    map[string]any{"input": "val"},
			inputs:    map[string]any{"val": "hello"},
			expResult: true,
			expBranch: "true",
		},
		{
			name:      "truthy - false",
			config:    map[string]any{"input": "val", "operator": "truthy"},
			inputs:    map[string]any{"val": ""},
			expResult: false,
			expBranch: "false",
		},
		{
			name:      "exists - true",
			config:    map[string]any{"input": "val", "operator": "exists"},
			inputs:    map[string]any{"val": nil},
			expResult: true,
			expBranch: "true",
		},
		{
			name:      "exists - false",
			config:    map[string]any{"input": "val", "operator": "exists"},
			inputs:    map[string]any{},
			expResult: false,
			expBranch: "false",
		},
		{
			name:      "eq numeric matching types",
			config:    map[string]any{"input": "val", "operator": "eq", "value": float64(42)},
			inputs:    map[string]any{"val": float64(42)},
			expResult: true,
			expBranch: "true",
		},
		{
			name:      "eq numeric normalisation (int vs float)",
			config:    map[string]any{"input": "val", "operator": "eq", "value": float64(42)},
			inputs:    map[string]any{"val": int(42)},
			expResult: true,
			expBranch: "true",
		},
		{
			name:      "gt numeric",
			config:    map[string]any{"input": "val", "operator": "gt", "value": float64(10)},
			inputs:    map[string]any{"val": float64(15)},
			expResult: true,
			expBranch: "true",
		},
		{
			name:      "lt string",
			config:    map[string]any{"input": "val", "operator": "lt", "value": "c"},
			inputs:    map[string]any{"val": "a"},
			expResult: true,
			expBranch: "true",
		},
		{
			name:      "missing input for gt",
			config:    map[string]any{"input": "val", "operator": "gt", "value": float64(10)},
			inputs:    map[string]any{},
			expResult: false,
			expErr:    true,
		},
		{
			name:      "mismatched types",
			config:    map[string]any{"input": "val", "operator": "gt", "value": "10"},
			inputs:    map[string]any{"val": float64(15)},
			expResult: false,
			expErr:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := reg.Instantiate("conditional_branch", tc.config)
			if err != nil {
				t.Fatalf("failed to instantiate: %v", err)
			}

			g, _ := engine.NewGraph("test-graph", nil, nil)
			state, _ := engine.NewGraphState("exec1", g)

			nc := &fakeNodeContext{
				nodeID: "node1",
				inputs: tc.inputs,
				state:  state,
			}

			mut, err := node.Execute(context.Background(), nc)
			if tc.expErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if engine.ClassOf(err) != engine.ClassNonRetryable {
					t.Errorf("expected non-retryable error, got %v", engine.ClassOf(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			if mut == nil {
				t.Fatal("expected mutation, got nil")
			}

			res, ok := mut.Set["result"].(bool)
			if !ok || res != tc.expResult {
				t.Errorf("expected result %v, got %v", tc.expResult, mut.Set["result"])
			}

			branch, ok := mut.Set["branch"].(string)
			if !ok || branch != tc.expBranch {
				t.Errorf("expected branch %v, got %v", tc.expBranch, mut.Set["branch"])
			}
		})
	}

	t.Run("race condition on concurrent execution", func(t *testing.T) {
		node, err := reg.Instantiate("conditional_branch", map[string]any{"input": "val"})
		if err != nil {
			t.Fatalf("failed to instantiate: %v", err)
		}

		nc := &fakeNodeContext{
			nodeID: "node1",
			inputs: map[string]any{"val": "hello"},
			state:  nil, // also test that state can be nil
		}

		concurrency := 10
		errs := make(chan error, concurrency)

		for i := 0; i < concurrency; i++ {
			go func() {
				_, e := node.Execute(context.Background(), nc)
				errs <- e
			}()
		}

		for i := 0; i < concurrency; i++ {
			if e := <-errs; e != nil {
				t.Errorf("concurrent execution failed: %v", e)
			}
		}
	})
}
