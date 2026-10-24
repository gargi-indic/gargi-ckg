package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/sim/jobs"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

// recoverClock is the fixed time Recover tests inject as Options.Now.
var recoverClock = time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)

// seed stores an experiment record directly, as a previous process left
// it.
func seed(t *testing.T, st *fakeStore, id string, status sim.ExperimentStatus, created time.Time) sim.ExperimentRecord {
	t.Helper()
	rec := sim.ExperimentRecord{
		ID:         id,
		Model:      sim.ModelRef{ID: "loan-underwriting", Version: "1"},
		Definition: []byte("apiVersion: experiment/v1\n"),
		Format:     "yaml",
		Status:     status,
		CreatedAt:  created,
	}
	if status != sim.ExperimentQueued {
		rec.StartedAt = created.Add(time.Second)
		rec.Progress = sim.Progress{Done: 2, Total: 10}
	}
	if status.Terminal() {
		rec.FinishedAt = created.Add(2 * time.Second)
		rec.Error = "kept: " + id
	}
	if status == sim.ExperimentSucceeded {
		rec.Report = []byte(`{"kept":true}`)
	}
	require.NoError(t, st.CreateExperiment(context.Background(), rec))
	return rec
}

func TestRunner_RecoverMarksUnfinishedFailed(t *testing.T) {
	h := newHarness(t, jobs.Options{Now: func() time.Time { return recoverClock }})
	base := recoverClock.Add(-time.Hour)
	// More records than one fake store page (fakePageSize), interleaved.
	var unfinished, terminal []sim.ExperimentRecord
	statuses := []sim.ExperimentStatus{
		sim.ExperimentQueued, sim.ExperimentSucceeded, sim.ExperimentRunning, sim.ExperimentFailed,
		sim.ExperimentRunning, sim.ExperimentCancelled, sim.ExperimentQueued,
	}
	for i, status := range statuses {
		rec := seed(t, h.store, fmt.Sprintf("old-%d", i), status, base.Add(time.Duration(i)*time.Minute))
		if status.Terminal() {
			terminal = append(terminal, rec)
		} else {
			unfinished = append(unfinished, rec)
		}
	}

	n, err := h.runner.Recover(context.Background())
	require.NoError(t, err)
	require.Equal(t, len(unfinished), n)

	for _, want := range unfinished {
		got, err := h.store.GetExperiment(context.Background(), want.ID)
		require.NoError(t, err)
		require.Equal(t, sim.ExperimentFailed, got.Status, want.ID)
		require.Equal(t, jobs.MsgInterrupted, got.Error, want.ID)
		require.Equal(t, "interrupted: server restarted before the experiment finished", got.Error)
		require.True(t, got.FinishedAt.Equal(recoverClock), "%s FinishedAt = %v, want the injected clock", want.ID, got.FinishedAt)
		// Everything else is kept.
		require.Equal(t, want.StartedAt, got.StartedAt, want.ID)
		require.Equal(t, want.Progress, got.Progress, want.ID)
		require.Equal(t, want.Definition, got.Definition, want.ID)
		require.Equal(t, want.Model, got.Model, want.ID)
	}
	for _, want := range terminal {
		got, err := h.store.GetExperiment(context.Background(), want.ID)
		require.NoError(t, err)
		require.Equal(t, want, got, "terminal record %s is untouched", want.ID)
		require.Len(t, h.store.writes(want.ID), 1, "terminal record %s is never rewritten", want.ID)
	}

	// Idempotent: nothing is left to recover.
	n, err = h.runner.Recover(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestRunner_RecoverEmptyStore(t *testing.T) {
	h := newHarness(t, jobs.Options{})
	n, err := h.runner.Recover(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestRunner_RecoverSkipsExperimentsActiveInRunner(t *testing.T) {
	h := newHarness(t, jobs.Options{})
	running := h.mustSubmit(t, loanExperiment("monte_carlo", longRuns, onePolicy))
	h.waitProgress(t, running.ID)
	seed(t, h.store, "orphan", sim.ExperimentRunning, time.Now().Add(-time.Hour))

	n, err := h.runner.Recover(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n, "only the orphan is recovered")
	got, err := h.store.GetExperiment(context.Background(), running.ID)
	require.NoError(t, err)
	require.Equal(t, sim.ExperimentRunning, got.Status, "the runner's own experiment keeps running")
	orphan, err := h.store.GetExperiment(context.Background(), "orphan")
	require.NoError(t, err)
	require.Equal(t, sim.ExperimentFailed, orphan.Status)

	require.NoError(t, h.runner.Cancel(context.Background(), running.ID))
	require.Equal(t, sim.ExperimentCancelled, h.waitTerminal(t, running.ID).Status)
}

func TestRunner_RecoverListError(t *testing.T) {
	h := newHarness(t, jobs.Options{})
	seed(t, h.store, "old", sim.ExperimentRunning, time.Now())
	errBoom := errors.New("boom")
	h.store.mu.Lock()
	h.store.listErr = errBoom
	h.store.mu.Unlock()
	n, err := h.runner.Recover(context.Background())
	require.ErrorIs(t, err, errBoom)
	require.Zero(t, n)
	got, err := h.store.GetExperiment(context.Background(), "old")
	require.NoError(t, err)
	require.Equal(t, sim.ExperimentRunning, got.Status, "nothing is changed when listing fails")
}
