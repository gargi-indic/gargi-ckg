package graph

import (
	"container/heap"
	"errors"
	"fmt"
	"sort"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

// ErrCycle is returned when a cycle is detected during topological sorting.
var ErrCycle = errors.New("cycle detected in graph")

// CycleError represents an error containing the cycle path found in the graph.
type CycleError struct {
	Path []engine.NodeID
}

// Error implements the error interface.
func (e *CycleError) Error() string {
	return fmt.Sprintf("%v: %v", ErrCycle, e.Path)
}

// Unwrap allows errors.Is to work with ErrCycle.
func (e *CycleError) Unwrap() error {
	return ErrCycle
}

// Roots returns the NodeIDs of all nodes that have no incoming edges, in insertion order.
func Roots(g *engine.Graph) []engine.NodeID {
	var roots []engine.NodeID
	for _, n := range g.Nodes() {
		id := n.ID()
		if len(g.IncomingEdges(id)) == 0 {
			roots = append(roots, id)
		}
	}
	return roots
}

// Leaves returns the NodeIDs of all nodes that have no outgoing edges, in insertion order.
func Leaves(g *engine.Graph) []engine.NodeID {
	var leaves []engine.NodeID
	for _, n := range g.Nodes() {
		id := n.ID()
		if len(g.OutgoingEdges(id)) == 0 {
			leaves = append(leaves, id)
		}
	}
	return leaves
}

// Upstream returns the unique, sorted transitive ancestors of the given node ID.
// It returns an error if the given node ID does not exist in the graph.
func Upstream(g *engine.Graph, id engine.NodeID) ([]engine.NodeID, error) {
	if g.Node(id) == nil {
		return nil, fmt.Errorf("unknown node ID: %s", id)
	}

	visited := make(map[engine.NodeID]bool)
	var result []engine.NodeID

	var stack []engine.NodeID
	// Add immediate parents to stack
	for _, edge := range g.IncomingEdges(id) {
		from := edge.From()
		if !visited[from] {
			visited[from] = true
			stack = append(stack, from)
		}
	}

	for len(stack) > 0 {
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		result = append(result, curr)

		for _, edge := range g.IncomingEdges(curr) {
			from := edge.From()
			if !visited[from] {
				visited[from] = true
				stack = append(stack, from)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i] < result[j]
	})

	return result, nil
}

// Downstream returns the unique, sorted transitive descendants of the given node ID.
// It returns an error if the given node ID does not exist in the graph.
func Downstream(g *engine.Graph, id engine.NodeID) ([]engine.NodeID, error) {
	if g.Node(id) == nil {
		return nil, fmt.Errorf("unknown node ID: %s", id)
	}

	visited := make(map[engine.NodeID]bool)
	var result []engine.NodeID

	var stack []engine.NodeID
	// Add immediate children to stack
	for _, edge := range g.OutgoingEdges(id) {
		to := edge.To()
		if !visited[to] {
			visited[to] = true
			stack = append(stack, to)
		}
	}

	for len(stack) > 0 {
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		result = append(result, curr)

		for _, edge := range g.OutgoingEdges(curr) {
			to := edge.To()
			if !visited[to] {
				visited[to] = true
				stack = append(stack, to)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i] < result[j]
	})

	return result, nil
}

type dfsFrame struct {
	node      engine.NodeID
	edgeIndex int
}

// FindCycle detects if the graph contains a cycle using an iterative DFS approach.
// It returns the offending cycle path (where first == last) or nil if acyclic.
func FindCycle(g *engine.Graph) []engine.NodeID {
	// 0 = unvisited, 1 = visiting, 2 = visited
	state := make(map[engine.NodeID]int)
	nodes := g.Nodes()

	for _, n := range nodes {
		startNode := n.ID()
		if state[startNode] == 0 {
			var stack []dfsFrame
			stack = append(stack, dfsFrame{node: startNode, edgeIndex: 0})
			state[startNode] = 1 // visiting

			for len(stack) > 0 {
				topIdx := len(stack) - 1
				frame := &stack[topIdx]
				curr := frame.node
				edges := g.OutgoingEdges(curr)

				if frame.edgeIndex < len(edges) {
					// There are still outgoing edges to explore
					nextEdge := edges[frame.edgeIndex]
					nextNode := nextEdge.To()
					frame.edgeIndex++

					switch state[nextNode] {
					case 0:
						// Unvisited, push to stack
						state[nextNode] = 1 // visiting
						stack = append(stack, dfsFrame{node: nextNode, edgeIndex: 0})
					case 1:
						// Cycle detected!
						// Extract cycle path from the stack.
						var cycle []engine.NodeID
						inCycle := false
						for _, f := range stack {
							if f.node == nextNode {
								inCycle = true
							}
							if inCycle {
								cycle = append(cycle, f.node)
							}
						}
						// Close the cycle
						cycle = append(cycle, nextNode)
						return cycle
					}
				} else {
					// No more edges to explore, pop from stack and mark as visited
					state[curr] = 2 // visited
					stack = stack[:topIdx]
				}
			}
		}
	}

	return nil
}

// A min-heap of NodeIDs for lexicographical sorting
type nodeIDHeap []engine.NodeID

func (h nodeIDHeap) Len() int           { return len(h) }
func (h nodeIDHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h nodeIDHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *nodeIDHeap) Push(x any) {
	*h = append(*h, x.(engine.NodeID))
}

func (h *nodeIDHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// TopologicalSort performs a topological sort on the graph using Kahn's algorithm.
// Ties are broken lexicographically by NodeID for deterministic output.
// Returns an error (wrapping ErrCycle) if the graph contains a cycle.
func TopologicalSort(g *engine.Graph) ([]engine.NodeID, error) {
	inDegree := make(map[engine.NodeID]int)
	nodes := g.Nodes()
	for _, n := range nodes {
		inDegree[n.ID()] = 0
	}

	for _, n := range nodes {
		id := n.ID()
		for _, e := range g.OutgoingEdges(id) {
			inDegree[e.To()]++
		}
	}

	h := &nodeIDHeap{}
	heap.Init(h)

	for id, deg := range inDegree {
		if deg == 0 {
			heap.Push(h, id)
		}
	}

	var result []engine.NodeID
	for h.Len() > 0 {
		u := heap.Pop(h).(engine.NodeID)
		result = append(result, u)

		for _, e := range g.OutgoingEdges(u) {
			v := e.To()
			inDegree[v]--
			if inDegree[v] == 0 {
				heap.Push(h, v)
			}
		}
	}

	if len(result) != len(nodes) {
		cyclePath := FindCycle(g)
		return nil, &CycleError{Path: cyclePath}
	}

	return result, nil
}
