package graph

import (
	"errors"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

type mockConfig struct {
	Count int    `json:"count"`
	Name  string `json:"name"`
}

func (m *mockConfig) Validate() error {
	if m.Count < 0 {
		return errors.New("count cannot be negative")
	}
	return nil
}

func createTestRegistry(t *testing.T, countCalls *int) *engine.Registry {
	r := engine.NewRegistry()

	factory := func(_ any) (engine.NodeInterface, error) {
		*countCalls++
		return nil, errors.New("should never be called")
	}

	if err := r.Register("valid_type", factory, &mockConfig{}); err != nil {
		t.Fatalf("failed to register valid_type: %v", err)
	}

	if err := r.Register("another_type", factory, nil); err != nil {
		t.Fatalf("failed to register another_type: %v", err)
	}

	// For schema test
	schemaFactory := func(_ any) (engine.NodeInterface, error) {
		*countCalls++
		return nil, errors.New("should never be called")
	}
	_ = r.Register("t1", schemaFactory, nil)
	_ = r.Register("t2", schemaFactory, nil)

	return r
}

func TestValidateSemantics(t *testing.T) {
	var factoryCalls int
	reg := createTestRegistry(t, &factoryCalls)

	tests := []struct {
		name       string
		def        *engine.GraphDefinition
		reg        *engine.Registry
		wantIssues map[string]engine.ErrorCode // path -> code
	}{
		{
			name: "nil definition",
			def:  nil,
			reg:  reg,
			wantIssues: map[string]engine.ErrorCode{
				"$": engine.CodeInvalidType,
			},
		},
		{
			name: "nil registry",
			def:  &engine.GraphDefinition{},
			reg:  nil,
			wantIssues: map[string]engine.ErrorCode{
				"$": engine.CodeInvalidValue,
			},
		},
		{
			name: "valid semantics all around",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{
						ID:   "n1",
						Type: "valid_type",
						Config: map[string]any{
							"count": float64(10), // parsed from JSON
							"name":  "Alice",
						},
						RetryPolicy:  "rp1",
						Compensation: "c1",
					},
					{
						ID:   "n2",
						Type: "another_type",
					},
				},
				RetryPolicies: map[string]engine.RetryPolicy{
					"rp1": {}, // used
					"rp2": {}, // unused, perfectly fine
				},
				Compensations: map[string]engine.CompensationDefinition{
					"c1": {
						Type: "valid_type",
						Config: map[string]any{
							"count": float64(5),
						},
					},
					"c2": { // unused, perfectly fine
						Type: "another_type",
					},
				},
			},
			reg:        reg,
			wantIssues: nil,
		},
		{
			name: "unknown node types and configs",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{
						ID:   "n1",
						Type: "unknown_type",
					},
					{
						ID:   "n2",
						Type: "valid_type",
						Config: map[string]any{
							"count": float64(-1), // fails validation
						},
					},
					{
						ID:   "n3",
						Type: "valid_type",
						Config: map[string]any{
							"count": []any{}, // wrong JSON type
						},
					},
					{
						ID:   "n4",
						Type: "", // skipped by semantics validation (handled by structural validation)
					},
				},
				Compensations: map[string]engine.CompensationDefinition{
					"c1": {
						Type: "unknown_type",
					},
					"c2": {
						Type: "valid_type",
						Config: map[string]any{
							"extra": "bad", // unknown field
						},
					},
					"c3": {
						Type: "", // skipped
					},
				},
			},
			reg: reg,
			wantIssues: map[string]engine.ErrorCode{
				"$.nodes[0].type":           engine.CodeUnknownNodeType,
				"$.nodes[1].config":         engine.CodeInvalidNodeConfig,
				"$.nodes[2].config":         engine.CodeInvalidNodeConfig,
				"$.compensations.c1.type":   engine.CodeUnknownNodeType,
				"$.compensations.c2.config": engine.CodeInvalidNodeConfig,
			},
		},
		{
			name: "unresolved references",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{
						ID:           "n1",
						Type:         "another_type",
						RetryPolicy:  "missing_rp",
						Compensation: "missing_comp",
					},
				},
			},
			reg: reg,
			wantIssues: map[string]engine.ErrorCode{
				"$.nodes[0].retryPolicy":  engine.CodeUnresolvedReference,
				"$.nodes[0].compensation": engine.CodeUnresolvedReference,
			},
		},
		{
			name: "determinism: all errors",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{
						ID:           "n1",
						Type:         "unknown_type",
						RetryPolicy:  "missing_rp",
						Compensation: "missing_comp",
					},
					{
						ID:   "n2",
						Type: "valid_type",
						Config: map[string]any{
							"count": float64(-5),
						},
					},
				},
				Compensations: map[string]engine.CompensationDefinition{
					"z_comp": {
						Type: "unknown_type",
					},
					"a_comp": {
						Type: "valid_type",
						Config: map[string]any{
							"unknown": 1,
						},
					},
				},
			},
			reg: reg,
			wantIssues: map[string]engine.ErrorCode{
				"$.nodes[0].type":               engine.CodeUnknownNodeType,
				"$.nodes[0].retryPolicy":        engine.CodeUnresolvedReference,
				"$.nodes[0].compensation":       engine.CodeUnresolvedReference,
				"$.nodes[1].config":             engine.CodeInvalidNodeConfig,
				"$.compensations.a_comp.config": engine.CodeInvalidNodeConfig,
				"$.compensations.z_comp.type":   engine.CodeUnknownNodeType,
			},
		},
		{
			name: "schema graph parsed and validated",
			def: func() *engine.GraphDefinition {
				parser := NewParser()
				data := []byte(`
{
  "apiVersion": "graph/v1",
  "id": "my-graph",
  "nodes": [
    { "id": "n1", "type": "t1" },
    { "id": "n2", "type": "t2" }
  ],
  "edges": [
    { "from": "n1", "to": "n2" }
  ]
}
`)
				def, _ := parser.Parse(data, engine.FormatJSON)
				return def
			}(),
			reg:        reg,
			wantIssues: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateSemantics(tt.def, tt.reg)

			if tt.wantIssues == nil && (errs == nil || errs.Len() == 0) {
				return // passed
			}

			if errs == nil {
				t.Fatal("ValidateSemantics returned nil errs")
			}

			issues := errs.Issues()

			// Sort check
			prevPath := ""
			for i, iss := range issues {
				if i > 0 {
					if iss.Path < prevPath {
						t.Errorf("Issues not sorted by path: %q came before %q", prevPath, iss.Path)
					}
				}
				prevPath = iss.Path
			}

			found := make(map[string]bool)
			for _, iss := range issues {
				if wantCode, ok := tt.wantIssues[iss.Path]; ok {
					if iss.Code != wantCode {
						t.Errorf("Path %q: got code %q, want %q", iss.Path, iss.Code, wantCode)
					}
					// ensure the message is trimmed if it's InvalidNodeConfig
					if iss.Code == engine.CodeInvalidNodeConfig {
						if strings.HasPrefix(iss.Message, engine.ErrInvalidConfig.Error()+": ") {
							t.Errorf("Path %q: error message was not trimmed, got %q", iss.Path, iss.Message)
						}
					}
					found[iss.Path] = true
				} else {
					t.Errorf("Unexpected issue: %v", iss)
				}
			}

			for wantPath := range tt.wantIssues {
				if !found[wantPath] {
					t.Errorf("Missing expected issue at path %q", wantPath)
				}
			}
		})
	}

	if factoryCalls != 0 {
		t.Errorf("expected 0 factory calls during ValidateSemantics, got %d", factoryCalls)
	}
}
