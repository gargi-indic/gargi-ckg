package engine

import (
	"errors"
	"strings"
	"testing"
)

func createTestGraph(t *testing.T) *Graph {
	t.Helper()
	n1, _ := NewNode("node1", "testType", nil)
	n2, _ := NewNode("node2", "testType", nil)
	e1, _ := NewEdge("node1", "node2", "", nil)
	g, err := NewGraph("graph1", []*Node{n1, n2}, []*Edge{e1})
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	return g
}

func TestNewGraphState(t *testing.T) {
	g := createTestGraph(t)
	execID, _ := NewExecutionID()

	state, err := NewGraphState(execID, g)
	if err != nil {
		t.Fatalf("NewGraphState unexpected error: %v", err)
	}

	if state.ExecutionID() != execID {
		t.Errorf("expected exec ID %s, got %s", execID, state.ExecutionID())
	}
	if state.GraphID() != g.ID() {
		t.Errorf("expected graph ID %s, got %s", g.ID(), state.GraphID())
	}
	if state.Status() != StatusPending {
		t.Errorf("expected status %s, got %s", StatusPending, state.Status())
	}
	if state.Version() != 0 {
		t.Errorf("expected version 0, got %d", state.Version())
	}

	nodes := state.NodeStates()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	for _, n := range nodes {
		if n.Status() != StatusPending {
			t.Errorf("node %s expected status Pending, got %s", n.NodeID(), n.Status())
		}
		if n.Version() != 0 {
			t.Errorf("node %s expected version 0, got %d", n.NodeID(), n.Version())
		}
		if len(n.Output()) != 0 {
			t.Errorf("node %s expected empty output, got %v", n.NodeID(), n.Output())
		}
	}

	// test invalid args
	_, err = NewGraphState("", g)
	if err == nil || !strings.Contains(err.Error(), "execution ID cannot be empty") {
		t.Errorf("expected error for empty execution ID, got %v", err)
	}

	_, err = NewGraphState(execID, nil)
	if err == nil || !strings.Contains(err.Error(), "graph cannot be nil") {
		t.Errorf("expected error for nil graph, got %v", err)
	}
}

func TestGraphState_Apply(t *testing.T) {
	g := createTestGraph(t)
	execID, _ := NewExecutionID()
	state, _ := NewGraphState(execID, g)

	newStatus := StatusReady
	errMsg := "some error"

	m1 := NewStateMutation("node1", 0, &newStatus, map[string]any{"k1": "v1"}, nil, &errMsg)

	newState, err := state.Apply(m1)
	if err != nil {
		t.Fatalf("Apply unexpected error: %v", err)
	}

	// check new state
	if newState.Version() != 1 {
		t.Errorf("expected new state version 1, got %d", newState.Version())
	}
	newNode1 := newState.NodeState("node1")
	if newNode1.Status() != StatusReady {
		t.Errorf("expected node1 status Ready, got %s", newNode1.Status())
	}
	if newNode1.Version() != 1 {
		t.Errorf("expected node1 version 1, got %d", newNode1.Version())
	}
	if newNode1.ErrorMessage() != errMsg {
		t.Errorf("expected node1 error msg %s, got %s", errMsg, newNode1.ErrorMessage())
	}
	if newNode1.Output()["k1"] != "v1" {
		t.Errorf("expected node1 output k1=v1, got %v", newNode1.Output()["k1"])
	}

	// verify receiver is unchanged
	if state.Version() != 0 {
		t.Errorf("expected old state version 0, got %d", state.Version())
	}
	oldNode1 := state.NodeState("node1")
	if oldNode1.Status() != StatusPending {
		t.Errorf("expected old node1 status Pending, got %s", oldNode1.Status())
	}
	if oldNode1.Version() != 0 {
		t.Errorf("expected old node1 version 0, got %d", oldNode1.Version())
	}
	if len(oldNode1.Output()) != 0 {
		t.Errorf("expected old node1 empty output, got %v", oldNode1.Output())
	}
	if oldNode1.ErrorMessage() != "" {
		t.Errorf("expected old node1 empty error msg, got %s", oldNode1.ErrorMessage())
	}

	// verify map copying is not aliasing
	state.nodeStates["node1"] = nil // corrupt old state map
	if newState.NodeState("node1") == nil {
		t.Errorf("new state's nodeMap was aliased with old state")
	}

	// Test Unset
	m2 := NewStateMutation("node1", 1, nil, nil, []string{"k1"}, nil)
	newState2, err := newState.Apply(m2)
	if err != nil {
		t.Fatalf("Apply unexpected error: %v", err)
	}
	if _, exists := newState2.NodeState("node1").Output()["k1"]; exists {
		t.Errorf("expected k1 to be unset")
	}

	// Test ErrVersionConflict
	mConflict := NewStateMutation("node1", 0, &newStatus, nil, nil, nil)
	_, err = newState.Apply(mConflict)
	if !errors.Is(err, ErrVersionConflict) {
		t.Errorf("expected ErrVersionConflict, got %v", err)
	}

	// Test ErrUnknownNode
	mUnknown := NewStateMutation("unknown", 1, &newStatus, nil, nil, nil)
	_, err = newState.Apply(mUnknown)
	if !errors.Is(err, ErrUnknownNode) {
		t.Errorf("expected ErrUnknownNode, got %v", err)
	}

	// Test ErrInvalidTransition
	invalidStatus := StatusCompensated // Pending -> Compensated is invalid
	mInvalidTrans := NewStateMutation("node2", 1, &invalidStatus, nil, nil, nil)
	_, err = newState.Apply(mInvalidTrans)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}

	// Test ErrInvalidMutation
	mInvalid := NewStateMutation("node1", 1, nil, nil, nil, nil)
	_, err = newState.Apply(mInvalid)
	if !errors.Is(err, ErrInvalidMutation) {
		t.Errorf("expected ErrInvalidMutation, got %v", err)
	}
}

func TestGraphState_Apply_Immutability(t *testing.T) {
	g := createTestGraph(t)
	execID, _ := NewExecutionID()
	state, _ := NewGraphState(execID, g)

	nestedMap := map[string]any{"nested_key": "nested_value"}
	newStatus := StatusReady
	m := NewStateMutation("node1", 0, &newStatus, map[string]any{"nested": nestedMap}, nil, nil)

	newState, err := state.Apply(m)
	if err != nil {
		t.Fatalf("Apply unexpected error: %v", err)
	}

	// Mutate input map
	nestedMap["nested_key"] = "changed"

	// Verify state wasn't affected
	output := newState.NodeState("node1").Output()
	outputNested := output["nested"].(map[string]any)
	if outputNested["nested_key"] != "nested_value" {
		t.Errorf("state was mutated via input reference")
	}

	// Mutate retrieved output
	outputNested["nested_key"] = "changed2"

	// Verify state wasn't affected again
	output2 := newState.NodeState("node1").Output()
	outputNested2 := output2["nested"].(map[string]any)
	if outputNested2["nested_key"] != "nested_value" {
		t.Errorf("state was mutated via retrieved output reference")
	}
}

func TestGraphState_WithStatus(t *testing.T) {
	g := createTestGraph(t)
	execID, _ := NewExecutionID()
	state, _ := NewGraphState(execID, g)

	newState, err := state.WithStatus(0, StatusReady)
	if err != nil {
		t.Fatalf("WithStatus unexpected error: %v", err)
	}

	if newState.Status() != StatusReady {
		t.Errorf("expected status Ready, got %s", newState.Status())
	}
	if newState.Version() != 1 {
		t.Errorf("expected version 1, got %d", newState.Version())
	}

	// Verify receiver unchanged
	if state.Status() != StatusPending {
		t.Errorf("expected old state status Pending, got %s", state.Status())
	}
	if state.Version() != 0 {
		t.Errorf("expected old state version 0, got %d", state.Version())
	}

	// Test ErrVersionConflict
	_, err = newState.WithStatus(0, StatusRunning)
	if !errors.Is(err, ErrVersionConflict) {
		t.Errorf("expected ErrVersionConflict, got %v", err)
	}

	// Test ErrInvalidTransition
	_, err = newState.WithStatus(1, StatusCompensated)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}
