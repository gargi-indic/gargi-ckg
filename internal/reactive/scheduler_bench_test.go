package reactive_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/reactive"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func BenchmarkScheduler_EvaluateStep_WideFanout(b *testing.B) {
	for _, size := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("nodes_%d", size), func(b *testing.B) {
			reg := engine.NewRegistry()
			_ = reg.Register("mock", func(_ any) (engine.NodeInterface, error) {
				return &mockNode{ready: true}, nil
			}, nil)

			nodes := make([]*engine.Node, size)
			for i := 0; i < size; i++ {
				n, _ := engine.NewNode(engine.NodeID(fmt.Sprintf("n%d", i)), "mock", nil)
				nodes[i] = n
			}

			g, err := engine.NewGraph("fanout", nodes, nil)
			if err != nil {
				b.Fatalf("failed to create graph: %v", err)
			}

			sched := reactive.NewScheduler(g, reg, nil, nil)
			execID, _ := engine.NewExecutionID()
			state, _ := engine.NewGraphState(execID, g)
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, err := sched.EvaluateStep(ctx, state)
				if err != nil {
					b.Fatalf("evaluate step failed: %v", err)
				}
			}
		})
	}
}

func BenchmarkScheduler_EvaluateStep_DeepPipeline(b *testing.B) {
	for _, depth := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("depth_%d", depth), func(b *testing.B) {
			reg := engine.NewRegistry()
			_ = reg.Register("mock", func(_ any) (engine.NodeInterface, error) {
				return &mockNode{ready: true}, nil
			}, nil)

			nodes := make([]*engine.Node, depth)
			edges := make([]*engine.Edge, depth-1)
			for i := 0; i < depth; i++ {
				n, _ := engine.NewNode(engine.NodeID(fmt.Sprintf("node_%d", i)), "mock", nil)
				nodes[i] = n
				if i > 0 {
					e, _ := engine.NewEdge(nodes[i-1].ID(), nodes[i].ID(), "", nil)
					edges[i-1] = e
				}
			}

			g, err := engine.NewGraph("deep", nodes, edges)
			if err != nil {
				b.Fatalf("failed to create graph: %v", err)
			}

			sched := reactive.NewScheduler(g, reg, nil, nil)
			execID, _ := engine.NewExecutionID()
			state, _ := engine.NewGraphState(execID, g)
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, err := sched.EvaluateStep(ctx, state)
				if err != nil {
					b.Fatalf("evaluate step failed: %v", err)
				}
			}
		})
	}
}

func BenchmarkGraphState_ApplyMutation(b *testing.B) {
	n1, _ := engine.NewNode("n1", "mock", nil)
	n2, _ := engine.NewNode("n2", "mock", nil)
	g, _ := engine.NewGraph("state_bench", []*engine.Node{n1, n2}, nil)
	execID, _ := engine.NewExecutionID()
	state, _ := engine.NewGraphState(execID, g)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mut := engine.StateMutation{
			NodeID:          "n1",
			ExpectedVersion: state.Version(),
			Set: map[string]any{
				"counter": i,
				"active":  true,
			},
		}
		newState, err := state.Apply(mut)
		if err != nil {
			b.Fatalf("apply failed: %v", err)
		}
		state = newState
	}
}
