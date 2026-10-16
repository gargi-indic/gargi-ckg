package reactive_test

import (
	"context"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/reactive"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func TestApplyDataMapping(t *testing.T) {
	upstreamOutput := map[string]any{
		"score": 0.85,
		"user": map[string]any{
			"id": "123",
			"address": map[string]any{
				"city": "NY",
			},
		},
	}

	tests := []struct {
		name         string
		mapping      map[string]string
		targetInputs map[string]any
		wantInputs   map[string]any
		wantErr      bool
	}{
		{
			name:         "empty mapping",
			mapping:      map[string]string{},
			targetInputs: make(map[string]any),
			wantInputs:   make(map[string]any),
			wantErr:      false,
		},
		{
			name: "simple key mapping",
			mapping: map[string]string{
				"threshold": "score",
			},
			targetInputs: make(map[string]any),
			wantInputs: map[string]any{
				"threshold": 0.85,
			},
			wantErr: false,
		},
		{
			name: "nested path mapping",
			mapping: map[string]string{
				"userId":   "user.id",
				"userCity": "user.address.city",
			},
			targetInputs: make(map[string]any),
			wantInputs: map[string]any{
				"userId":   "123",
				"userCity": "NY",
			},
			wantErr: false,
		},
		{
			name: "invalid path mapping",
			mapping: map[string]string{
				"missing": "user.missing",
			},
			targetInputs: make(map[string]any),
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reactive.ApplyDataMapping(tt.mapping, upstreamOutput, tt.targetInputs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ApplyDataMapping() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				for k, v := range tt.wantInputs {
					if tt.targetInputs[k] != v {
						t.Errorf("ApplyDataMapping() targetInputs[%s] = %v, want %v", k, tt.targetInputs[k], v)
					}
				}
			}
		})
	}
}

func TestCELConditionEvaluator_Evaluate(t *testing.T) {
	evaluator, _ := reactive.NewCELConditionEvaluator()

	upstreamOutput := map[string]any{
		"score": 0.9,
	}

	tests := []struct {
		name       string
		expression string
		want       bool
		wantErr    bool
	}{
		{
			name:       "empty condition",
			expression: "  ",
			want:       true,
			wantErr:    false,
		},
		{
			name:       "simple condition",
			expression: "upstream.output.score > 0.8",
			want:       true,
			wantErr:    false,
		},
		{
			name:       "failing condition",
			expression: "upstream.output.score < 0.8",
			want:       false,
			wantErr:    false,
		},
		{
			name:       "invalid condition",
			expression: "upstream.output.missing > 0.8",
			want:       false,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.Evaluate(context.Background(), tt.expression, upstreamOutput, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Evaluate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPreconditionResolver_ResolveNodePreconditions(t *testing.T) {
	n1, _ := engine.NewNode("n1", "test", nil)
	n2, _ := engine.NewNodeWithJoinMode("n2", "test", nil, engine.JoinModeAllOf)
	n3, _ := engine.NewNodeWithJoinMode("n3", "test", nil, engine.JoinModeAnyOf)

	e12, _ := engine.NewEdge("n1", "n2", "", map[string]string{"input1": "output1"})
	e13, _ := engine.NewEdge("n1", "n3", "", map[string]string{"input1": "output1"})

	g1, _ := engine.NewGraph("g1", []*engine.Node{n1, n2}, []*engine.Edge{e12})
	g2, _ := engine.NewGraph("g2", []*engine.Node{n1, n3}, []*engine.Edge{e13})

	execID, _ := engine.NewExecutionID()

	state1, _ := engine.NewGraphState(execID, g1)
	mutation01 := engine.StateMutation{
		NodeID:          "n1",
		ExpectedVersion: 0,
		Status:          statusPtr(engine.StatusReady),
	}
	mutation11 := engine.StateMutation{
		NodeID:          "n1",
		ExpectedVersion: 1,
		Status:          statusPtr(engine.StatusRunning),
	}
	mutation21 := engine.StateMutation{
		NodeID:          "n1",
		ExpectedVersion: 2,
		Status:          statusPtr(engine.StatusSucceeded),
		Set:             map[string]any{"output1": "val1"},
	}
	state1, err := state1.Apply(mutation01)
	if err != nil {
		t.Fatalf("Failed to apply mutation01: %v", err)
	}
	state1, err = state1.Apply(mutation11)
	if err != nil {
		t.Fatalf("Failed to apply mutation11: %v", err)
	}
	state1, err = state1.Apply(mutation21)
	if err != nil {
		t.Fatalf("Failed to apply mutation21: %v", err)
	}

	state2, _ := engine.NewGraphState(execID, g2)
	state2, _ = state2.Apply(mutation01)
	state2, _ = state2.Apply(mutation11)
	state2, _ = state2.Apply(mutation21)

	evaluator, _ := reactive.NewCELConditionEvaluator()
	resolver := reactive.NewPreconditionResolver(evaluator)

	t.Run("JoinModeAllOf - all satisfied", func(t *testing.T) {
		res, err := resolver.ResolveNodePreconditions(context.Background(), "n2", g1, state1)
		if err != nil {
			t.Errorf("ResolveNodePreconditions() error = %v", err)
			return
		}
		if !res.Ready {
			t.Errorf("expected node to be ready")
		}
		if res.Inputs["input1"] != "val1" {
			t.Errorf("expected inputs to contain 'val1', got %v", res.Inputs["input1"])
		}
	})

	t.Run("JoinModeAnyOf - any satisfied", func(t *testing.T) {
		res, err := resolver.ResolveNodePreconditions(context.Background(), "n3", g2, state2)
		if err != nil {
			t.Errorf("ResolveNodePreconditions() error = %v", err)
			return
		}
		if !res.Ready {
			t.Errorf("expected node to be ready")
		}
		if res.Inputs["input1"] != "val1" {
			t.Errorf("expected inputs to contain 'val1', got %v", res.Inputs["input1"])
		}
	})
}

func statusPtr(s engine.Status) *engine.Status {
	return &s
}
