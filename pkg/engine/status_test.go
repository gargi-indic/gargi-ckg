package engine

import (
	"testing"
)

func TestStatus_Valid(t *testing.T) {
	tests := []struct {
		name string
		s    Status
		want bool
	}{
		{"Pending", StatusPending, true},
		{"Ready", StatusReady, true},
		{"Running", StatusRunning, true},
		{"Succeeded", StatusSucceeded, true},
		{"Failed", StatusFailed, true},
		{"Skipped", StatusSkipped, true},
		{"Compensating", StatusCompensating, true},
		{"Compensated", StatusCompensated, true},
		{"Empty", "", false},
		{"Invalid", "Unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatus_String(t *testing.T) {
	if got := StatusPending.String(); got != "Pending" {
		t.Errorf("String() = %v, want %v", got, "Pending")
	}
}

func TestStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name string
		s    Status
		want bool
	}{
		{"Pending", StatusPending, false},
		{"Ready", StatusReady, false},
		{"Running", StatusRunning, false},
		{"Succeeded", StatusSucceeded, false},
		{"Failed", StatusFailed, false},
		{"Skipped", StatusSkipped, true},
		{"Compensating", StatusCompensating, false},
		{"Compensated", StatusCompensated, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.IsTerminal(); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name    string
		str     string
		want    Status
		wantErr bool
	}{
		{"Valid Pending", "Pending", StatusPending, false},
		{"Valid Compensated", "Compensated", StatusCompensated, false},
		{"Empty", "", "", true},
		{"Invalid", "FOO", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStatus(tt.str)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseStatus() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanTransition(t *testing.T) {
	allStatuses := []Status{
		StatusPending, StatusReady, StatusRunning, StatusSucceeded,
		StatusFailed, StatusSkipped, StatusCompensating, StatusCompensated,
	}

	allowedTransitions := map[Status]map[Status]bool{
		StatusPending:      {StatusReady: true, StatusSkipped: true},
		StatusReady:        {StatusRunning: true, StatusSkipped: true},
		StatusRunning:      {StatusSucceeded: true, StatusFailed: true},
		StatusSucceeded:    {StatusCompensating: true},
		StatusFailed:       {StatusCompensating: true},
		StatusCompensating: {StatusCompensated: true},
	}

	for _, from := range allStatuses {
		for _, to := range allStatuses {
			want := allowedTransitions[from][to]
			got := CanTransition(from, to)
			if got != want {
				t.Errorf("CanTransition(%v, %v) = %v, want %v", from, to, got, want)
			}
		}
	}

	// Test invalid transitions
	if CanTransition("", StatusPending) {
		t.Errorf("CanTransition from invalid status should be false")
	}
	if CanTransition(StatusPending, "") {
		t.Errorf("CanTransition to invalid status should be false")
	}
	if CanTransition("", "") {
		t.Errorf("CanTransition empty to empty should be false")
	}
}
