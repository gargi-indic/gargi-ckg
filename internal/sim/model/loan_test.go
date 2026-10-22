package model

import (
	"os"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/graph"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/require"
)

func TestLoanGraphValidates(t *testing.T) {
	data, err := os.ReadFile("../../../examples/simulation/loan/graph.yaml")
	require.NoError(t, err)

	parser := graph.NewParser()
	def, err := parser.Parse(data, engine.FormatYAML)
	require.NoError(t, err)

	errs := graph.ValidateStructure(def)
	if errs != nil && errs.Len() > 0 {
		t.Fatalf("loan graph structure validation failed: %v", errs.Error())
	}
}

func TestLoanSimFilesParse(t *testing.T) {
	// sim.yaml
	simData, err := os.ReadFile("../../../examples/simulation/loan/sim.yaml")
	require.NoError(t, err)
	sim, err := ParseSimModel(simData, engine.FormatYAML)
	require.NoError(t, err)

	require.Equal(t, "loan-underwriting", sim.ID)
	require.Contains(t, sim.Worlds, "base")
	require.Contains(t, sim.Worlds, "recession")
	require.Len(t, sim.Worlds, 2)
	require.NotNil(t, sim.Latents["bad"])
	require.NotNil(t, sim.Latents["bad"].Bernoulli)
	require.NotNil(t, sim.Behaviors["ai_score"])
	require.NotNil(t, sim.Behaviors["human_review"])

	// experiment.yaml
	expData, err := os.ReadFile("../../../examples/simulation/loan/experiment.yaml")
	require.NoError(t, err)
	exp, err := ParseExperiment(expData, engine.FormatYAML)
	require.NoError(t, err)

	require.Equal(t, "both", exp.Evaluator)
	require.Equal(t, 200000, exp.Runs)
	require.Equal(t, uint64(42), exp.Seed)
	require.Equal(t, "exhaustive", exp.Policies.Search)

	// policies/recession-v1.yaml
	polData, err := os.ReadFile("../../../examples/simulation/loan/policies/recession-v1.yaml")
	require.NoError(t, err)
	pol, err := ParsePolicy(polData, engine.FormatYAML)
	require.NoError(t, err)

	require.Equal(t, "recession-v1", pol.Version)
	require.Contains(t, pol.Decisions, "screen")
	require.Contains(t, pol.Decisions, "route")
	require.Contains(t, pol.Decisions, "final")
}
