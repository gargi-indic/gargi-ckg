package engine

import (
	"testing"
)

func TestNewExecutionID(t *testing.T) {
	id, err := NewExecutionID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id == "" {
		t.Errorf("expected non-empty ID")
	}

	_, err = ParseExecutionID(id.String())
	if err != nil {
		t.Errorf("generated ID is not a valid UUIDv4: %v", err)
	}
}

func TestParseExecutionID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid format",
			input:   "123e4567-e89b-41d3-a456-426614174000",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format - no dashes",
			input:   "123e4567e89b41d3a456426614174000",
			wantErr: true,
		},
		{
			name:    "invalid format - wrong version",
			input:   "123e4567-e89b-11d3-a456-426614174000", // v1
			wantErr: true,
		},
		{
			name:    "invalid format - wrong variant",
			input:   "123e4567-e89b-41d3-c456-426614174000", // C instead of 8,9,A,B
			wantErr: true,
		},
		{
			name:    "invalid format - uppercase",
			input:   "123E4567-E89B-41D3-A456-426614174000",
			wantErr: true,
		},
		{
			name:    "invalid format - too short",
			input:   "123e4567-e89b-41d3-a456-42661417400",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExecutionID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseExecutionID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.String() != tt.input {
				t.Errorf("ParseExecutionID() got = %v, want %v", got, tt.input)
			}
		})
	}
}
