package kernel_test

import (
	"context"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/kernel"
	"github.com/gargi-indic/gargi-flow/internal/sim/sampler"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

// TestSimKernel_TraceLoan checks the started nodes and traversed edges of
// every chance branch of the loan model under a review policy. Loan graph
// nodes: 0 screen, 1 ai_score, 2 route, 3 human_review, 4 final, 5
// approve, 6 reject; edges: 0 screen->ai_score, 1 screen->route, 2
// ai_score->route, 3 route->human_review, 4 human_review->final, 5
// route->approve, 6 final->approve, 7 route->reject, 8 final->reject.
func TestSimKernel_TraceLoan(t *testing.T) {
	m, _ := loanEpisode(t)
	review := fixedPolicy{fn: func(d engine.NodeID, _ []string) string {
		return map[engine.NodeID]string{"screen": "score", "route": "review", "final": "approve"}[d]
	}}
	world := loanWorld(m, "base")
	enum := sampler.NewEnumerator()
	abandoned, completed := 0, 0
	for enum.Next() {
		traced, err := kernel.RunEpisode(context.Background(), m, world, review, enum.Sampler(), kernel.Options{Trace: true})
		require.NoError(t, err)
		if traced.Abandoned {
			abandoned++
			require.Equal(t, []int{0, 1, 2, 3}, traced.Started)
			require.Equal(t, []int{0, 2, 3}, traced.Edges)
		} else {
			completed++
			require.Equal(t, []int{0, 1, 2, 3, 4, 5}, traced.Started)
			require.Equal(t, []int{0, 2, 3, 4, 6}, traced.Edges)
		}
	}
	require.Positive(t, abandoned)
	require.Positive(t, completed)

	// Skipping the score: route starts through screen->route, approve
	// through route->approve.
	approve := fixedPolicy{fn: func(d engine.NodeID, _ []string) string {
		return map[engine.NodeID]string{"screen": "skip", "route": "approve"}[d]
	}}
	rec, err := kernel.RunEpisode(context.Background(), m, world, approve, sampler.NewMonteCarlo(1), kernel.Options{Trace: true})
	require.NoError(t, err)
	require.Equal(t, []int{0, 2, 5}, rec.Started)
	require.Equal(t, []int{1, 5}, rec.Edges)

	// Tracing changes nothing else, and is off by default.
	plain, err := kernel.RunEpisode(context.Background(), m, world, approve, sampler.NewMonteCarlo(1), kernel.Options{})
	require.NoError(t, err)
	require.Nil(t, plain.Started)
	require.Nil(t, plain.Edges)
	rec.Started, rec.Edges = nil, nil
	require.Equal(t, plain, rec)
}

// TestSimKernel_TraceJoins checks that an all_of join starts through all of
// its incoming edges and an any_of join through the satisfied ones only.
func TestSimKernel_TraceJoins(t *testing.T) {
	graph := `apiVersion: graph/v1
id: joins
nodes:
  - id: root
    type: transform
    config: {set: {go: true}}
  - {id: b, type: passthrough}
  - {id: c, type: passthrough}
  - {id: all, type: passthrough}
  - {id: any, type: passthrough, joinMode: any_of}
edges:
  - {from: root, to: b}
  - {from: root, to: c}
  - {from: b, to: all}
  - {from: c, to: all}
  - {from: b, to: any}
  - {from: c, to: any, condition: "nodes.root.output.go == false"}
`
	m := compileInDir(t, graph, simBody{objective: "double(size(run.path))"})
	rec, err := kernel.RunEpisode(context.Background(), m, sim.World{ID: "w"}, fixedPolicy{}, sampler.NewMonteCarlo(0), kernel.Options{Trace: true})
	require.NoError(t, err)
	require.Equal(t, engine.StatusSucceeded, rec.FinalStatus)
	require.ElementsMatch(t, []int{0, 1, 2, 3, 4}, rec.Started)
	require.Equal(t, 0, rec.Started[0])
	require.ElementsMatch(t, []int{0, 1, 2, 3, 4}, rec.Edges)
}
