package engine

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorCode_Valid(t *testing.T) {
	codes := []ErrorCode{
		CodeInvalidFormat, CodeUnknownField, CodeMissingRequired, CodeInvalidType,
		CodeInvalidValue, CodeUnsupportedVersion, CodeDuplicateID, CodeDanglingEdge,
		CodeSelfLoop, CodeCycle, CodeNoRoot, CodeNoLeaf, CodeUnknownNodeType,
		CodeInvalidNodeConfig, CodeUnresolvedReference, CodeInvalidCondition,
	}

	// Make sure we have exactly 16 codes
	if len(codes) != 16 {
		t.Fatalf("expected exactly 16 error codes, got %d", len(codes))
	}

	for _, code := range codes {
		if !code.Valid() {
			t.Errorf("ErrorCode(%q).Valid() returned false, expected true", code)
		}
	}

	// Test invalid code
	invalidCode := ErrorCode("invalid_code_xyz")
	if invalidCode.Valid() {
		t.Errorf("ErrorCode(%q).Valid() returned true, expected false", invalidCode)
	}
}

func TestValidationIssue_Error(t *testing.T) {
	issue := ValidationIssue{
		Path:    "$.nodes[0]",
		Code:    CodeMissingRequired,
		Message: "missing id",
	}
	expected := "$.nodes[0]: missing id (missing_required)"
	if issue.Error() != expected {
		t.Errorf("ValidationIssue.Error() = %q, want %q", issue.Error(), expected)
	}
}

func TestValidationErrors_Err(t *testing.T) {
	// Nil receiver
	var v *ValidationErrors
	if v.Err() != nil {
		t.Errorf("nil ValidationErrors.Err() should be nil, got %v", v.Err())
	}

	// Empty ValidationErrors
	v2 := &ValidationErrors{}
	if v2.Err() != nil {
		t.Errorf("empty ValidationErrors.Err() should be nil, got %v", v2.Err())
	}

	// Typed nil pitfall test
	err := v.Err() // typed error, statically typed as error interface based on return type
	if err != nil {
		t.Errorf("expected err variable to be exactly nil, not typed-nil. got: %T %v", err, err)
	}

	// ValidationErrors with issues
	v3 := &ValidationErrors{}
	v3.Add("$.id", CodeInvalidValue, "bad id")
	if v3.Err() == nil {
		t.Errorf("ValidationErrors with issues should not return nil from Err()")
	}
}

func TestValidationErrors_Error(t *testing.T) {
	v := &ValidationErrors{}

	// 0 issues
	if v.Error() != "0 validation errors" {
		t.Errorf("expected '0 validation errors', got %q", v.Error())
	}

	// 1 issue
	v.Add("$.id", CodeInvalidValue, "bad id")
	expected1 := "1 validation errors: $.id: bad id (invalid_value)"
	if v.Error() != expected1 {
		t.Errorf("expected %q, got %q", expected1, v.Error())
	}

	// 5 issues
	for i := 2; i <= 5; i++ {
		v.Add(fmt.Sprintf("$.nodes[%d]", i), CodeUnknownNodeType, "bad type")
	}
	msg5 := v.Error()
	if msg5 == "0 validation errors" || len(msg5) < 50 {
		t.Errorf("unexpected 5-issue Error() string: %q", msg5)
	}

	// 6 issues (truncation)
	v.Add("$.edges[0]", CodeDanglingEdge, "dangling")
	msg6 := v.Error()
	expectedEnd := " ... and 1 more"
	if msg6[len(msg6)-len(expectedEnd):] != expectedEnd {
		t.Errorf("expected Error() to end with %q, got %q", expectedEnd, msg6)
	}
}

func TestValidationErrors_LenAndIssues(t *testing.T) {
	var v *ValidationErrors
	if v.Len() != 0 {
		t.Errorf("nil Len() = %d, want 0", v.Len())
	}
	if v.Issues() != nil {
		t.Errorf("nil Issues() = %v, want nil", v.Issues())
	}

	v2 := &ValidationErrors{}
	v2.Add("$.id", CodeMissingRequired, "missing")
	v2.AddIssue(ValidationIssue{Path: "$.name", Code: CodeInvalidType, Message: "bad type"})

	if v2.Len() != 2 {
		t.Errorf("Len() = %d, want 2", v2.Len())
	}

	issues := v2.Issues()
	if len(issues) != 2 {
		t.Fatalf("Issues() returned %d issues, want 2", len(issues))
	}

	// Ensure Issues() returns a copy
	issues[0].Message = "mutated"
	if v2.issues[0].Message == "mutated" {
		t.Errorf("Issues() leaked internal slice reference")
	}
}

func TestValidationErrors_Merge(t *testing.T) {
	v1 := &ValidationErrors{}
	v1.Add("$.a", CodeUnknownField, "a")

	// Merge nil
	v1.Merge(nil)
	if v1.Len() != 1 {
		t.Errorf("after Merge(nil), Len() = %d, want 1", v1.Len())
	}

	// Merge empty
	v2 := &ValidationErrors{}
	v1.Merge(v2)
	if v1.Len() != 1 {
		t.Errorf("after Merge(empty), Len() = %d, want 1", v1.Len())
	}

	// Merge with issues
	v3 := &ValidationErrors{}
	v3.Add("$.b", CodeUnknownField, "b")
	v1.Merge(v3)
	if v1.Len() != 2 {
		t.Errorf("after Merge, Len() = %d, want 2", v1.Len())
	}
}

func TestValidationErrors_IsAsUnwrap(t *testing.T) {
	v := &ValidationErrors{}
	v.Add("$.id", CodeInvalidValue, "bad")

	var err error = v

	// Test Is
	if !errors.Is(err, ErrInvalidDefinition) {
		t.Errorf("errors.Is(err, ErrInvalidDefinition) returned false")
	}

	// Wrap the error
	wrapped := fmt.Errorf("wrapper: %w", err)
	if !errors.Is(wrapped, ErrInvalidDefinition) {
		t.Errorf("errors.Is(wrapped, ErrInvalidDefinition) returned false")
	}

	// Test As with ValidationIssue
	var issue ValidationIssue
	if !errors.As(wrapped, &issue) {
		t.Fatalf("errors.As(wrapped, &ValidationIssue) returned false")
	}
	if issue.Path != "$.id" {
		t.Errorf("errors.As extracted wrong issue, Path = %q", issue.Path)
	}

	// Test As with *ValidationErrors
	var vAs *ValidationErrors
	if !errors.As(wrapped, &vAs) {
		t.Fatalf("errors.As(wrapped, &*ValidationErrors) returned false")
	}
	if vAs.Len() != 1 {
		t.Errorf("errors.As extracted wrong *ValidationErrors")
	}

	// Unwrap nil
	var nilV *ValidationErrors
	if nilV.Unwrap() != nil {
		t.Errorf("nilV.Unwrap() should be nil")
	}
}

func TestValidationErrors_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		v        *ValidationErrors
		expected string
	}{
		{
			name:     "nil receiver",
			v:        nil,
			expected: `{"errors":[]}`,
		},
		{
			name:     "empty receiver",
			v:        &ValidationErrors{},
			expected: `{"errors":[]}`,
		},
		{
			name: "with issues",
			v: func() *ValidationErrors {
				v := &ValidationErrors{}
				v.Add("$.id", CodeInvalidValue, "bad \"id\"")
				v.Add("$.nodes[0]", CodeMissingRequired, "missing type")
				return v
			}(),
			expected: `{"errors":[{"path":"$.id","code":"invalid_value","message":"bad \"id\""},{"path":"$.nodes[0]","code":"missing_required","message":"missing type"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := tt.v.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON error = %v", err)
			}
			if string(b) != tt.expected {
				t.Errorf("MarshalJSON = %s\nwant = %s", string(b), tt.expected)
			}
		})
	}
}
