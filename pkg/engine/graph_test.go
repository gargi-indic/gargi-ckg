package engine

import (
	"testing"
)

func TestNewGraph(t *testing.T) {
	node1, _ := NewNode("n1", "compute", nil)
	node2, _ := NewNode("n2", "compute", nil)
	node3, _ := NewNode("n3", "compute", nil)

	edge1, _ := NewEdge("n1", "n2", "", nil)
	edge2, _ := NewEdge("n2", "n3", "", nil)

	tests := []struct {
		name    string
		id      string
		nodes   []*Node
		edges   []*Edge
		wantErr bool
	}{
		{
			name:    "valid graph",
			id:      "g1",
			nodes:   []*Node{node1, node2, node3},
			edges:   []*Edge{edge1, edge2},
			wantErr: false,
		},
		{
			name:    "empty id",
			id:      "",
			nodes:   []*Node{node1},
			edges:   []*Edge{},
			wantErr: true,
		},
		{
			name:    "nil node",
			id:      "g1",
			nodes:   []*Node{node1, nil},
			edges:   []*Edge{},
			wantErr: true,
		},
		{
			name:    "invalid node (empty id)",
			id:      "g1",
			nodes:   []*Node{{id: "", nodeType: "compute"}},
			edges:   []*Edge{},
			wantErr: true,
		},
		{
			name:    "duplicate node id",
			id:      "g1",
			nodes:   []*Node{node1, node1},
			edges:   []*Edge{},
			wantErr: true,
		},
		{
			name:    "nil edge",
			id:      "g1",
			nodes:   []*Node{node1, node2},
			edges:   []*Edge{nil},
			wantErr: true,
		},
		{
			name:    "invalid edge (empty from)",
			id:      "g1",
			nodes:   []*Node{node1, node2},
			edges:   []*Edge{{from: "", to: "n2"}},
			wantErr: true,
		},
		{
			name:    "edge referencing unknown from node",
			id:      "g1",
			nodes:   []*Node{node2, node3},
			edges:   []*Edge{edge1}, // edge1 is n1 -> n2, but n1 is missing
			wantErr: true,
		},
		{
			name:    "edge referencing unknown to node",
			id:      "g1",
			nodes:   []*Node{node1, node2},
			edges:   []*Edge{edge2}, // edge2 is n2 -> n3, but n3 is missing
			wantErr: true,
		},
		{
			name:    "duplicate edge",
			id:      "g1",
			nodes:   []*Node{node1, node2},
			edges:   []*Edge{edge1, edge1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewGraph(tt.id, tt.nodes, tt.edges)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewGraph() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.ID() != tt.id {
					t.Errorf("ID() got = %v, want %v", got.ID(), tt.id)
				}
				if len(got.Nodes()) != len(tt.nodes) {
					t.Errorf("Nodes() len got = %d, want %d", len(got.Nodes()), len(tt.nodes))
				}
				if len(got.Edges()) != len(tt.edges) {
					t.Errorf("Edges() len got = %d, want %d", len(got.Edges()), len(tt.edges))
				}
			}
		})
	}
}

func TestGraphAccessors(t *testing.T) {
	node1, _ := NewNode("n1", "compute", nil)
	node2, _ := NewNode("n2", "compute", nil)
	node3, _ := NewNode("n3", "compute", nil)

	edge1, _ := NewEdge("n1", "n2", "c1", nil)
	edge2, _ := NewEdge("n2", "n3", "c2", nil)
	edge3, _ := NewEdge("n1", "n3", "c3", nil)

	g, err := NewGraph("g1", []*Node{node1, node2, node3}, []*Edge{edge1, edge2, edge3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("Node()", func(t *testing.T) {
		n := g.Node("n2")
		if n == nil || n.ID() != "n2" {
			t.Errorf("Node() got = %v, want node n2", n)
		}

		n = g.Node("unknown")
		if n != nil {
			t.Errorf("Node() got = %v, want nil", n)
		}
	})

	t.Run("OutgoingEdges()", func(t *testing.T) {
		edges := g.OutgoingEdges("n1")
		if len(edges) != 2 {
			t.Errorf("OutgoingEdges() len = %d, want 2", len(edges))
		}
		if edges[0].To() != "n2" || edges[1].To() != "n3" {
			t.Errorf("OutgoingEdges() order/content unexpected")
		}

		edges = g.OutgoingEdges("n3")
		if len(edges) != 0 {
			t.Errorf("OutgoingEdges() len = %d, want 0", len(edges))
		}
	})

	t.Run("IncomingEdges()", func(t *testing.T) {
		edges := g.IncomingEdges("n3")
		if len(edges) != 2 {
			t.Errorf("IncomingEdges() len = %d, want 2", len(edges))
		}
		if edges[0].From() != "n2" || edges[1].From() != "n1" {
			t.Errorf("IncomingEdges() order/content unexpected")
		}

		edges = g.IncomingEdges("n1")
		if len(edges) != 0 {
			t.Errorf("IncomingEdges() len = %d, want 0", len(edges))
		}
	})

	t.Run("Slice Accessors Return Fresh Slices", func(t *testing.T) {
		nodes1 := g.Nodes()
		nodes1[0] = nil
		nodes2 := g.Nodes()
		if nodes2[0] == nil {
			t.Errorf("Nodes() did not return a fresh slice")
		}

		out1 := g.OutgoingEdges("n1")
		out1[0] = nil
		out2 := g.OutgoingEdges("n1")
		if out2[0] == nil {
			t.Errorf("OutgoingEdges() did not return a fresh slice")
		}

		in1 := g.IncomingEdges("n3")
		in1[0] = nil
		in2 := g.IncomingEdges("n3")
		if in2[0] == nil {
			t.Errorf("IncomingEdges() did not return a fresh slice")
		}
	})
}

func TestGraphImmutability(t *testing.T) {
	node1, _ := NewNode("n1", "t1", map[string]any{"k": "v"})
	node2, _ := NewNode("n2", "t2", nil)
	edge1, _ := NewEdge("n1", "n2", "cond", map[string]string{"k": "v"})

	nodesInput := []*Node{node1, node2}
	edgesInput := []*Edge{edge1}

	g, err := NewGraph("g1", nodesInput, edgesInput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("mutating input slices does not affect graph", func(t *testing.T) {
		nodesInput[0] = nil
		edgesInput[0] = nil

		if g.Nodes()[0] == nil || g.Edges()[0] == nil {
			t.Errorf("graph state was mutated by changing input slices")
		}
	})

	t.Run("mutating input node/edge config does not affect graph", func(t *testing.T) {
		node1.Config()["k"] = "mutated"
		edge1.DataMapping()["k"] = "mutated"

		gNode := g.Node("n1")
		if gNode.Config()["k"] == "mutated" {
			t.Errorf("graph node config was mutated")
		}

		gEdge := g.Edges()[0]
		if gEdge.DataMapping()["k"] == "mutated" {
			t.Errorf("graph edge data mapping was mutated")
		}
	})

	t.Run("mutating returned slices does not affect graph", func(t *testing.T) {
		retNodes := g.Nodes()
		retEdges := g.Edges()

		retNodes[0] = nil
		retEdges[0] = nil

		freshNodes := g.Nodes()
		freshEdges := g.Edges()

		if freshNodes[0] == nil || freshEdges[0] == nil {
			t.Errorf("graph state was mutated by changing returned slices")
		}
	})

	t.Run("mutating returned node/edge components does not affect graph", func(t *testing.T) {
		retNode := g.Node("n1")
		cfg := retNode.Config()
		cfg["k"] = "mutated"

		retEdge := g.Edges()[0]
		dm := retEdge.DataMapping()
		dm["k"] = "mutated"

		freshNode := g.Node("n1")
		if freshNode.Config()["k"] == "mutated" {
			t.Errorf("graph node config was mutated by changing returned component")
		}

		freshEdge := g.Edges()[0]
		if freshEdge.DataMapping()["k"] == "mutated" {
			t.Errorf("graph edge data mapping was mutated by changing returned component")
		}
	})
}
