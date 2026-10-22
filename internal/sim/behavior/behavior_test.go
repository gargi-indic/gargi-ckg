package behavior

import (
	"context"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/internal/sim/sampler"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/google/cel-go/cel"
)

func TestBehavior_Abandon(t *testing.T) {
	env, _ := cel.NewEnv(cel.Variable("world", cel.MapType(cel.StringType, cel.DynType)))
	ast, _ := env.Compile("world.abandon_p")
	prog, _ := env.Program(ast)

	cb := model.CompiledBehavior{
		AbandonP: &model.CompiledParam{Source: "world.abandon_p", Program: prog},
	}
	b := New(cb)

	s := sampler.NewMonteCarlo(42)
	in := sim.BehaviorInput{
		NodeID: "test",
		World:  sim.World{Params: map[string]any{"abandon_p": 1.0}},
	}
	out, err := b.Sample(context.Background(), s, in)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Abandoned {
		t.Errorf("expected abandoned to be true")
	}
}

func TestBehavior_Fail(t *testing.T) {
	env, _ := cel.NewEnv(cel.Variable("world", cel.MapType(cel.StringType, cel.DynType)))
	ast, _ := env.Compile("world.fail_p")
	prog, _ := env.Program(ast)

	cb := model.CompiledBehavior{
		FailP:     &model.CompiledParam{Source: "world.fail_p", Program: prog},
		FailClass: string(engine.ClassRetryable),
	}
	b := New(cb)

	s := sampler.NewMonteCarlo(42)
	in := sim.BehaviorInput{
		NodeID: "test",
		World:  sim.World{Params: map[string]any{"fail_p": 1.0}},
	}
	out, err := b.Sample(context.Background(), s, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Err == nil {
		t.Errorf("expected failure error")
	} else if engine.ClassOf(out.Err) != engine.ClassRetryable {
		t.Errorf("expected retryable error, got %v", engine.ClassOf(out.Err))
	}
}

func TestBehavior_Duration(t *testing.T) {
	cb := model.CompiledBehavior{
		Duration: &model.CompiledValue{
			Kind:  model.KindFixed,
			Fixed: "48h",
		},
	}
	b := New(cb)

	s := sampler.NewMonteCarlo(42)
	in := sim.BehaviorInput{
		NodeID: "test",
	}
	out, err := b.Sample(context.Background(), s, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Duration.Hours() != 48 {
		t.Errorf("expected 48h, got %v", out.Duration)
	}
}

func TestBehavior_OutputChoice(t *testing.T) {
	cb := model.CompiledBehavior{
		Outputs: map[string]model.CompiledValue{
			"status": {
				Kind: model.KindChoice,
				Choice: []model.CompiledChoiceOption{
					{Value: "approved", Weight: model.CompiledParam{Literal: 1.0}},
				},
			},
		},
	}
	b := New(cb)

	s := sampler.NewMonteCarlo(42)
	in := sim.BehaviorInput{
		NodeID: "test",
	}
	out, err := b.Sample(context.Background(), s, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Output["status"] != "approved" {
		t.Errorf("expected approved, got %v", out.Output["status"])
	}
}

func TestBehavior_CELError(t *testing.T) {
	env, _ := cel.NewEnv(cel.Variable("world", cel.MapType(cel.StringType, cel.DynType)))
	ast, _ := env.Compile("world.cost + 'invalid'")
	prog, _ := env.Program(ast)

	cb := model.CompiledBehavior{
		Cost: &model.CompiledParam{Source: "world.cost + 'invalid'", Program: prog},
	}
	b := New(cb)

	s := sampler.NewMonteCarlo(42)
	in := sim.BehaviorInput{
		NodeID: "test",
		World:  sim.World{Params: map[string]any{"cost": 10.0}},
	}
	_, err := b.Sample(context.Background(), s, in)
	if err == nil {
		t.Errorf("expected CEL eval error")
	}
}
