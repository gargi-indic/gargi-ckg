package engine

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrorCode represents a stable, lower_snake_case string for validation errors.
// These codes are part of the public API and will never be renamed, only added to.
type ErrorCode string

// The following constants define all possible validation error codes.
const (
	CodeInvalidFormat       ErrorCode = "invalid_format"
	CodeUnknownField        ErrorCode = "unknown_field"
	CodeMissingRequired     ErrorCode = "missing_required"
	CodeInvalidType         ErrorCode = "invalid_type"
	CodeInvalidValue        ErrorCode = "invalid_value"
	CodeUnsupportedVersion  ErrorCode = "unsupported_version"
	CodeDuplicateID         ErrorCode = "duplicate_id"
	CodeDanglingEdge        ErrorCode = "dangling_edge"
	CodeSelfLoop            ErrorCode = "self_loop"
	CodeCycle               ErrorCode = "cycle"
	CodeNoRoot              ErrorCode = "no_root"
	CodeNoLeaf              ErrorCode = "no_leaf"
	CodeUnknownNodeType     ErrorCode = "unknown_node_type"
	CodeInvalidNodeConfig   ErrorCode = "invalid_node_config"
	CodeUnresolvedReference ErrorCode = "unresolved_reference"
	CodeInvalidCondition    ErrorCode = "invalid_condition"
)

// Valid returns true if the ErrorCode is one of the defined constants.
func (c ErrorCode) Valid() bool {
	switch c {
	case CodeInvalidFormat, CodeUnknownField, CodeMissingRequired, CodeInvalidType,
		CodeInvalidValue, CodeUnsupportedVersion, CodeDuplicateID, CodeDanglingEdge,
		CodeSelfLoop, CodeCycle, CodeNoRoot, CodeNoLeaf, CodeUnknownNodeType,
		CodeInvalidNodeConfig, CodeUnresolvedReference, CodeInvalidCondition:
		return true
	default:
		return false
	}
}

// ErrInvalidDefinition is a sentinel error indicating that a graph definition is invalid.
var ErrInvalidDefinition = errors.New("invalid graph definition")

// ValidationIssue represents a single validation problem.
// It implements the error interface.
//
// The API layer will map ValidationIssue and ValidationErrors to a gRPC InvalidArgument
// status with a BadRequest detail (one field violation per issue: field = Path,
// description = Message plus Code) and to an HTTP 400 problem body.
// Producers must return all issues rather than the first.
type ValidationIssue struct {
	Path    string    `json:"path"`
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// Error implements the error interface for ValidationIssue.
func (i ValidationIssue) Error() string {
	return fmt.Sprintf("%s: %s (%s)", i.Path, i.Message, i.Code)
}

// ValidationErrors is a collector that aggregates multiple validation issues.
//
// The API layer will map ValidationIssue and ValidationErrors to a gRPC InvalidArgument
// status with a BadRequest detail (one field violation per issue: field = Path,
// description = Message plus Code) and to an HTTP 400 problem body.
// Producers must return all issues rather than the first.
type ValidationErrors struct {
	issues []ValidationIssue
}

// Add appends a new issue to the collector, preserving insertion order.
func (v *ValidationErrors) Add(path string, code ErrorCode, message string) {
	v.issues = append(v.issues, ValidationIssue{
		Path:    path,
		Code:    code,
		Message: message,
	})
}

// AddIssue appends an existing ValidationIssue to the collector.
func (v *ValidationErrors) AddIssue(issue ValidationIssue) {
	v.issues = append(v.issues, issue)
}

// Merge adds all issues from another ValidationErrors instance into this one.
// It is nil-safe.
func (v *ValidationErrors) Merge(other *ValidationErrors) {
	if other != nil && len(other.issues) > 0 {
		v.issues = append(v.issues, other.issues...)
	}
}

// Len returns the number of collected issues. Safe for nil receivers.
func (v *ValidationErrors) Len() int {
	if v == nil {
		return 0
	}
	return len(v.issues)
}

// Issues returns a fresh copy of the collected issues. Safe for nil receivers.
func (v *ValidationErrors) Issues() []ValidationIssue {
	if v == nil || len(v.issues) == 0 {
		return nil
	}
	cp := make([]ValidationIssue, len(v.issues))
	copy(cp, v.issues)
	return cp
}

// Err returns nil if there are no issues, otherwise it returns the ValidationErrors itself.
// This allows callers to write "return v.Err()" and avoid returning a typed-nil error.
func (v *ValidationErrors) Err() error {
	if v == nil || len(v.issues) == 0 {
		return nil
	}
	return v
}

// Error returns a summary string of the validation errors.
// It lists up to the first 5 issues and appends "... and N more" if there are more.
func (v *ValidationErrors) Error() string {
	if v == nil || len(v.issues) == 0 {
		return "0 validation errors"
	}

	count := len(v.issues)
	msg := fmt.Sprintf("%d validation errors: ", count)

	limit := count
	if limit > 5 {
		limit = 5
	}

	for i := 0; i < limit; i++ {
		if i > 0 {
			msg += "; "
		}
		msg += v.issues[i].Error()
	}

	if count > 5 {
		msg += fmt.Sprintf(" ... and %d more", count-5)
	}

	return msg
}

// Unwrap returns a slice of all issues as individual errors, allowing errors.As to work.
// It returns a slice of *ValidationIssue pointers.
func (v *ValidationErrors) Unwrap() []error {
	if v == nil || len(v.issues) == 0 {
		return nil
	}
	errs := make([]error, len(v.issues))
	for i, issue := range v.issues {
		// Store pointers to a copy so errors.As with *ValidationIssue works.
		p := issue
		errs[i] = &p
	}
	return errs
}

// Is returns true if the target error is ErrInvalidDefinition.
func (v *ValidationErrors) Is(target error) bool {
	return target == ErrInvalidDefinition
}

// MarshalJSON customizes the JSON representation of ValidationErrors.
// It uses a value receiver so that marshalling a value does not emit an empty object.
// A nil pointer will be serialized as `null` by the encoding/json package before calling this.
func (v ValidationErrors) MarshalJSON() ([]byte, error) {
	if len(v.issues) == 0 {
		return []byte(`{"errors":[]}`), nil
	}

	wrapper := struct {
		Errors []ValidationIssue `json:"errors"`
	}{
		Errors: v.issues,
	}
	return json.Marshal(wrapper)
}
