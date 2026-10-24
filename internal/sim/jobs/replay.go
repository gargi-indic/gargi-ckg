package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gargi-indic/gargi-flow/internal/sim/experiment"
	"github.com/gargi-indic/gargi-flow/internal/sim/kernel"
	"github.com/gargi-indic/gargi-flow/internal/sim/sampler"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

var (
	// ErrUnknownWorld is returned by Replay for a world the experiment does
	// not evaluate.
	ErrUnknownWorld = errors.New("jobs: unknown world")
	// ErrUnknownPolicy is returned by Replay for a policy the experiment does
	// not evaluate.
	ErrUnknownPolicy = errors.New("jobs: unknown policy")
	// ErrRunIndex is returned by Replay for a replication index outside
	// [0, runs).
	ErrRunIndex = errors.New("jobs: replication index out of range")
	// ErrNoReport is returned by Replay when no policy is given and the
	// experiment has no report to take the best policy from.
	ErrNoReport = errors.New("jobs: the experiment has no report yet")
)

// ReplayRequest selects one simulated run of an experiment.
type ReplayRequest struct {
	// World is the world to replay; empty selects the experiment's first
	// world.
	World string
	// Policy is the policy ID to replay; empty selects the best policy of
	// World in Report (its exact solution when it has one, else the
	// top-ranked policy).
	Policy string
	// Index is the replication index, in [0, runs).
	Index int
	// Report is the experiment's report; it is only needed when Policy is
	// empty.
	Report *experiment.Report
	// ExecutionID is stamped on the events; empty selects the kernel's
	// default.
	ExecutionID engine.ExecutionID
}

// RunReplay is one replayed run.
type RunReplay struct {
	// World is the replayed world.
	World sim.WorldID
	// PolicyID is the replayed policy.
	PolicyID string
	// Index is the replication index.
	Index int
	// Seed is the replication's sampler seed, sim.Seed(experiment seed,
	// World, Index).
	Seed uint64
	// Events is the engine event stream of the run, in emission order,
	// stamped on the virtual clock.
	Events []*engine.Event
	// Record is the run's outcome.
	Record sim.RunRecord
}

// eventLog is an engine.EventSink that keeps every event.
type eventLog struct {
	mu     sync.Mutex
	events []*engine.Event
}

func (l *eventLog) Emit(_ context.Context, ev *engine.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, ev)
	return nil
}

// Replay re-runs replication req.Index of exp for one world and policy
// exactly as the Monte Carlo evaluator does: kernel.RunEpisode with
// sampler.NewMonteCarlo(sim.Seed(seed, world, index)), recording the engine
// events. The policy is looked up among the experiment's policies (solving
// again for policies: {search: exact}).
func Replay(ctx context.Context, exp *experiment.CompiledExperiment, req ReplayRequest) (*RunReplay, error) {
	if exp == nil || exp.Def == nil || exp.Model == nil {
		return nil, errors.New("jobs: compiled experiment is nil")
	}
	if req.Index < 0 || req.Index >= exp.Def.Runs {
		return nil, fmt.Errorf("%w: %d not in [0, %d)", ErrRunIndex, req.Index, exp.Def.Runs)
	}
	w := -1
	if req.World == "" {
		w = 0
	}
	for i, world := range exp.Worlds {
		if string(world.ID) == req.World {
			w = i
		}
	}
	if w < 0 || w >= len(exp.Worlds) {
		return nil, fmt.Errorf("%w %q", ErrUnknownWorld, req.World)
	}
	world := exp.Worlds[w]

	policyID := req.Policy
	if policyID == "" {
		if req.Report == nil {
			return nil, ErrNoReport
		}
		for _, wr := range req.Report.Worlds {
			if wr.World != world.ID {
				continue
			}
			switch {
			case wr.Solution != nil:
				policyID = wr.Solution.PolicyID
			case len(wr.Policies) > 0:
				policyID = wr.Policies[0].PolicyID
			}
		}
		if policyID == "" {
			return nil, fmt.Errorf("%w: world %q has no ranked policy", ErrNoReport, world.ID)
		}
	}

	var policies []sim.Policy
	var err error
	if solver, ok := exp.PolicySource().(experiment.Solver); ok {
		var sol *experiment.Solution
		if sol, err = solver.Solve(ctx, exp.Model, exp.Worlds); err != nil {
			return nil, err
		}
		policies, err = sol.Policies(exp.Model)
	} else {
		policies, err = exp.PolicySource().Policies(exp.Model)
	}
	if err != nil {
		return nil, err
	}
	var policy sim.Policy
	for _, p := range policies {
		if p.ID() == policyID {
			policy = p
			break
		}
	}
	if policy == nil {
		return nil, fmt.Errorf("%w %q", ErrUnknownPolicy, policyID)
	}

	seed := sim.Seed(exp.Def.Seed, world.ID, req.Index)
	log := &eventLog{}
	rec, err := kernel.RunEpisode(ctx, exp.Model, world, policy, sampler.NewMonteCarlo(seed), kernel.Options{Events: log, ExecutionID: req.ExecutionID})
	if err != nil {
		return nil, err
	}
	rec.Replication = req.Index
	return &RunReplay{
		World:    world.ID,
		PolicyID: policy.ID(),
		Index:    req.Index,
		Seed:     seed,
		Events:   log.events,
		Record:   rec,
	}, nil
}
