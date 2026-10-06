package engine

import (
	"errors"
)

// StateMutation represents a set of state changes emitted by a node's execution.
// It is designed to be a value type and acts as a patch to apply to a NodeState.
type StateMutation struct {
	// NodeID is the identifier of the node this mutation applies to.
	NodeID NodeID
	// ExpectedVersion is the version of the GraphState that the producer last saw,
	// used for optimistic concurrency control.
	ExpectedVersion uint64
	// Status is the optional new status for the node.
	// If nil, the status is not changed.
	Status *Status
	// Set represents a map of output keys to set or overwrite.
	// The map and its nested reference values are deep-copied during creation.
	Set map[string]any
	// Unset represents a list of output keys to remove.
	Unset []string
	// ErrorMessage is an optional new error message.
	// If nil, the error message is left unchanged.
	ErrorMessage *string
}

// NewStateMutation creates a new StateMutation, ensuring deep copies of the Set map.
func NewStateMutation(nodeID NodeID, expectedVersion uint64, status *Status, set map[string]any, unset []string, errMsg *string) StateMutation {
	var copiedUnset []string
	if unset != nil {
		copiedUnset = make([]string, len(unset))
		copy(copiedUnset, unset)
	}

	return StateMutation{
		NodeID:          nodeID,
		ExpectedVersion: expectedVersion,
		Status:          status,
		Set:             deepCopyMap(set),
		Unset:           copiedUnset,
		ErrorMessage:    errMsg,
	}
}

// Validate checks that the StateMutation is valid.
// It returns an error if:
// - NodeID is empty.
// - Status is set but invalid.
// - Keys exist in both Set and Unset.
// - Any key in Set or Unset is empty.
// - The mutation has no effect (Status is nil, Set and Unset are empty, ErrorMessage is nil).
func (m StateMutation) Validate() error {
	if m.NodeID == "" {
		return errors.New("mutation must have a non-empty NodeID")
	}
	if m.Status != nil && !m.Status.Valid() {
		return errors.New("mutation has an invalid Status")
	}

	hasEffect := m.Status != nil || len(m.Set) > 0 || len(m.Unset) > 0 || m.ErrorMessage != nil
	if !hasEffect {
		return errors.New("mutation has no effect (empty Status, Set, Unset, and ErrorMessage)")
	}

	unsetMap := make(map[string]struct{}, len(m.Unset))
	for _, k := range m.Unset {
		if k == "" {
			return errors.New("mutation has an empty key in Unset")
		}
		unsetMap[k] = struct{}{}
	}

	for k := range m.Set {
		if k == "" {
			return errors.New("mutation has an empty key in Set")
		}
		if _, exists := unsetMap[k]; exists {
			return errors.New("mutation has conflicting key in both Set and Unset: " + k)
		}
	}

	return nil
}
