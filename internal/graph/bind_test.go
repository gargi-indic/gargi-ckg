package graph

import (
	"testing"
)

func TestBindDefinitionErrors(t *testing.T) {
	// Simple tests validating binding error checks
	_, err := bindDefinition(nil)
	if err == nil {
		t.Error("expected error for nil tree")
	}

	_, err = bindDefinition("string")
	if err == nil {
		t.Error("expected error for non-object tree")
	}
}
