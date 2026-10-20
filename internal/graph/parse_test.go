package graph_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/graph"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
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
		"apiVersion: graph/v1\n  id: test\n  nodes:\n    - id: n1",
		"apiVersion: graph/v1\n  id: test\n  nodes: *unresolved",
		`nodes: {a: b, c: d, a: x}`,
	}

	for _, in := range inputs {
		_, _ = parser.Parse([]byte(in), engine.FormatJSON)
		_, _ = parser.Parse([]byte(in), engine.FormatYAML)
	}
}

func TestParserAliasBombRejectQuickly(t *testing.T) {
	parser := graph.NewParser()

	// A classic YAML alias bomb (billion laughs)
	bomb := `
a: &a ["lol","lol","lol","lol","lol","lol","lol","lol","lol"]
b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]
c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]
d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c]
e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d]
f: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e]
g: &g [*f,*f,*f,*f,*f,*f,*f,*f,*f]
h: &h [*g,*g,*g,*g,*g,*g,*g,*g,*g]
i: &i [*h,*h,*h,*h,*h,*h,*h,*h,*h]
`
	_, err := parser.Parse([]byte(bomb), engine.FormatYAML)
	if err == nil {
		t.Fatalf("expected error from alias bomb, got none")
	}
	if !strings.Contains(err.Error(), "YAML aliases are not supported") {
		t.Errorf("expected alias rejection error, got: %v", err)
	}
}

func TestParserNumbers(t *testing.T) {
	parser := graph.NewParser()

	jsonInput := `{"apiVersion": "graph/v1", "id": "t", "nodes": [{"id": "n1", "type": "t1", "config": {"n1": 1000, "n2": 1e3, "n3": 1000.0}}]}`
	yamlInput := "apiVersion: graph/v1\nid: t\nnodes:\n  - id: n1\n    type: t1\n    config:\n      n1: 1000\n      n2: 1e3\n      n3: 1000.0\n"

	jsonModel, err := parser.Parse([]byte(jsonInput), engine.FormatJSON)
	if err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	yamlModel, err := parser.Parse([]byte(yamlInput), engine.FormatYAML)
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	for _, n := range []string{"n1", "n2", "n3"} {
		vJSON := jsonModel.Nodes[0].Config[n]
		vYAML := yamlModel.Nodes[0].Config[n]

		if vJSON != float64(1000) {
			t.Errorf("JSON config %s expected float64 1000, got %T %v", n, vJSON, vJSON)
		}
		if vYAML != float64(1000) {
			t.Errorf("YAML config %s expected float64 1000, got %T %v", n, vYAML, vYAML)
		}
	}
}

func TestParserShuffledKeys(t *testing.T) {
	parser := graph.NewParser()

	json1 := `{"apiVersion": "graph/v1", "id": "1", "nodes": []}`
	json2 := `{"nodes": [], "id": "1", "apiVersion": "graph/v1"}`

	yaml1 := "apiVersion: graph/v1\nid: '1'\nnodes: []\n"
	yaml2 := "nodes: []\napiVersion: graph/v1\nid: '1'\n"

	m1, _ := parser.Parse([]byte(json1), engine.FormatJSON)
	m2, _ := parser.Parse([]byte(json2), engine.FormatJSON)
	m3, _ := parser.Parse([]byte(yaml1), engine.FormatYAML)
	m4, _ := parser.Parse([]byte(yaml2), engine.FormatYAML)

	if !reflect.DeepEqual(m1, m2) {
		t.Error("JSON models with shuffled keys are not deep equal")
	}
	if !reflect.DeepEqual(m3, m4) {
		t.Error("YAML models with shuffled keys are not deep equal")
	}
	if !reflect.DeepEqual(m1, m3) {
		t.Error("JSON and YAML models are not deep equal")
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
