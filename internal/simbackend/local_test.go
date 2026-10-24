package simbackend_test

import (
	"context"
	"os"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/simbackend"
	"github.com/stretchr/testify/require"
)

func TestLocal_ValidateModel(t *testing.T) {
	backend := simbackend.NewLocal()
	ctx := context.Background()

	t.Run("valid", func(t *testing.T) {
		issues, err := backend.ValidateModel(ctx, "../../examples/simulation/loan/sim.yaml")
		require.NoError(t, err)
		require.Empty(t, issues)
	})

	t.Run("invalid", func(t *testing.T) {
		issues, err := backend.ValidateModel(ctx, "testdata/invalid.yaml")
		require.NoError(t, err)
		require.NotEmpty(t, issues)
		// Should have some validation issues
	})
}

func TestLocal_RunExperiment(t *testing.T) {
	backend := simbackend.NewLocal()
	ctx := context.Background()

	res, err := backend.RunExperiment(ctx, "testdata/experiment.yaml", simbackend.RunOptions{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Report)
	require.NotEmpty(t, res.Policies)

	require.Len(t, res.Report.Worlds, 2)

	for _, wr := range res.Report.Worlds {
		switch wr.World {
		case "base":
			require.InDelta(t, 460.0, wr.Policies[0].Objective.Mean, 0.001)
		case "recession":
			require.InDelta(t, 202.525, wr.Policies[0].Objective.Mean, 0.001)
		default:
			t.Fatalf("unexpected world: %s", wr.World)
		}
	}
}

func TestLocal_Report(t *testing.T) {
	backend := simbackend.NewLocal()
	ctx := context.Background()

	_, err := backend.Report(ctx, "exp-123", "summary")
	require.ErrorIs(t, err, simbackend.ErrUnsupported)
}

func TestLocal_ExplainPolicy(t *testing.T) {
	backend := simbackend.NewLocal()
	ctx := context.Background()

	golden, err := os.ReadFile("../../internal/sim/explain/testdata/recession-v1.golden")
	require.NoError(t, err)

	out, err := backend.ExplainPolicy(ctx, "../../internal/sim/explain/testdata/recession-v1.policy.yaml")
	require.NoError(t, err)

	require.Equal(t, string(golden), out)
}

func TestLocal_Compliance(_ *testing.T) {
	var _ simbackend.Backend = simbackend.NewLocal()
}
