package graph_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/graph"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func generateChainGraph(b *testing.B, size int) *engine.Graph {
	nodes := make([]*engine.Node, size)
	edges := make([]*engine.Edge, size-1)

	var err error
	for i := 0; i < size; i++ {
		nodes[i], err = engine.NewNode(engine.NodeID(fmt.Sprintf("n%d", i)), "test", nil)
		if err != nil {
			b.Fatalf("Failed to create node: %v", err)
		}
		if i > 0 {
			edges[i-1], err = engine.NewEdge(nodes[i-1].ID(), nodes[i].ID(), "", nil)
			if err != nil {
				b.Fatalf("Failed to create edge: %v", err)
			}
		}
	}

	g, err := engine.NewGraph("chain", nodes, edges)
	if err != nil {
		b.Fatalf("Failed to create graph: %v", err)
	}
	return g
}

func generateLayeredGraph(b *testing.B, numLayers, nodesPerLayer int, seed int64) *engine.Graph {
	r := rand.New(rand.NewSource(seed))
	size := numLayers * nodesPerLayer
	nodes := make([]*engine.Node, size)
	var edges []*engine.Edge

	var err error
	for i := 0; i < size; i++ {
		nodes[i], err = engine.NewNode(engine.NodeID(fmt.Sprintf("n%d", i)), "test", nil)
		if err != nil {
			b.Fatalf("Failed to create node: %v", err)
		}
	}

	for l := 0; l < numLayers-1; l++ {
		for i := 0; i < nodesPerLayer; i++ {
			from := l*nodesPerLayer + i
			for j := 0; j < nodesPerLayer; j++ {
				// Sparse connection (25% chance) to avoid too many edges
				if r.Float32() < 0.25 {
					to := (l+1)*nodesPerLayer + j
					e, err := engine.NewEdge(nodes[from].ID(), nodes[to].ID(), "", nil)
					if err != nil {
						b.Fatalf("Failed to create edge: %v", err)
					}
					edges = append(edges, e)
				}
			}
		}
	}

	g, err := engine.NewGraph("layered", nodes, edges)
	if err != nil {
		b.Fatalf("Failed to create graph: %v", err)
	}
	return g
}

func generateRandomDAG(b *testing.B, size int, seed int64) *engine.Graph {
	r := rand.New(rand.NewSource(seed))
	nodes := make([]*engine.Node, size)
	var edges []*engine.Edge

	var err error
	for i := 0; i < size; i++ {
		nodes[i], err = engine.NewNode(engine.NodeID(fmt.Sprintf("n%d", i)), "test", nil)
		if err != nil {
			b.Fatalf("Failed to create node: %v", err)
		}
	}

	// Create a random DAG: edges only go from lower index to higher index
	for i := 0; i < size; i++ {
		// Each node has max 3 outgoing edges to higher nodes
		numEdges := r.Intn(4)
		for e := 0; e < numEdges; e++ {
			if i+1 < size {
				toIdx := i + 1 + r.Intn(size-(i+1))
				// Ensure no duplicate edges (simplified here by catching NewGraph error if duplicate is created, or avoiding it)
				edge, err := engine.NewEdge(nodes[i].ID(), nodes[toIdx].ID(), "", nil)
				if err != nil {
					b.Fatalf("Failed to create edge: %v", err)
				}
				edges = append(edges, edge)
			}
		}
	}

	// Deduplicate edges to avoid NewGraph error
	type edgeKey struct{ from, to engine.NodeID }
	edgeMap := make(map[edgeKey]struct{})
	var uniqueEdges []*engine.Edge
	for _, e := range edges {
		key := edgeKey{e.From(), e.To()}
		if _, exists := edgeMap[key]; !exists {
			edgeMap[key] = struct{}{}
			uniqueEdges = append(uniqueEdges, e)
		}
	}

	g, err := engine.NewGraph("random", nodes, uniqueEdges)
	if err != nil {
		b.Fatalf("Failed to create graph: %v", err)
	}
	return g
}

func generateStarGraphFanOut(b *testing.B, size int) *engine.Graph {
	nodes := make([]*engine.Node, size)
	var edges []*engine.Edge

	var err error
	for i := 0; i < size; i++ {
		nodes[i], err = engine.NewNode(engine.NodeID(fmt.Sprintf("n%d", i)), "test", nil)
		if err != nil {
			b.Fatalf("Failed to create node: %v", err)
		}
		if i > 0 {
			e, err := engine.NewEdge(nodes[0].ID(), nodes[i].ID(), "", nil)
			if err != nil {
				b.Fatalf("Failed to create edge: %v", err)
			}
			edges = append(edges, e)
		}
	}

	g, err := engine.NewGraph("star-fanout", nodes, edges)
	if err != nil {
		b.Fatalf("Failed to create graph: %v", err)
	}
	return g
}

func generateStarGraphFanIn(b *testing.B, size int) *engine.Graph {
	nodes := make([]*engine.Node, size)
	var edges []*engine.Edge

	var err error
	for i := 0; i < size; i++ {
		nodes[i], err = engine.NewNode(engine.NodeID(fmt.Sprintf("n%d", i)), "test", nil)
		if err != nil {
			b.Fatalf("Failed to create node: %v", err)
		}
		if i > 0 {
			e, err := engine.NewEdge(nodes[i].ID(), nodes[0].ID(), "", nil)
			if err != nil {
				b.Fatalf("Failed to create edge: %v", err)
			}
			edges = append(edges, e)
		}
	}

	g, err := engine.NewGraph("star-fanin", nodes, edges)
	if err != nil {
		b.Fatalf("Failed to create graph: %v", err)
	}
	return g
}

func BenchmarkTopologicalSort10k(b *testing.B) {
	b.Run("Chain", func(b *testing.B) {
		g := generateChainGraph(b, 10000)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := graph.TopologicalSort(g)
			if err != nil {
				b.Fatalf("unexpected error: %v", err)
			}
		}
	})

	b.Run("Layered", func(b *testing.B) {
		g := generateLayeredGraph(b, 100, 100, 42) // 10k nodes
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := graph.TopologicalSort(g)
			if err != nil {
				b.Fatalf("unexpected error: %v", err)
			}
		}
	})

	b.Run("Random", func(b *testing.B) {
		g := generateRandomDAG(b, 10000, 42) // Fixed seed
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := graph.TopologicalSort(g)
			if err != nil {
				b.Fatalf("unexpected error: %v", err)
			}
		}
	})

	b.Run("FanOut", func(b *testing.B) {
		g := generateStarGraphFanOut(b, 10000)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := graph.TopologicalSort(g)
			if err != nil {
				b.Fatalf("unexpected error: %v", err)
			}
		}
	})

	b.Run("FanIn", func(b *testing.B) {
		g := generateStarGraphFanIn(b, 10000)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := graph.TopologicalSort(g)
			if err != nil {
				b.Fatalf("unexpected error: %v", err)
			}
		}
	})
}

func BenchmarkFindCycle10k(b *testing.B) {
	b.Run("Chain", func(b *testing.B) {
		g := generateChainGraph(b, 10000)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cycle := graph.FindCycle(g)
			if cycle != nil {
				b.Fatalf("expected no cycle, got %v", cycle)
			}
		}
	})

	b.Run("Layered", func(b *testing.B) {
		g := generateLayeredGraph(b, 100, 100, 42) // 10k nodes
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cycle := graph.FindCycle(g)
			if cycle != nil {
				b.Fatalf("expected no cycle, got %v", cycle)
			}
		}
	})

	b.Run("Random", func(b *testing.B) {
		g := generateRandomDAG(b, 10000, 42) // Fixed seed
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cycle := graph.FindCycle(g)
			if cycle != nil {
				b.Fatalf("expected no cycle, got %v", cycle)
			}
		}
	})

	b.Run("FanOut", func(b *testing.B) {
		g := generateStarGraphFanOut(b, 10000)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cycle := graph.FindCycle(g)
			if cycle != nil {
				b.Fatalf("expected no cycle, got %v", cycle)
			}
		}
	})

	b.Run("FanIn", func(b *testing.B) {
		g := generateStarGraphFanIn(b, 10000)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cycle := graph.FindCycle(g)
			if cycle != nil {
				b.Fatalf("expected no cycle, got %v", cycle)
			}
		}
	})
}

func BenchmarkUpstream10k(b *testing.B) {
	g := generateRandomDAG(b, 10000, 42)
	nodeID := engine.NodeID("n9999") // Typically has many upstream nodes in random DAG

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := graph.Upstream(g, nodeID)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

func BenchmarkDownstream10k(b *testing.B) {
	g := generateRandomDAG(b, 10000, 42)
	nodeID := engine.NodeID("n0") // Typically has many downstream nodes in random DAG

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := graph.Downstream(g, nodeID)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
