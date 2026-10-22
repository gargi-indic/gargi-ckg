package sampler

import (
	"context"
	"math"
	"os"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/behavior"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

func TestEnumerator_LoanWeights(t *testing.T) {
	simData, err := os.ReadFile("../../../examples/simulation/loan/sim.yaml")
	if err != nil {
		t.Fatal(err)
	}

	simDef, err := model.ParseSimModel(simData, engine.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}

	reg := engine.NewRegistry()
	factory := func(_ any) (engine.NodeInterface, error) {
		return &engine.BaseNode{}, nil
	}
	_ = reg.Register("decision", factory, nil)
	_ = reg.Register("http_call", factory, nil)
	_ = reg.Register("wait_for_signal", factory, nil)
	_ = reg.Register("end", factory, nil)
	_ = reg.Register("passthrough", factory, nil)

	compiled, ve := model.Compile(simDef, "../../../examples/simulation/loan", reg)
	if ve != nil {
		t.Fatal(ve)
	}

	for _, worldID := range []string{"base", "recession"} {
		enum := NewEnumerator()
		totalWeight := 0.0
		iters := 0

		for enum.Next() {
			iters++
			s := enum.Sampler()

			vars := map[string]any{"world": compiled.Def.Worlds[worldID]}
			latents, err := behavior.SampleLatents(compiled, s, vars)
			if err != nil {
				t.Fatal(err)
			}
			vars["latent"] = latents

			for _, nodeID := range []string{"ai_score", "human_review"} {
				if cb, ok := compiled.CompiledBehaviors[nodeID]; ok {
					class := ""
					if b, ok := compiled.Def.Behaviors[nodeID]; ok && b.Fail != nil {
						class = b.Fail.Class
					}
					b := behavior.New(cb, class)
					_, err = b.Sample(context.Background(), s, sim.BehaviorInput{
						NodeID:  engine.NodeID(nodeID),
						World:   sim.World{Params: vars["world"].(map[string]any)},
						Latents: latents,
					})
					if err != nil {
						t.Fatal(err)
					}
				}
			}

			totalWeight += enum.Weight()
		}

		if math.Abs(totalWeight-1.0) > 1e-12 {
			t.Errorf("world %s: expected sum of weights 1.0, got %f", worldID, totalWeight)
		}
	}
}
