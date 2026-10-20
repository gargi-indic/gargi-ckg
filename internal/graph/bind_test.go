package graph

import (
	"errors"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

func TestBindDefinitionComprehensive(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name           string
		input          string
		format         engine.Format
		expectedErrors []engine.ValidationIssue
	}{
		{
			name:   "top level types missing",
			input:  `{}`,
			format: engine.FormatJSON,
			expectedErrors: []engine.ValidationIssue{
				{Path: "$.apiVersion", Code: engine.CodeMissingRequired, Message: "missing required field 'apiVersion'"},
				{Path: "$.id", Code: engine.CodeMissingRequired, Message: "missing required field 'id'"},
				{Path: "$.nodes", Code: engine.CodeMissingRequired, Message: "missing required field 'nodes'"},
			},
		},
		{
			name:   "invalid edge type",
			input:  `{"apiVersion": "graph/v1", "id": "1", "nodes": [{"id":"1", "type": "t"}], "edges": [{ "from": 1, "to": 2 }]}`,
			format: engine.FormatJSON,
			expectedErrors: []engine.ValidationIssue{
				{Path: "$.edges[0].from", Code: engine.CodeInvalidType, Message: "expected string, got number"},
				{Path: "$.edges[0].to", Code: engine.CodeInvalidType, Message: "expected string, got number"},
			},
		},
		{
			name:   "invalid retry policy types",
			input:  `{"apiVersion": "graph/v1", "id": "1", "nodes": [{"id":"1", "type": "t"}], "retryPolicies": {"test": {"maxAttempts": 1.5, "backoffCoefficient": "2"}}}`,
			format: engine.FormatJSON,
			expectedErrors: []engine.ValidationIssue{
				{Path: "$.retryPolicies.test.backoffCoefficient", Code: engine.CodeInvalidType, Message: "expected float, got string"},
				{Path: "$.retryPolicies.test.maxAttempts", Code: engine.CodeInvalidType, Message: "expected integer, got number with a fractional part"},
			},
		},
		{
			name:   "invalid compensation",
			input:  `{"apiVersion": "graph/v1", "id": "1", "nodes": [{"id":"1", "type": "t"}], "compensations": {"test": {}}}`,
			format: engine.FormatJSON,
			expectedErrors: []engine.ValidationIssue{
				{Path: "$.compensations.test.type", Code: engine.CodeMissingRequired, Message: "missing required field 'type'"},
			},
		},
		{
			name:   "invalid failure policy types",
			input:  `{"apiVersion": "graph/v1", "id": "1", "nodes": [{"id":"1", "type": "t"}], "failurePolicy": {"onNodeFailure": 1, "onCriticalFailure": 2}}`,
			format: engine.FormatJSON,
			expectedErrors: []engine.ValidationIssue{
				{Path: "$.failurePolicy.onCriticalFailure", Code: engine.CodeInvalidType, Message: "expected string, got number"},
				{Path: "$.failurePolicy.onNodeFailure", Code: engine.CodeInvalidType, Message: "expected string, got number"},
			},
		},
		{
			name:   "null object where expected",
			input:  `{"apiVersion": "graph/v1", "id": "1", "nodes": null}`,
			format: engine.FormatJSON,
			expectedErrors: []engine.ValidationIssue{
				{Path: "$.nodes", Code: engine.CodeInvalidType, Message: "expected array, got null"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.Parse([]byte(tt.input), tt.format)
			if err == nil {
				t.Fatalf("expected errors but got nil")
			}

			var valErrs *engine.ValidationErrors
			if !errors.As(err, &valErrs) { // Just for the import errors
				t.Fatalf("expected validation errors")
			}

			issues := valErrs.Issues()
			if len(issues) != len(tt.expectedErrors) {
				t.Errorf("expected %d errors, got %d: %v", len(tt.expectedErrors), len(issues), issues)
			}

			for i, exp := range tt.expectedErrors {
				if i >= len(issues) {
					continue
				}
				if issues[i].Path != exp.Path {
					t.Errorf("[%d] expected path %s, got %s", i, exp.Path, issues[i].Path)
				}
				if issues[i].Code != exp.Code {
					t.Errorf("[%d] expected code %s, got %s", i, exp.Code, issues[i].Code)
				}
				if issues[i].Message != exp.Message {
					t.Errorf("[%d] expected message %s, got %s", i, exp.Message, issues[i].Message)
				}
			}
		})
	}
}
