package engine

import (
	"errors"
	"fmt"
	"testing"
)

func TestNodeError_NilErrorHandling(t *testing.T) {
	err1 := NewRetryableError(nil)
	if err1 == nil {
		t.Fatal("expected non-nil error")
	}
	if err1.Class != ClassRetryable {
		t.Errorf("expected ClassRetryable, got %s", err1.Class)
	}
	if err1.Error() != "retryable error occurred" {
		t.Errorf("expected generic error string, got %s", err1.Error())
	}

	err2 := NewNonRetryableError(nil)
	if err2 == nil {
		t.Fatal("expected non-nil error")
	}
	if err2.Class != ClassNonRetryable {
		t.Errorf("expected ClassNonRetryable, got %s", err2.Class)
	}
	if err2.Error() != "non-retryable error occurred" {
		t.Errorf("expected generic error string, got %s", err2.Error())
	}

	err3 := NewCriticalError(nil)
	if err3 == nil {
		t.Fatal("expected non-nil error")
	}
	if err3.Class != ClassCritical {
		t.Errorf("expected ClassCritical, got %s", err3.Class)
	}
	if err3.Error() != "critical error occurred" {
		t.Errorf("expected generic error string, got %s", err3.Error())
	}
}

func TestNodeError_Wrapping(t *testing.T) {
	baseErr := errors.New("underlying fault")
	retryErr := NewRetryableError(baseErr)

	// Wrap the NodeError
	wrappedErr := fmt.Errorf("node execution failed: %w", retryErr)

	// Should still be able to find the base error
	if !errors.Is(wrappedErr, baseErr) {
		t.Errorf("expected errors.Is to find the base error")
	}

	// Should still resolve class correctly
	if class := ClassOf(wrappedErr); class != ClassRetryable {
		t.Errorf("expected ClassRetryable, got %s", class)
	}

	if !IsRetryable(wrappedErr) {
		t.Error("expected IsRetryable to return true")
	}
}

func TestNodeError_NestedClassification(t *testing.T) {
	baseErr := NewCriticalError(errors.New("db failure"))
	// Wrap it in a non-retryable
	outerErr := NewNonRetryableError(baseErr)

	// Since errors.As matches the first error in the chain that fits,
	// and Error() traverses from outermost inward, errors.As will
	// match the outermost NodeError first. Thus we expect ClassNonRetryable.
	if class := ClassOf(outerErr); class != ClassNonRetryable {
		t.Errorf("expected outermost classification (ClassNonRetryable), got %s", class)
	}

	// Test wrapping with fmt.Errorf
	wrappedOuter := fmt.Errorf("context: %w", outerErr)
	if class := ClassOf(wrappedOuter); class != ClassNonRetryable {
		t.Errorf("expected outermost classification from wrapped error, got %s", class)
	}
}

func TestNodeError_UnclassifiedAndNil(t *testing.T) {
	// Nil error
	if class := ClassOf(nil); class != "" {
		t.Errorf("expected empty string for nil error, got %s", class)
	}

	// Unclassified error
	stdErr := errors.New("unknown problem")
	if class := ClassOf(stdErr); class != ClassNonRetryable {
		t.Errorf("expected ClassNonRetryable for unclassified error, got %s", class)
	}

	if IsRetryable(stdErr) {
		t.Error("expected IsRetryable to be false for unclassified error")
	}
	if IsCritical(stdErr) {
		t.Error("expected IsCritical to be false for unclassified error")
	}
}

func TestNodeError_InvalidClasses(t *testing.T) {
	// Empty class
	emptyErr := &NodeError{}
	if class := ClassOf(emptyErr); class != ClassNonRetryable {
		t.Errorf("expected ClassNonRetryable for empty struct, got %s", class)
	}
	if emptyErr.Error() != "unclassified error" {
		t.Errorf("expected 'unclassified error' string, got %s", emptyErr.Error())
	}

	// Bogus class
	bogusErr := &NodeError{Class: "bogus", Err: errors.New("bad")}
	if class := ClassOf(bogusErr); class != ClassNonRetryable {
		t.Errorf("expected ClassNonRetryable for bogus class, got %s", class)
	}
	if bogusErr.Error() != "bad" {
		t.Errorf("expected 'bad' string, got %s", bogusErr.Error())
	}

	// Wrapped empty and bogus
	wrappedEmpty := fmt.Errorf("wrap: %w", emptyErr)
	if class := ClassOf(wrappedEmpty); class != ClassNonRetryable {
		t.Errorf("expected ClassNonRetryable for wrapped empty struct, got %s", class)
	}

	wrappedBogus := fmt.Errorf("wrap: %w", bogusErr)
	if class := ClassOf(wrappedBogus); class != ClassNonRetryable {
		t.Errorf("expected ClassNonRetryable for wrapped bogus class, got %s", class)
	}
}

func TestNodeError_NilReceiver(t *testing.T) {
	var nilErr *NodeError

	// Error() should not panic
	if str := nilErr.Error(); str != "nil NodeError" {
		t.Errorf("expected 'nil NodeError' from nil receiver Error(), got %s", str)
	}

	// Unwrap() should not panic
	if unwrapped := nilErr.Unwrap(); unwrapped != nil {
		t.Errorf("expected nil from nil receiver Unwrap(), got %v", unwrapped)
	}

	// ClassOf() should handle typed nil gracefully
	var typedNil error = nilErr
	if class := ClassOf(typedNil); class != ClassNonRetryable {
		t.Errorf("expected ClassNonRetryable for typed nil NodeError, got %s", class)
	}
}
