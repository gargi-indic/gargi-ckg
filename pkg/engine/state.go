package engine

import (
	"errors"
	"fmt"
)

var (
	// ErrVersionConflict is returned when a state update is attempted with a stale expected version.
	ErrVersionConflict = errors.New("version conflict: expected version does not match current state version")
	// ErrUnknownNode is returned when a node ID is not found in the graph state.
	ErrUnknownNode = errors.New("unknown node ID")
	// ErrInvalidTransition is returned when a state transition is not allowed.
	ErrInvalidTransition = errors.New("invalid status transition")
	// ErrInvalidMutation is returned when a mutation patch is invalid.
	ErrInvalidMutation = errors.New("invalid state mutation")
)

// NodeState represents the execution state of a single node within a graph execution.
// It is strictly immutable.
type NodeState struct {
	nodeID       NodeID
	status       Status
	output       map[string]any
	errorMessage string
	version      uint64
}

// NodeID returns the identifier of the node.
func (n *NodeState) NodeID() NodeID {
	return n.nodeID
}

// Status returns the current execution status of the node.
func (n *NodeState) Status() Status {
	return n.status
}

// Output returns a deep copy of the node's output map.
func (n *NodeState) Output() map[string]any {
	return deepCopyMap(n.output)
}

// ErrorMessage returns the node's error message, or empty string if none.
func (n *NodeState) ErrorMessage() string {
	return n.errorMessage
}

// Version returns the current version of the node's state.
func (n *NodeState) Version() uint64 {
	return n.version
}

// GraphState represents the execution state of an entire graph.
// It is strictly immutable and acts as a point-in-time snapshot.
type GraphState struct {
	executionID ExecutionID
	graphID     string
	status      Status
	nodes       []NodeID // Ordered list of nodes
	nodeStates  map[NodeID]*NodeState
	version     uint64
}

// NewGraphState creates a new GraphState for a given execution and graph.
// All nodes are initialized to StatusPending with an empty output and version 0.
// The GraphState itself starts at version 0 and StatusPending.
// The provided graph is used only to initialize the state and is not retained.
func NewGraphState(execID ExecutionID, g *Graph) (*GraphState, error) {
	if execID == "" {
		return nil, errors.New("execution ID cannot be empty")
	}
	if g == nil {
		return nil, errors.New("graph cannot be nil")
	}

	nodesList := g.Nodes()
	nodes := make([]NodeID, 0, len(nodesList))
	nodeStates := make(map[NodeID]*NodeState, len(nodesList))

	for _, n := range nodesList {
		id := n.ID()
		nodes = append(nodes, id)
		nodeStates[id] = &NodeState{
			nodeID:  id,
			status:  StatusPending,
			output:  make(map[string]any),
			version: 0,
		}
	}

	return &GraphState{
		executionID: execID,
		graphID:     g.ID(),
		status:      StatusPending,
		nodes:       nodes,
		nodeStates:  nodeStates,
		version:     0,
	}, nil
}

// ExecutionID returns the unique identifier for this graph execution.
func (g *GraphState) ExecutionID() ExecutionID {
	return g.executionID
}

// GraphID returns the identifier of the graph definition being executed.
func (g *GraphState) GraphID() string {
	return g.graphID
}

// Status returns the current execution status of the graph.
func (g *GraphState) Status() Status {
	return g.status
}

// Version returns the current monotonic version of the graph state.
func (g *GraphState) Version() uint64 {
	return g.version
}

// NodeState returns the state of a specific node, or nil if not found.
func (g *GraphState) NodeState(id NodeID) *NodeState {
	return g.nodeStates[id]
}

// NodeStates returns a fresh slice of node states in their insertion order.
func (g *GraphState) NodeStates() []*NodeState {
	states := make([]*NodeState, 0, len(g.nodes))
	for _, id := range g.nodes {
		states = append(states, g.nodeStates[id])
	}
	return states
}

// Apply applies a StateMutation to the GraphState and returns a new GraphState.
// It implements optimistic concurrency control using m.ExpectedVersion against the GraphState's version.
// On success, both the GraphState's version and the updated NodeState's version are incremented by 1.
// The receiver GraphState is left entirely unchanged.
func (g *GraphState) Apply(m StateMutation) (*GraphState, error) {
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMutation, err)
	}

	if m.ExpectedVersion != g.version {
		return nil, fmt.Errorf("%w: graph version %d, expected %d", ErrVersionConflict, g.version, m.ExpectedVersion)
	}

	oldNodeState, exists := g.nodeStates[m.NodeID]
	if !exists {
		return nil, fmt.Errorf("%w: node %q", ErrUnknownNode, m.NodeID)
	}

	if m.Status != nil {
		if !CanTransition(oldNodeState.status, *m.Status) {
			return nil, fmt.Errorf("%w: node %q cannot transition from %s to %s", ErrInvalidTransition, m.NodeID, oldNodeState.status, *m.Status)
		}
	}

	// Create new node state
	newNodeState := &NodeState{
		nodeID:       oldNodeState.nodeID,
		status:       oldNodeState.status,
		output:       oldNodeState.Output(), // deep copy
		errorMessage: oldNodeState.errorMessage,
		version:      oldNodeState.version + 1,
	}

	if m.Status != nil {
		newNodeState.status = *m.Status
	}

	if m.ErrorMessage != nil {
		newNodeState.errorMessage = *m.ErrorMessage
	}

	for k, v := range m.Set {
		newNodeState.output[k] = deepCopyAny(v)
	}

	for _, k := range m.Unset {
		delete(newNodeState.output, k)
	}

	// Create new graph state
	newNodeStates := make(map[NodeID]*NodeState, len(g.nodeStates))
	for k, v := range g.nodeStates {
		newNodeStates[k] = v
	}
	newNodeStates[m.NodeID] = newNodeState

	// new node list
	newNodes := make([]NodeID, len(g.nodes))
	copy(newNodes, g.nodes)

	return &GraphState{
		executionID: g.executionID,
		graphID:     g.graphID,
		status:      g.status,
		nodes:       newNodes,
		nodeStates:  newNodeStates,
		version:     g.version + 1,
	}, nil
}

// WithStatus returns a new GraphState with the specified status.
// It implements optimistic concurrency control using expectedVersion against the GraphState's version.
// On success, the GraphState's version is incremented by 1.
// The receiver GraphState and its node states are left entirely unchanged.
func (g *GraphState) WithStatus(expectedVersion uint64, to Status) (*GraphState, error) {
	if expectedVersion != g.version {
		return nil, fmt.Errorf("%w: graph version %d, expected %d", ErrVersionConflict, g.version, expectedVersion)
	}

	if !CanTransition(g.status, to) {
		return nil, fmt.Errorf("%w: graph cannot transition from %s to %s", ErrInvalidTransition, g.status, to)
	}

	newNodeStates := make(map[NodeID]*NodeState, len(g.nodeStates))
	for k, v := range g.nodeStates {
		newNodeStates[k] = v
	}

	newNodes := make([]NodeID, len(g.nodes))
	copy(newNodes, g.nodes)

	return &GraphState{
		executionID: g.executionID,
		graphID:     g.graphID,
		status:      to,
		nodes:       newNodes,
		nodeStates:  newNodeStates,
		version:     g.version + 1,
	}, nil
}
