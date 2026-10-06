package engine

import (
	"errors"
	"fmt"
)

// Graph represents a directed graph (acyclicity is validated separately).
// Once created via NewGraph, the Graph and its components are immutable.
type Graph struct {
	id       string
	nodes    []*Node
	edges    []*Edge
	nodeMap  map[NodeID]*Node
	outgoing map[NodeID][]*Edge
	incoming map[NodeID][]*Edge
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

	nodeMap := make(map[NodeID]*Node)
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
		nodeMap[n.id] = n
		cpNodes = append(cpNodes, n)
	}

	type edgeKey struct{ from, to NodeID }
	edgeMap := make(map[edgeKey]struct{})
	cpEdges := make([]*Edge, 0, len(edges))
	outgoing := make(map[NodeID][]*Edge)
	incoming := make(map[NodeID][]*Edge)

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

		cpEdges = append(cpEdges, e)
		outgoing[e.from] = append(outgoing[e.from], e)
		incoming[e.to] = append(incoming[e.to], e)
	}

	return &Graph{
		id:       id,
		nodes:    cpNodes,
		edges:    cpEdges,
		nodeMap:  nodeMap,
		outgoing: outgoing,
		incoming: incoming,
	}, nil
}

// ID returns the graph's identifier.
func (g *Graph) ID() string {
	return g.id
}

// Nodes returns a fresh slice of the graph's nodes in insertion order.
// The returned nodes themselves are immutable pointers.
func (g *Graph) Nodes() []*Node {
	cp := make([]*Node, len(g.nodes))
	copy(cp, g.nodes)
	return cp
}

// Edges returns a fresh slice of the graph's edges.
// The returned edges themselves are immutable pointers.
func (g *Graph) Edges() []*Edge {
	cp := make([]*Edge, len(g.edges))
	copy(cp, g.edges)
	return cp
}

// Node returns the node with the specified ID, or nil if not found.
func (g *Graph) Node(id NodeID) *Node {
	return g.nodeMap[id]
}

// OutgoingEdges returns a fresh slice of edges originating from the specified node ID.
// The edges are returned in the order they were provided during graph creation.
func (g *Graph) OutgoingEdges(id NodeID) []*Edge {
	edges := g.outgoing[id]
	cp := make([]*Edge, len(edges))
	copy(cp, edges)
	return cp
}

// IncomingEdges returns a fresh slice of edges pointing to the specified node ID.
// The edges are returned in the order they were provided during graph creation.
func (g *Graph) IncomingEdges(id NodeID) []*Edge {
	edges := g.incoming[id]
	cp := make([]*Edge, len(edges))
	copy(cp, edges)
	return cp
}
