package engine

import (
	"strings"
	"testing"
)

func TestStateMutation_Validate(t *testing.T) {
	validStatus := StatusSucceeded
	invalidStatus := Status("Unknown")
	errMsg := "some error"

	tests := []struct {
		name    string
		m       StateMutation
		wantErr string
	}{
		{
			name: "Valid status change",
			m:    NewStateMutation("node1", 1, &validStatus, nil, nil, nil),
		},
		{
			name: "Valid set map",
			m:    NewStateMutation("node1", 1, nil, map[string]any{"k1": "v1"}, nil, nil),
		},
		{
			name: "Valid unset list",
			m:    NewStateMutation("node1", 1, nil, nil, []string{"k1"}, nil),
		},
		{
			name: "Valid error message change",
			m:    NewStateMutation("node1", 1, nil, nil, nil, &errMsg),
		},
		{
			name:    "Invalid empty node ID",
			m:       NewStateMutation("", 1, &validStatus, nil, nil, nil),
			wantErr: "mutation must have a non-empty NodeID",
		},
		{
			name:    "Invalid status",
			m:       NewStateMutation("node1", 1, &invalidStatus, nil, nil, nil),
			wantErr: "mutation has an invalid Status",
		},
		{
			name:    "No effect mutation",
			m:       NewStateMutation("node1", 1, nil, nil, nil, nil),
			wantErr: "mutation has no effect",
		},
		{
			name:    "Empty key in Set",
			m:       NewStateMutation("node1", 1, nil, map[string]any{"": "v1"}, nil, nil),
			wantErr: "mutation has an empty key in Set",
		},
		{
			name:    "Empty key in Unset",
			m:       NewStateMutation("node1", 1, nil, nil, []string{""}, nil),
			wantErr: "mutation has an empty key in Unset",
		},
		{
			name:    "Conflicting key in Set and Unset",
			m:       NewStateMutation("node1", 1, nil, map[string]any{"k1": "v1"}, []string{"k1"}, nil),
			wantErr: "mutation has conflicting key in both Set and Unset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.m.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("Validate() expected error containing %q, got none", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Validate() error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestStateMutation_DeepCopy(t *testing.T) {
	originalMap := map[string]any{
		"nested": map[string]any{
			"key": "value",
		},
	}
	originalSlice := []string{"k1", "k2"}

	m := NewStateMutation("node1", 1, nil, originalMap, originalSlice, nil)

	// Mutate original map
	nested := originalMap["nested"].(map[string]any)
	nested["key"] = "changed"
	originalMap["new_key"] = "new_value"

	// Mutate original slice
	originalSlice[0] = "changed"

	if m.Set["new_key"] != nil {
		t.Error("Set map was modified by changing the original map")
	}
	mNested := m.Set["nested"].(map[string]any)
	if mNested["key"] != "value" {
		t.Error("Nested map in Set was modified by changing the original map")
	}
	if m.Unset[0] != "k1" {
		t.Error("Unset slice was modified by changing the original slice")
	}
}
