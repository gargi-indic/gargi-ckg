package graph

import (
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func ptr[T any](v T) *T {
	return &v
}

func TestValidateStructure(t *testing.T) {
	tests := []struct {
		name       string
		def        *engine.GraphDefinition
		wantIssues map[string]engine.ErrorCode // path -> code
	}{
		{
			name: "valid graph",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{ID: "n1", Type: "t1"},
					{ID: "n2", Type: "t2"},
				},
				Edges: []engine.EdgeDefinition{
					{From: "n1", To: "n2"},
				},
			},
			wantIssues: nil, // empty validation errors
		},
		{
			name: "nil definition",
			def:  nil,
			wantIssues: map[string]engine.ErrorCode{
				"$": engine.CodeInvalidType,
			},
		},
		{
			name: "empty nodes",
			def: &engine.GraphDefinition{
				ID: "g1",
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.nodes": engine.CodeMissingRequired,
			},
		},
		{
			name: "invalid identifier patterns",
			def: &engine.GraphDefinition{
				ID: "g_1!",
				Nodes: []engine.NodeDefinition{
					{ID: "-n1", Type: "1t"}, // type doesn't start with letter
					{ID: "n2", Type: "t2", RetryPolicy: "rp!", Compensation: "c!"},
				},
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.id":                    engine.CodeInvalidValue,
				"$.nodes[0].id":           engine.CodeInvalidValue,
				"$.nodes[0].type":         engine.CodeInvalidValue,
				"$.nodes[1].retryPolicy":  engine.CodeInvalidValue,
				"$.nodes[1].compensation": engine.CodeInvalidValue,
			},
		},
		{
			name: "duplicate node ID",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{ID: "n1", Type: "t1"},
					{ID: "n1", Type: "t2"},
				},
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.nodes[1].id": engine.CodeDuplicateID,
			},
		},
		{
			name: "edges checks: dangling, self_loop, duplicate, valid conditions/dataMapping",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{ID: "n1", Type: "t1"},
					{ID: "n2", Type: "t2"},
				},
				Edges: []engine.EdgeDefinition{
					{From: "n1", To: "nx"},
					{From: "n1", To: "n1"},
					{From: "n1", To: "n2"},
					{From: "n1", To: "n2"},
					{From: "n2", To: "n1", Condition: "", DataMapping: map[string]string{"": "v", "k": ""}},
					{From: "n2", To: "n1", Condition: strings.Repeat("é", 4097)},
					{From: "n2", To: "n1", Condition: "1 + 2"}, // invalid CEL bool
				},
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.edges[0].to":              engine.CodeDanglingEdge,
				"$.edges[1]":                 engine.CodeSelfLoop,
				"$.edges[3]":                 engine.CodeInvalidValue,
				"$.edges[4].dataMapping['']": engine.CodeInvalidValue,
				"$.edges[4].dataMapping.k":   engine.CodeInvalidValue,
				"$.edges[5].condition":       engine.CodeInvalidCondition,
				"$.edges[5]":                 engine.CodeInvalidValue,
				"$.edges[6].condition":       engine.CodeInvalidCondition,
				"$.edges[6]":                 engine.CodeInvalidValue,
			},
		},
		{
			name: "retryPolicies checks",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{ID: "n1", Type: "t1"},
				},
				RetryPolicies: map[string]engine.RetryPolicy{
					"a.b\"c": {}, // test escaping
					"rp1": {
						MaxAttempts:        ptr(0),
						InitialIntervalMs:  ptr(0),
						BackoffCoefficient: ptr(0.99),
						MaxIntervalMs:      ptr(0),
					},
				},
				Compensations: map[string]engine.CompensationDefinition{
					"c_!": {Type: "t1"},
					"c1":  {Type: "1t"},
				},
				FailurePolicy: &engine.FailurePolicy{
					OnNodeFailure:     "invalid_enum",
					OnCriticalFailure: "invalid_enum",
				},
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.retryPolicies['a.b\"c']":              engine.CodeInvalidValue,
				"$.retryPolicies.rp1.maxAttempts":        engine.CodeInvalidValue,
				"$.retryPolicies.rp1.initialIntervalMs":  engine.CodeInvalidValue,
				"$.retryPolicies.rp1.backoffCoefficient": engine.CodeInvalidValue,
				"$.retryPolicies.rp1.maxIntervalMs":      engine.CodeInvalidValue,
				"$.compensations['c_!']":                 engine.CodeInvalidValue,
				"$.compensations.c1.type":                engine.CodeInvalidValue,
				"$.failurePolicy.onNodeFailure":          engine.CodeInvalidValue,
				"$.failurePolicy.onCriticalFailure":      engine.CodeInvalidValue,
			},
		},
		{
			name: "cycle check 2 nodes",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{ID: "n1", Type: "t1"},
					{ID: "n2", Type: "t2"},
				},
				Edges: []engine.EdgeDefinition{
					{From: "n1", To: "n2"},
					{From: "n2", To: "n1"},
				},
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.edges": engine.CodeCycle,
			},
		},
		{
			name: "cycle check 3 nodes",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{ID: "n1", Type: "t1"},
					{ID: "n2", Type: "t2"},
					{ID: "n3", Type: "t3"},
				},
				Edges: []engine.EdgeDefinition{
					{From: "n1", To: "n2"},
					{From: "n2", To: "n3"},
					{From: "n3", To: "n1"},
				},
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.edges": engine.CodeCycle,
			},
		},

		{
			name: "all errors at once",
			def: &engine.GraphDefinition{
				ID: "g 1",
				Nodes: []engine.NodeDefinition{
					{ID: "n1", Type: "t1"},
					{ID: "n1", Type: "t2"},
				},
				Edges: []engine.EdgeDefinition{
					{From: "n1", To: "nx"},
					{From: "n1", To: "n1"},
				},
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.id":          engine.CodeInvalidValue,
				"$.nodes[1].id": engine.CodeDuplicateID,
				"$.edges[0].to": engine.CodeDanglingEdge,
				"$.edges[1]":    engine.CodeSelfLoop,
			},
		},
		{
			name: "determinism: shuffled edge order",
			def: &engine.GraphDefinition{
				ID: "g1",
				Nodes: []engine.NodeDefinition{
					{ID: "n1", Type: "t1"},
					{ID: "n2", Type: "t2"},
					{ID: "n3", Type: "t3"},
				},
				Edges: []engine.EdgeDefinition{
					{From: "n2", To: "n3"},
					{From: "n1", To: "n2"},
					{From: "n3", To: "n1"},
				},
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.edges": engine.CodeCycle,
			},
		},
		{
			name: "schema example parsing",
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
			wantIssues: nil,
		},

		{
			name: "zero value GraphDefinition",
			def:  &engine.GraphDefinition{},
			wantIssues: map[string]engine.ErrorCode{
				"$.id":    engine.CodeInvalidValue,
				"$.nodes": engine.CodeMissingRequired,
			},
		},

		{
			name: "identifier bounds and sorting",
			def: &engine.GraphDefinition{
				ID: strings.Repeat("a", 128), // OK
				Nodes: []engine.NodeDefinition{
					{ID: engine.NodeID(strings.Repeat("b", 129)), Type: "t"}, // Invalid
					{ID: "valid", Type: "t"},
				},
				Edges: []engine.EdgeDefinition{
					{From: engine.NodeID(strings.Repeat("b", 129)), To: "valid"}, // 1 issue (invalid from), dangling should NOT be triggered and NO ToGraph error!
				},
			},
			wantIssues: map[string]engine.ErrorCode{
				"$.nodes[0].id":   engine.CodeInvalidValue,
				"$.edges[0].from": engine.CodeInvalidValue,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateStructure(tt.def)
			if errs == nil {
				t.Fatal("ValidateStructure returned nil")
			}

			if tt.wantIssues == nil && errs.Len() == 0 {
				return // passed
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
}
