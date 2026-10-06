package engine

import (
	"testing"
)

func TestPath_Field(t *testing.T) {
	tests := []struct {
		name     string
		base     Path
		field    string
		expected Path
	}{
		{
			name:     "simple identifier",
			base:     PathRoot,
			field:    "config",
			expected: "$.config",
		},
		{
			name:     "simple identifier with underscore and numbers",
			base:     Path("$.nodes[0]"),
			field:    "my_field_123",
			expected: "$.nodes[0].my_field_123",
		},
		{
			name:     "starts with number (not simple)",
			base:     PathRoot,
			field:    "123field",
			expected: "$['123field']",
		},
		{
			name:     "contains hyphen",
			base:     PathRoot,
			field:    "my-key",
			expected: "$['my-key']",
		},
		{
			name:     "contains dot",
			base:     PathRoot,
			field:    "my.key",
			expected: "$['my.key']",
		},
		{
			name:     "contains space",
			base:     PathRoot,
			field:    "my key",
			expected: "$['my key']",
		},
		{
			name:     "contains single quote",
			base:     PathRoot,
			field:    "my'key",
			expected: "$['my\\'key']",
		},
		{
			name:     "contains backslash",
			base:     PathRoot,
			field:    "my\\key",
			expected: "$['my\\\\key']",
		},
		{
			name:     "empty string",
			base:     PathRoot,
			field:    "",
			expected: "$['']",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.Field(tt.field)
			if result != tt.expected {
				t.Errorf("Path.Field() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestPath_Index(t *testing.T) {
	tests := []struct {
		name     string
		base     Path
		index    int
		expected Path
	}{
		{
			name:     "root index",
			base:     PathRoot,
			index:    0,
			expected: "$[0]",
		},
		{
			name:     "nested index",
			base:     Path("$.nodes"),
			index:    5,
			expected: "$.nodes[5]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.Index(tt.index)
			if result != tt.expected {
				t.Errorf("Path.Index() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestPath_String(t *testing.T) {
	p := PathRoot.Field("nodes").Index(2).Field("config").Field("a-b")
	expected := "$.nodes[2].config['a-b']"
	if p.String() != expected {
		t.Errorf("Path.String() = %v, want %v", p.String(), expected)
	}
}
