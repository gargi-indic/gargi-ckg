package engine

import (
	"errors"
)

// ErrorClass defines the orchestration-agnostic classification of an error.
type ErrorClass string

const (
	// ClassRetryable indicates the error is transient and the operation should be retried.
	ClassRetryable ErrorClass = "retryable"
	// ClassNonRetryable indicates the error is permanent and retrying will not help.
	ClassNonRetryable ErrorClass = "non-retryable"
	// ClassCritical indicates a severe failure that must trigger Saga compensation workflows (per ADR-003).
	ClassCritical ErrorClass = "critical"
)

// Valid returns true if the ErrorClass is one of the predefined constants.
func (c ErrorClass) Valid() bool {
	switch c {
	case ClassRetryable, ClassNonRetryable, ClassCritical:
		return true
	default:
		return false
	}
}

// NodeError represents a classified error returned by a node's execution hooks.
type NodeError struct {
	Class ErrorClass
	Err   error
}

// Error returns the underlying error message or a generic message if no underlying error is provided.
// It is safe to call on a nil receiver.
func (e *NodeError) Error() string {
	if e == nil {
		return "nil NodeError"
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if !e.Class.Valid() {
		return "unclassified error"
	}
	return string(e.Class) + " error"
}

// Unwrap returns the underlying error, allowing errors.Is and errors.As to work.
// It is safe to call on a nil receiver.
func (e *NodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewRetryableError creates a new NodeError classified as ClassRetryable.
// If err is nil, it still returns a non-nil *NodeError with a generic message.
func NewRetryableError(err error) *NodeError {
	if err == nil {
		err = errors.New("retryable error occurred")
	}
	return &NodeError{
		Class: ClassRetryable,
		Err:   err,
	}
}

// NewNonRetryableError creates a new NodeError classified as ClassNonRetryable.
// If err is nil, it still returns a non-nil *NodeError with a generic message.
func NewNonRetryableError(err error) *NodeError {
	if err == nil {
		err = errors.New("non-retryable error occurred")
	}
	return &NodeError{
		Class: ClassNonRetryable,
		Err:   err,
	}
}

// NewCriticalError creates a new NodeError classified as ClassCritical.
// If err is nil, it still returns a non-nil *NodeError with a generic message.
func NewCriticalError(err error) *NodeError {
	if err == nil {
		err = errors.New("critical error occurred")
	}
	return &NodeError{
		Class: ClassCritical,
		Err:   err,
	}
}

// ClassOf determines the ErrorClass of an error.
// It unwraps the error using errors.As to find a *NodeError.
// If found, and its Class is valid, it returns the Class of the outermost *NodeError.
// If the error is nil, it returns an empty string.
// If the error does not contain a *NodeError, or if it contains a *NodeError
// with an invalid or empty Class (e.g., manually constructed), or if the
// matched *NodeError is a typed nil, it returns ClassNonRetryable because unknown
// failures must not be retried blindly.
func ClassOf(err error) ErrorClass {
	if err == nil {
		return ""
	}

	var nodeErr *NodeError
	if errors.As(err, &nodeErr) {
		if nodeErr != nil && nodeErr.Class.Valid() {
			return nodeErr.Class
		}
		return ClassNonRetryable
	}

	return ClassNonRetryable
}

// IsRetryable returns true if the error classification is ClassRetryable.
func IsRetryable(err error) bool {
	return ClassOf(err) == ClassRetryable
}

// IsCritical returns true if the error classification is ClassCritical.
// Critical errors trigger Saga compensation workflows per ADR-003.
func IsCritical(err error) bool {
	return ClassOf(err) == ClassCritical
}
