package engine

import (
	"errors"
	"fmt"
)

// Graph represents a directed acyclic graph of execution nodes.
// Once created via NewGraph, the Graph and its components are immutable.
type Graph struct {
	id    string
	nodes []*Node
	edges []*Edge
}

// NewGraph creates a new immutable Graph instance.
// It performs validation on the provided nodes and edges:
// - Graph ID must not be empty.
// - Nodes and Edges cannot be nil or contain nil/invalid elements.
// - Node IDs must be unique.
// - Edges must reference existing Node IDs.
// - Duplicate edges (same from and to) are not allowed.
//
// Note: Verification of acyclicity and topological ordering is explicitly
// out of scope for this constructor and should be handled by validation layers.
func NewGraph(id string, nodes []*Node, edges []*Edge) (*Graph, error) {
	if id == "" {
		return nil, errors.New("graph ID cannot be empty")
	}

	nodeMap := make(map[NodeID]struct{})
	cpNodes := make([]*Node, 0, len(nodes))

	for i, n := range nodes {
		if n == nil {
			return nil, fmt.Errorf("node at index %d is nil", i)
		}
		if n.id == "" || n.nodeType == "" {
			return nil, fmt.Errorf("node at index %d is invalid (missing ID or Type)", i)
		}
		if _, exists := nodeMap[n.id]; exists {
			return nil, fmt.Errorf("duplicate node ID found: %s", n.id)
		}
		nodeMap[n.id] = struct{}{}

		// Create a deep copy of the node to ensure immutability
		cpNode, _ := NewNode(n.id, n.nodeType, n.config) // Error impossible here due to previous checks
		cpNodes = append(cpNodes, cpNode)
	}

	type edgeKey struct{ from, to NodeID }
	edgeMap := make(map[edgeKey]struct{})
	cpEdges := make([]*Edge, 0, len(edges))

	for i, e := range edges {
		if e == nil {
			return nil, fmt.Errorf("edge at index %d is nil", i)
		}
		if e.from == "" || e.to == "" || e.from == e.to {
			return nil, fmt.Errorf("edge at index %d is invalid (missing endpoints or is a self-loop)", i)
		}

		if _, exists := nodeMap[e.from]; !exists {
			return nil, fmt.Errorf("edge at index %d references unknown source node: %s", i, e.from)
		}
		if _, exists := nodeMap[e.to]; !exists {
			return nil, fmt.Errorf("edge at index %d references unknown target node: %s", i, e.to)
		}

		key := edgeKey{e.from, e.to}
		if _, exists := edgeMap[key]; exists {
			return nil, fmt.Errorf("duplicate edge found from %s to %s", e.from, e.to)
		}
		edgeMap[key] = struct{}{}

		// Create a defensive copy of the edge
		cpEdge, _ := NewEdge(e.from, e.to, e.condition, e.dataMapping) // Error impossible here due to previous checks
		cpEdges = append(cpEdges, cpEdge)
	}

	return &Graph{
		id:    id,
		nodes: cpNodes,
		edges: cpEdges,
	}, nil
}

// ID returns the graph's identifier.
func (g *Graph) ID() string {
	return g.id
}

// Nodes returns a copy of the graph's nodes in insertion order.
// The returned nodes themselves are immutable.
func (g *Graph) Nodes() []*Node {
	cp := make([]*Node, len(g.nodes))
	for i, n := range g.nodes {
		cp[i], _ = NewNode(n.id, n.nodeType, n.config)
	}
	return cp
}

// Edges returns a copy of the graph's edges.
// The returned edges themselves are immutable.
func (g *Graph) Edges() []*Edge {
	cp := make([]*Edge, len(g.edges))
	for i, e := range g.edges {
		cp[i], _ = NewEdge(e.from, e.to, e.condition, e.dataMapping)
	}
	return cp
}

// Node returns the node with the specified ID, or nil if not found.
// The returned node is a copy.
func (g *Graph) Node(id NodeID) *Node {
	for _, n := range g.nodes {
		if n.id == id {
			cp, _ := NewNode(n.id, n.nodeType, n.config)
			return cp
		}
	}
	return nil
}

// OutgoingEdges returns a list of edges originating from the specified node ID.
// The edges are returned in the order they were provided during graph creation.
// The returned edges are copies.
func (g *Graph) OutgoingEdges(id NodeID) []*Edge {
	var result []*Edge
	for _, e := range g.edges {
		if e.from == id {
			cp, _ := NewEdge(e.from, e.to, e.condition, e.dataMapping)
			result = append(result, cp)
		}
	}
	return result
}

// IncomingEdges returns a list of edges pointing to the specified node ID.
// The edges are returned in the order they were provided during graph creation.
// The returned edges are copies.
func (g *Graph) IncomingEdges(id NodeID) []*Edge {
	var result []*Edge
	for _, e := range g.edges {
		if e.to == id {
			cp, _ := NewEdge(e.from, e.to, e.condition, e.dataMapping)
			result = append(result, cp)
		}
	}
	return result
}
