package graph_test

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/graph"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func createNode(t *testing.T, id string) *engine.Node {
	n, err := engine.NewNode(engine.NodeID(id), "test-type", nil)
	if err != nil {
		t.Fatalf("failed to create node %s: %v", id, err)
	}
	return n
}

func createEdge(t *testing.T, from, to string) *engine.Edge {
	e, err := engine.NewEdge(engine.NodeID(from), engine.NodeID(to), "", nil)
	if err != nil {
		t.Fatalf("failed to create edge %s -> %s: %v", from, to, err)
	}
	return e
}

func TestRootsAndLeaves(t *testing.T) {
	n1 := createNode(t, "A")
	n2 := createNode(t, "B")
	n3 := createNode(t, "C")
	n4 := createNode(t, "D")

	e1 := createEdge(t, "A", "B")
	e2 := createEdge(t, "B", "C")
	e3 := createEdge(t, "A", "D")

	g, err := engine.NewGraph("g1", []*engine.Node{n1, n2, n3, n4}, []*engine.Edge{e1, e2, e3})
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}

	roots := graph.Roots(g)
	if len(roots) != 1 || roots[0] != "A" {
		t.Errorf("expected roots [A], got %v", roots)
	}

	leaves := graph.Leaves(g)
	if len(leaves) != 2 || leaves[0] != "C" || leaves[1] != "D" {
		t.Errorf("expected leaves [C D], got %v", leaves)
	}
}

func TestUpstreamAndDownstream(t *testing.T) {
	// A -> B -> C -> D
	// A -> E -> D
	nA := createNode(t, "A")
	nB := createNode(t, "B")
	nC := createNode(t, "C")
	nD := createNode(t, "D")
	nE := createNode(t, "E")

	e1 := createEdge(t, "A", "B")
	e2 := createEdge(t, "B", "C")
	e3 := createEdge(t, "C", "D")
	e4 := createEdge(t, "A", "E")
	e5 := createEdge(t, "E", "D")

	g, err := engine.NewGraph("g1", []*engine.Node{nA, nB, nC, nD, nE}, []*engine.Edge{e1, e2, e3, e4, e5})
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}

	// Downstream of A
	downstreamA, err := graph.Downstream(g, "A")
	if err != nil {
		t.Fatalf("Downstream failed: %v", err)
	}
	expectedDownA := []engine.NodeID{"B", "C", "D", "E"}
	if !reflect.DeepEqual(downstreamA, expectedDownA) {
		t.Errorf("expected downstream of A to be %v, got %v", expectedDownA, downstreamA)
	}

	// Upstream of D
	upstreamD, err := graph.Upstream(g, "D")
	if err != nil {
		t.Fatalf("Upstream failed: %v", err)
	}
	expectedUpD := []engine.NodeID{"A", "B", "C", "E"}
	if !reflect.DeepEqual(upstreamD, expectedUpD) {
		t.Errorf("expected upstream of D to be %v, got %v", expectedUpD, upstreamD)
	}

	// Unknown node
	_, err = graph.Upstream(g, "Z")
	if err == nil {
		t.Errorf("expected error for unknown node, got nil")
	}
	_, err = graph.Downstream(g, "Z")
	if err == nil {
		t.Errorf("expected error for unknown node, got nil")
	}
}

func TestFindCycle(t *testing.T) {
	t.Run("Acyclic", func(t *testing.T) {
		nA := createNode(t, "A")
		nB := createNode(t, "B")
		e1 := createEdge(t, "A", "B")
		g, _ := engine.NewGraph("g1", []*engine.Node{nA, nB}, []*engine.Edge{e1})

		cycle := graph.FindCycle(g)
		if cycle != nil {
			t.Errorf("expected no cycle, got %v", cycle)
		}
	})

	t.Run("SelfContainedCycle", func(t *testing.T) {
		nA := createNode(t, "A")
		nB := createNode(t, "B")
		nC := createNode(t, "C")
		e1 := createEdge(t, "A", "B")
		e2 := createEdge(t, "B", "C")
		e3 := createEdge(t, "C", "A")
		g, _ := engine.NewGraph("g1", []*engine.Node{nA, nB, nC}, []*engine.Edge{e1, e2, e3})

		cycle := graph.FindCycle(g)
		if len(cycle) == 0 {
			t.Fatalf("expected cycle, got nil")
		}
		if cycle[0] != cycle[len(cycle)-1] {
			t.Errorf("expected cycle to start and end with the same node, got %v", cycle)
		}
		// Verify consecutive pairs are real edges
		for i := 0; i < len(cycle)-1; i++ {
			found := false
			for _, e := range g.Edges() {
				if e.From() == cycle[i] && e.To() == cycle[i+1] {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("edge %v -> %v not found in graph", cycle[i], cycle[i+1])
			}
		}
	})

	t.Run("ReachableFromNonCyclicPrefix", func(t *testing.T) {
		nA := createNode(t, "A")
		nB := createNode(t, "B")
		nC := createNode(t, "C")
		nD := createNode(t, "D")
		e1 := createEdge(t, "A", "B")
		e2 := createEdge(t, "B", "C")
		e3 := createEdge(t, "C", "D")
		e4 := createEdge(t, "D", "B") // Cycle B-C-D-B
		g, _ := engine.NewGraph("g1", []*engine.Node{nA, nB, nC, nD}, []*engine.Edge{e1, e2, e3, e4})

		cycle := graph.FindCycle(g)
		if len(cycle) == 0 {
			t.Fatalf("expected cycle, got nil")
		}
		if cycle[0] != cycle[len(cycle)-1] {
			t.Errorf("expected cycle to start and end with the same node, got %v", cycle)
		}
	})

	t.Run("DisconnectedGraphs", func(t *testing.T) {
		nA := createNode(t, "A")
		nB := createNode(t, "B")
		nC := createNode(t, "C")
		nD := createNode(t, "D")
		e1 := createEdge(t, "A", "B")
		e2 := createEdge(t, "C", "D")
		e3 := createEdge(t, "D", "C") // Cycle C-D-C
		g, _ := engine.NewGraph("g1", []*engine.Node{nA, nB, nC, nD}, []*engine.Edge{e1, e2, e3})

		cycle := graph.FindCycle(g)
		if len(cycle) == 0 {
			t.Fatalf("expected cycle, got nil")
		}
		if cycle[0] != cycle[len(cycle)-1] {
			t.Errorf("expected cycle to start and end with the same node, got %v", cycle)
		}
	})
}

func TestTopologicalSort(t *testing.T) {
	t.Run("DeterministicOrder", func(t *testing.T) {
		nodesList := []*engine.Node{
			createNode(t, "C"),
			createNode(t, "B"),
			createNode(t, "A"),
			createNode(t, "D"),
		}
		edgesList := []*engine.Edge{
			createEdge(t, "B", "D"),
			createEdge(t, "A", "B"),
			createEdge(t, "A", "C"),
			createEdge(t, "C", "D"),
		}

		g1, _ := engine.NewGraph("g1", nodesList, edgesList)
		order1, err := graph.TopologicalSort(g1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Shuffle and create g2
		r := rand.New(rand.NewSource(42))
		r.Shuffle(len(nodesList), func(i, j int) { nodesList[i], nodesList[j] = nodesList[j], nodesList[i] })
		r.Shuffle(len(edgesList), func(i, j int) { edgesList[i], edgesList[j] = edgesList[j], edgesList[i] })
		g2, _ := engine.NewGraph("g2", nodesList, edgesList)
		order2, err := graph.TopologicalSort(g2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !reflect.DeepEqual(order1, order2) {
			t.Errorf("expected identical topological sort order despite shuffling, got %v and %v", order1, order2)
		}

		expectedOrder := []engine.NodeID{"A", "B", "C", "D"}
		if !reflect.DeepEqual(order1, expectedOrder) {
			t.Errorf("expected deterministic lexicographical tie-breaking %v, got %v", expectedOrder, order1)
		}
	})

	t.Run("CycleError", func(t *testing.T) {
		nA := createNode(t, "A")
		nB := createNode(t, "B")
		nC := createNode(t, "C")
		e1 := createEdge(t, "A", "B")
		e2 := createEdge(t, "B", "C")
		e3 := createEdge(t, "C", "A")
		g, _ := engine.NewGraph("g1", []*engine.Node{nA, nB, nC}, []*engine.Edge{e1, e2, e3})

		_, err := graph.TopologicalSort(g)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		if !errors.Is(err, graph.ErrCycle) {
			t.Errorf("expected error to be ErrCycle, got %v", err)
		}

		var cycleErr *graph.CycleError
		if !errors.As(err, &cycleErr) {
			t.Fatalf("expected error to be of type *CycleError")
		}

		if len(cycleErr.Path) == 0 {
			t.Errorf("expected error to contain cycle path, got empty path")
		}
	})

	t.Run("10kNodeChainDoesNotBlowStack", func(t *testing.T) {
		const size = 10000
		nodes := make([]*engine.Node, size)
		edges := make([]*engine.Edge, size-1)

		var err error
		for i := 0; i < size; i++ {
			id := engine.NodeID(string(rune(i))) // Dummy unique IDs
			nodes[i], err = engine.NewNode(id, "test", nil)
			if err != nil {
				t.Fatalf("Failed to create node: %v", err)
			}
			if i > 0 {
				edges[i-1], err = engine.NewEdge(nodes[i-1].ID(), id, "", nil)
				if err != nil {
					t.Fatalf("Failed to create edge: %v", err)
				}
			}
		}

		g, err := engine.NewGraph("g10k", nodes, edges)
		if err != nil {
			t.Fatalf("Failed to create graph: %v", err)
		}

		// TopologicalSort
		order, err := graph.TopologicalSort(g)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(order) != size {
			t.Errorf("expected %d nodes in order, got %d", size, len(order))
		}

		// FindCycle
		cycle := graph.FindCycle(g)
		if cycle != nil {
			t.Errorf("expected no cycle, got %v", cycle)
		}
	})
}
