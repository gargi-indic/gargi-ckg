package model

import (
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

// TestCompileGraph compiles the loan model against its already parsed graph
// and reports graph and model issues like Compile.
func TestCompileGraph(t *testing.T) {
	m := loanModel(t)
	again, errs := CompileGraph(m.Def, m.Graph, setupRegistry())
	require.Nil(t, errs)
	require.Same(t, m.Graph, again.Graph)
	require.Same(t, m.Def, again.Def)
	require.Equal(t, m.LatentOrder, again.LatentOrder)
	require.Len(t, again.CompiledBehaviors, len(m.CompiledBehaviors))

	issues := func(g *engine.GraphDefinition) []engine.ValidationIssue {
		_, errs := CompileGraph(m.Def, g, setupRegistry())
		require.NotNil(t, errs)
		out := errs.Issues()
		for i := range out {
			out[i].Message = ""
		}
		return out
	}

	// A graph edge to an unknown node is a graph issue.
	g := *m.Graph
	g.Edges = append(append([]engine.EdgeDefinition(nil), m.Graph.Edges...), engine.EdgeDefinition{From: "final", To: "ghost"})
	got := issues(&g)
	require.Len(t, got, 1)
	require.Equal(t, "$.graph:$.edges[9].to", got[0].Path)

	// An observes path to a node that is not an ancestor is a model issue.
	g = *m.Graph
	g.Nodes = append([]engine.NodeDefinition(nil), m.Graph.Nodes...)
	g.Nodes[0].Config = map[string]any{"options": []any{"score", "skip"}, "observes": []any{"final.output.choice"}}
	require.Equal(t, []engine.ValidationIssue{
		{Path: "$.graph:$.nodes[0].config.observes[0]", Code: sim.CodeObservesNonAncestor},
	}, issues(&g))

	_, errs = CompileGraph(nil, m.Graph, setupRegistry())
	require.Equal(t, 1, errs.Len())
	_, errs = CompileGraph(m.Def, nil, setupRegistry())
	require.Equal(t, 1, errs.Len())
}
