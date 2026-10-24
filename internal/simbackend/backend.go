package simbackend

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gargi-indic/gargi-flow/internal/sim/experiment"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// ErrUnsupported is returned (wrapped) by a Backend for an operation it cannot perform,
// e.g. Local.Report (Local keeps no experiment store).
var ErrUnsupported = errors.New("simbackend: operation not supported by this backend")

// RunOptions configures an experiment run.
type RunOptions struct {
	Workers, Top int                // 0 = experiment default, same as the CLI flags today
	Progress     func(sim.Progress) // optional; Local may call it from experiment.Options.Progress
}

// RunResult contains the outcome of an experiment run.
type RunResult struct {
	ExperimentID string // "" for Local
	Report       *experiment.Report
	Policies     []sim.Policy // evaluated policies (Local only; used by --emit-best); nil for remote
}

// Backend runs simulation work for the gargi CLI and (later) the MCP server.
// Local runs in-process; the remote implementation (#187) talks to a gargi server.
type Backend interface {
	// ValidateModel validates the sim/v1 model at path. Problems come back as
	// sorted issues with a nil error; error is reserved for I/O or internal failures.
	ValidateModel(ctx context.Context, path string) ([]engine.ValidationIssue, error)
	// RunExperiment runs the experiment/v1 document at path.
	RunExperiment(ctx context.Context, path string, opts RunOptions) (*RunResult, error)
	// Report returns report kind ("summary", "regret", ...) of a stored experiment as JSON.
	Report(ctx context.Context, experimentID, kind string) (json.RawMessage, error)
	// ExplainPolicy renders a policy in plain language (internal/sim/explain).
	// For Local, ref is a policy/v1 file path.
	ExplainPolicy(ctx context.Context, ref string) (string, error)
}
