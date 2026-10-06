package engine

import (
	"reflect"
	"testing"
)

func TestNewEdge(t *testing.T) {
	tests := []struct {
		name        string
		from        NodeID
		to          NodeID
		condition   string
		dataMapping map[string]string
		wantErr     bool
	}{
		{
			name:        "valid edge",
			from:        "node1",
			to:          "node2",
			condition:   "success == true",
			dataMapping: map[string]string{"targetKey": "source.path"},
			wantErr:     false,
		},
		{
			name:        "empty from",
			from:        "",
			to:          "node2",
			condition:   "",
			dataMapping: nil,
			wantErr:     true,
		},
		{
			name:        "empty to",
			from:        "node1",
			to:          "",
			condition:   "",
			dataMapping: nil,
			wantErr:     true,
		},
		{
			name:        "self loop",
			from:        "node1",
			to:          "node1",
			condition:   "",
			dataMapping: nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewEdge(tt.from, tt.to, tt.condition, tt.dataMapping)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEdge() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.From() != tt.from {
					t.Errorf("From() got = %v, want %v", got.From(), tt.from)
				}
				if got.To() != tt.to {
					t.Errorf("To() got = %v, want %v", got.To(), tt.to)
				}
				if got.Condition() != tt.condition {
					t.Errorf("Condition() got = %v, want %v", got.Condition(), tt.condition)
				}
				if !reflect.DeepEqual(got.DataMapping(), tt.dataMapping) {
					if len(got.DataMapping()) != 0 || len(tt.dataMapping) != 0 {
						t.Errorf("DataMapping() got = %v, want %v", got.DataMapping(), tt.dataMapping)
					}
				}
			}
		})
	}
}

func TestEdgeImmutability(t *testing.T) {
	t.Run("input map mutation does not affect edge", func(t *testing.T) {
		mapping := map[string]string{
			"key1": "val1",
		}

		edge, err := NewEdge("nodeA", "nodeB", "", mapping)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Mutate original map
		mapping["key1"] = "mutated"

		if edge.DataMapping()["key1"] == "mutated" {
			t.Errorf("edge data mapping was mutated by changing original input map")
		}
	})

	t.Run("returned map mutation does not affect edge", func(t *testing.T) {
		mapping := map[string]string{
			"key1": "val1",
		}

		edge, err := NewEdge("nodeA", "nodeB", "", mapping)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Get map and mutate it
		retMapping := edge.DataMapping()
		retMapping["key1"] = "mutated"

		// Get a fresh map and verify it's unchanged
		freshMapping := edge.DataMapping()
		if freshMapping["key1"] == "mutated" {
			t.Errorf("edge data mapping was mutated by changing previously returned map")
		}
	})
}
