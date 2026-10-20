package engine_test

import (
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected engine.Format
		wantErr  bool
	}{
		{"json", engine.FormatJSON, false},
		{"JSON", engine.FormatJSON, false},
		{"yaml", engine.FormatYAML, false},
		{"YAML", engine.FormatYAML, false},
		{"yml", engine.FormatYAML, false},
		{"YML", engine.FormatYAML, false},
		{"xml", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := engine.ParseFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFormat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("ParseFormat() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestToGraph(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		def := &engine.GraphDefinition{
			ID: "test-graph",
			Nodes: []engine.NodeDefinition{
				{ID: "node1", Type: "type1", Config: nil},
				{ID: "node2", Type: "type2", Config: nil},
			},
			Edges: []engine.EdgeDefinition{
				{From: "node1", To: "node2", Condition: "true"},
			},
		}

		g, err := def.ToGraph()
		if err != nil {
			t.Fatalf("ToGraph() failed: %v", err)
		}
		if g.ID() != "test-graph" {
			t.Errorf("expected graph ID test-graph, got %s", g.ID())
		}
		if len(g.Nodes()) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(g.Nodes()))
		}
		if len(g.Edges()) != 1 {
			t.Errorf("expected 1 edge, got %d", len(g.Edges()))
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		var def *engine.GraphDefinition
		_, err := def.ToGraph()
		if err == nil {
			t.Error("expected error for nil receiver, got none")
		}
	})

	t.Run("node error", func(t *testing.T) {
		def := &engine.GraphDefinition{
			ID: "test-graph",
			Nodes: []engine.NodeDefinition{
				{ID: "", Type: "type1", Config: nil}, // empty ID is invalid
			},
		}
		_, err := def.ToGraph()
		if err == nil {
			t.Error("expected error for invalid node, got none")
		}
	})

	t.Run("edge error", func(t *testing.T) {
		def := &engine.GraphDefinition{
			ID: "test-graph",
			Nodes: []engine.NodeDefinition{
				{ID: "node1", Type: "type1", Config: nil},
			},
			Edges: []engine.EdgeDefinition{
				{From: "node1", To: "node1"}, // self loop is invalid
			},
		}
		_, err := def.ToGraph()
		if err == nil {
			t.Error("expected error for invalid edge, got none")
		}
	})

	t.Run("graph error", func(t *testing.T) {
		def := &engine.GraphDefinition{
			ID: "", // empty graph ID is invalid
			Nodes: []engine.NodeDefinition{
				{ID: "node1", Type: "type1", Config: nil},
			},
		}
		_, err := def.ToGraph()
		if err == nil {
			t.Error("expected error for invalid graph, got none")
		}
	})
}
