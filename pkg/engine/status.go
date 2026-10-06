package engine

import "errors"

// Status represents the execution status of a graph or node.
type Status string

// Enumeration of all valid execution statuses.
const (
	StatusPending      Status = "Pending"
	StatusReady        Status = "Ready"
	StatusRunning      Status = "Running"
	StatusSucceeded    Status = "Succeeded"
	StatusFailed       Status = "Failed"
	StatusSkipped      Status = "Skipped"
	StatusCompensating Status = "Compensating"
	StatusCompensated  Status = "Compensated"
)

// Valid returns true if the status is a recognized value.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusReady, StatusRunning, StatusSucceeded,
		StatusFailed, StatusSkipped, StatusCompensating, StatusCompensated:
		return true
	default:
		return false
	}
}

// String returns the string representation of the status.
func (s Status) String() string {
	return string(s)
}

// IsTerminal returns true if the status is a terminal state (Skipped or Compensated)
// from which no further transitions are allowed.
func (s Status) IsTerminal() bool {
	return s == StatusSkipped || s == StatusCompensated
}

// ParseStatus converts a string into a Status, returning an error if invalid.
func ParseStatus(str string) (Status, error) {
	s := Status(str)
	if !s.Valid() {
		return "", errors.New("invalid status: " + str)
	}
	return s, nil
}

// CanTransition returns true if the transition from the 'from' status to the 'to' status is allowed.
// The allowed transitions are:
// - Pending -> Ready, Skipped
// - Ready -> Running, Skipped
// - Running -> Succeeded, Failed
// - Succeeded -> Compensating
// - Failed -> Compensating
// - Compensating -> Compensated
func CanTransition(from, to Status) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}

	switch from {
	case StatusPending:
		return to == StatusReady || to == StatusSkipped
	case StatusReady:
		return to == StatusRunning || to == StatusSkipped
	case StatusRunning:
		return to == StatusSucceeded || to == StatusFailed
	case StatusSucceeded, StatusFailed:
		return to == StatusCompensating
	case StatusCompensating:
		return to == StatusCompensated
	default:
		return false
	}
}
