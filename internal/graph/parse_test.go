package graph_test

import (
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/graph"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func TestParserLimits(t *testing.T) {
	parser := graph.NewParser()

	t.Run("empty input", func(t *testing.T) {
		_, err := parser.Parse([]byte("   "), engine.FormatJSON)
		if err == nil || !strings.Contains(err.Error(), "empty or whitespace only") {
			t.Errorf("expected empty input error, got %v", err)
		}
	})

	t.Run("max bytes exceeded", func(t *testing.T) {
		data := make([]byte, graph.MaxDefinitionBytes+1)
		_, err := parser.Parse(data, engine.FormatJSON)
		if err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
			t.Errorf("expected max size error, got %v", err)
		}
	})
}

func TestParserMalformedInputsNoPanic(_ *testing.T) {
	parser := graph.NewParser()
	inputs := []string{
		"{",
		`{"nodes": [}`,
		`{"nodes": [{"id": 1} `,
		`apiVersion: graph/v1\n  id: test\n  nodes:\n    - id: n1`,
		`apiVersion: graph/v1\n  id: test\n  nodes: *unresolved`,
		`nodes: {a: b, c: d, a: x}`,
	}

	for _, in := range inputs {
		_, _ = parser.Parse([]byte(in), engine.FormatJSON)
		_, _ = parser.Parse([]byte(in), engine.FormatYAML)
	}
}

func TestParserMaxDepth(t *testing.T) {
	parser := graph.NewParser()

	// Generate deeply nested JSON
	depth := graph.MaxDefinitionDepth + 5
	var nested string
	for i := 0; i < depth; i++ {
		nested += `{"a":`
	}
	nested += `1`
	for i := 0; i < depth; i++ {
		nested += `}`
	}

	_, err := parser.Parse([]byte(nested), engine.FormatJSON)
	if err == nil || !strings.Contains(err.Error(), "maximum nesting depth") {
		t.Errorf("expected max depth error for JSON, got %v", err)
	}

	// Generate deeply nested YAML
	var nestedYaml string
	for i := 0; i < depth; i++ {
		nestedYaml += strings.Repeat("  ", i) + "a:\n"
	}
	nestedYaml += strings.Repeat("  ", depth) + "1\n"

	_, err = parser.Parse([]byte(nestedYaml), engine.FormatYAML)
	if err == nil || !strings.Contains(err.Error(), "maximum nesting depth") {
		t.Errorf("expected max depth error for YAML, got %v", err)
	}
}
