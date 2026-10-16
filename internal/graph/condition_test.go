package graph_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/graph"
)

func TestCompileCondition_Success(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		nodeIDs []string
	}{
		{
			name: "upstream output check",
			expr: "upstream.output.score > 0.8",
		},
		{
			name: "upstream status check",
			expr: "upstream.status == 'succeeded'",
		},
		{
			name:    "specific node status check",
			expr:    "reserve_inventory.status == 'reserved'",
			nodeIDs: []string{"reserve_inventory"},
		},
		{
			name: "compound logic",
			expr: "upstream.status == 'succeeded' && (state.attempts < 3 || !state.aborted)",
		},
		{
			name: "in list",
			expr: "state.status in ['active', 'pending']",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := graph.CompileCondition(tt.expr, tt.nodeIDs...)
			if err != nil {
				t.Errorf("CompileCondition() error = %v", err)
			}
		})
	}
}

func TestCompileCondition_Errors(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		nodeIDs []string
		wantErr string
	}{
		{
			name:    "empty condition",
			expr:    "   ",
			wantErr: "condition cannot be empty",
		},
		{
			name:    "exceeds max length",
			expr:    "true || " + strings.Repeat("a", 4096),
			wantErr: "condition length exceeds maximum of 4096 runes",
		},
		{
			name:    "syntax error",
			expr:    "1 + 'bad'",
			wantErr: "failed to compile condition",
		},
		{
			name:    "static type mismatch",
			expr:    "'a' > 5",
			wantErr: "failed to compile condition",
		},
		{
			name:    "non-bool output (int)",
			expr:    "1 + 2",
			wantErr: "condition must evaluate to bool, got int",
		},
		{
			name:    "non-bool output (string)",
			expr:    "'str'",
			wantErr: "condition must evaluate to bool, got string",
		},
		{
			name:    "non-bool output (dyn)",
			expr:    "upstream.output",
			wantErr: "condition must evaluate to bool, got dyn",
		},
		{
			name:    "undeclared identifier",
			expr:    "unknown_node.status == 'ok'",
			wantErr: "undeclared reference to 'unknown_node'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := graph.CompileCondition(tt.expr, tt.nodeIDs...)
			if err == nil {
				t.Errorf("expected error, got nil")
			} else if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestEvaluateCondition(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		vars    map[string]any
		nodeIDs []string
		want    bool
		wantErr bool
	}{
		{
			name: "evaluates to true",
			expr: "upstream.output.score > 0.8",
			vars: map[string]any{
				"upstream": map[string]any{
					"output": map[string]any{
						"score": 0.9,
					},
				},
			},
			want: true,
		},
		{
			name: "evaluates to false",
			expr: "upstream.output.score > 0.8",
			vars: map[string]any{
				"upstream": map[string]any{
					"output": map[string]any{
						"score": 0.5,
					},
				},
			},
			want: false,
		},
		{
			name: "handles missing keys without error if not accessed",
			expr: "true",
			vars: map[string]any{},
			want: true,
		},
		{
			name:    "missing key error when accessed",
			expr:    "upstream.output.missing_field == 1",
			vars:    map[string]any{}, // upstream is populated as empty map by prepareVars, but output is missing
			wantErr: true,
		},
		{
			name: "node specific variable",
			expr: "n1.status == 'ok'",
			vars: map[string]any{
				"n1": map[string]any{"status": "ok"},
			},
			nodeIDs: []string{"n1"},
			want:    true,
		},
		{
			name: "cost limit enforcement (exceeds)",
			expr: "state.items.all(x, state.items.all(y, x != y || true))",
			vars: map[string]any{"state": map[string]any{
				"items": func() []int {
					items := make([]int, 200)
					for i := range items {
						items[i] = i
					}
					return items
				}(),
			}},
			wantErr: true,
		},
		{
			name: "nil vars handling",
			expr: "true",
			vars: nil,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := graph.EvaluateCondition(tt.expr, tt.vars, tt.nodeIDs...)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateCondition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompiledCondition_ConcurrentEvaluation(t *testing.T) {
	c, err := graph.CompileCondition("upstream.val > 0")
	if err != nil {
		t.Fatalf("CompileCondition failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			vars := map[string]any{
				"upstream": map[string]any{
					"val": val,
				},
			}
			res, err := c.Evaluate(vars)
			if err != nil {
				t.Errorf("Evaluate failed: %v", err)
			}
			expected := val > 0
			if res != expected {
				t.Errorf("Evaluate = %v, want %v", res, expected)
			}
		}(i - 50)
	}
	wg.Wait()
}

func TestCompiledCondition_Determinism(t *testing.T) {
	c, err := graph.CompileCondition("upstream.val == 42")
	if err != nil {
		t.Fatalf("CompileCondition failed: %v", err)
	}

	vars := map[string]any{
		"upstream": map[string]any{
			"val": 42,
		},
	}

	// Multiple sequential evaluations should yield exactly same result with no side effects
	for i := 0; i < 10; i++ {
		res, err := c.Evaluate(vars)
		if err != nil {
			t.Fatalf("Evaluate failed on iteration %d: %v", i, err)
		}
		if !res {
			t.Errorf("expected true on iteration %d, got false", i)
		}
	}
}

func TestCompiledCondition_NilReceiver(t *testing.T) {
	var c *graph.CompiledCondition
	_, err := c.Evaluate(nil)
	if err == nil || !strings.Contains(err.Error(), "nil compiled condition") {
		t.Errorf("expected error containing 'nil compiled condition', got %v", err)
	}
}
