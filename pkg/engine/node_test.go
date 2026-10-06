package engine

import (
	"reflect"
	"testing"
)

func TestNewNode(t *testing.T) {
	tests := []struct {
		name     string
		id       NodeID
		nodeType string
		config   map[string]any
		wantErr  bool
	}{
		{
			name:     "valid node",
			id:       "node1",
			nodeType: "compute",
			config:   map[string]any{"key": "value"},
			wantErr:  false,
		},
		{
			name:     "empty id",
			id:       "",
			nodeType: "compute",
			config:   nil,
			wantErr:  true,
		},
		{
			name:     "empty type",
			id:       "node1",
			nodeType: "",
			config:   nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewNode(tt.id, tt.nodeType, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.ID() != tt.id {
					t.Errorf("ID() got = %v, want %v", got.ID(), tt.id)
				}
				if got.Type() != tt.nodeType {
					t.Errorf("Type() got = %v, want %v", got.Type(), tt.nodeType)
				}
				if !reflect.DeepEqual(got.Config(), tt.config) {
					// Handle nil map vs empty map
					if len(got.Config()) != 0 || len(tt.config) != 0 {
						t.Errorf("Config() got = %v, want %v", got.Config(), tt.config)
					}
				}
			}
		})
	}
}

func TestNodeImmutability(t *testing.T) {
	t.Run("input map mutation does not affect node", func(t *testing.T) {
		config := map[string]any{
			"nested": map[string]any{
				"key": "initial",
			},
			"slice": []any{1, 2, 3},
		}

		node, err := NewNode("node1", "typeA", config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Mutate original config map
		nested := config["nested"].(map[string]any)
		nested["key"] = "mutated"
		config["slice"].([]any)[0] = 99

		nodeConfig := node.Config()
		nodeNested := nodeConfig["nested"].(map[string]any)
		if nodeNested["key"] == "mutated" {
			t.Errorf("node config was mutated by changing original input map")
		}

		nodeSlice := nodeConfig["slice"].([]any)
		if nodeSlice[0] == 99 {
			t.Errorf("node config slice was mutated by changing original input map")
		}
	})

	t.Run("returned map mutation does not affect node", func(t *testing.T) {
		config := map[string]any{
			"nested": map[string]any{
				"key": "initial",
			},
			"slice": []any{1, 2, 3},
		}

		node, err := NewNode("node1", "typeA", config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Get config and mutate it
		retConfig := node.Config()
		nested := retConfig["nested"].(map[string]any)
		nested["key"] = "mutated"
		retConfig["slice"].([]any)[0] = 99

		// Get a fresh config and verify it's unchanged
		freshConfig := node.Config()
		freshNested := freshConfig["nested"].(map[string]any)
		if freshNested["key"] == "mutated" {
			t.Errorf("node config was mutated by changing previously returned map")
		}

		freshSlice := freshConfig["slice"].([]any)
		if freshSlice[0] == 99 {
			t.Errorf("node config slice was mutated by changing previously returned map")
		}
	})
}
