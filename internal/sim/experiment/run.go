package experiment

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/gargi-indic/gargi-flow/internal/sim/kernel"
	"github.com/gargi-indic/gargi-flow/internal/sim/sampler"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

const (
	// ChunkSize is the number of replications evaluated and summarized as one
	// unit of work. It is fixed so that aggregation does not depend on the
	// worker count.
	ChunkSize = 1000

	// MaxRetained is the number of replications per (world, policy) whose
	// objective and duration values are retained for exact P50/P95. Runs
	// beyond the cap still count towards every other statistic.
	MaxRetained = 1_000_000

	// DefaultTop is the number of policies kept per world when neither
	// Options.Top nor the experiment's top is set.
	DefaultTop = 5

	// EvaluatorMonteCarlo is the evaluator name reported for Monte Carlo
	// estimates.
	EvaluatorMonteCarlo = "monte_carlo"
)

// ErrExactUnsupported is returned by Run for an experiment whose evaluator is
// "exact": only the Monte Carlo evaluator is implemented by this package.
var ErrExactUnsupported = errors.New("experiment: exact evaluator is not supported by Run")

// Options configures Run.
type Options struct {
	// Workers is the size of the worker pool. Zero selects the experiment's
	// workers, or runtime.GOMAXPROCS(0) when that is unset too. The report
	// does not depend on it.
	Workers int
	// Top is the number of best policies kept per world in
	// WorldReport.Policies. Zero selects the experiment's top, or DefaultTop.
	Top int
	// All also fills WorldReport.All with every policy result, not only the
	// top ones.
	All bool
	// Progress, when set, is called after each completed chunk with the
	// number of episodes done and the total number of episodes. Calls are
	// serialized, done is non-decreasing, and on success the final call has
	// done == total.
	Progress func(done, total int)
}

// Report is the JSON-stable result of an experiment.
type Report struct {
	// Model is the id of the simulated sim/v1 model.
	Model string `json:"model"`
	// Evaluator names the evaluator that produced the estimates.
	Evaluator string `json:"evaluator"`
	// Runs is the number of replications per world and policy.
	Runs int `json:"runs"`
	// Seed is the experiment seed.
	Seed uint64 `json:"seed"`
	// Top is the maximum number of policies listed per world.
	Top int `json:"top"`
	// Worlds holds one entry per evaluated world, in experiment order.
	Worlds []WorldReport `json:"worlds"`
}

// WorldReport holds the policy results of one world.
type WorldReport struct {
	// World is the world id.
	World sim.WorldID `json:"world"`
	// Policies lists the best policies, sorted by objective mean descending
	// then policy id ascending, truncated to Report.Top.
	Policies []PolicyResult `json:"policies"`
	// All lists every policy in the same order. It is only set when
	// Options.All is true.
	All []PolicyResult `json:"all,omitempty"`
}

// PolicyResult holds the estimates of one policy in one world.
type PolicyResult struct {
	// PolicyID is the policy's ID().
	PolicyID string `json:"policyId"`
	// Description is the policy's Describe().
	Description string `json:"description"`
	// Objective estimates the objective value; its ConstraintProb maps every
	// model constraint to the fraction of runs that satisfied it.
	Objective sim.Estimate `json:"objective"`
	// Duration estimates the simulated run duration in seconds; its
	// ConstraintProb is nil.
	Duration sim.Estimate `json:"duration"`
}

// job identifies one chunk of replications of one world.
type job struct {
	world, chunk int
}

// chunkResult is the summary of one job, one entry per policy.
type chunkResult struct {
	job      job
	stats    []*policyStats
	episodes int
	err      error
}

// Run evaluates every policy of src in every world of exp over exp.Def.Runs
// replications with the Monte Carlo evaluator and returns the aggregated
// report. Replication r of world w uses
// sampler.NewMonteCarlo(sim.Seed(exp.Def.Seed, w, r)), re-created for each
// policy (common random numbers). Episodes run on a pool of opts.Workers
// goroutines; the report is identical for any worker count. Run stops
// promptly when ctx is done and returns ctx.Err().
func Run(ctx context.Context, exp *CompiledExperiment, src PolicySource, opts Options) (*Report, error) {
	if exp == nil || exp.Def == nil || exp.Model == nil {
		return nil, errors.New("experiment: compiled experiment is nil")
	}
	if src == nil {
		return nil, errors.New("experiment: policy source is nil")
	}
	if exp.Def.Evaluator == "exact" {
		return nil, ErrExactUnsupported
	}
	if exp.Def.Runs < 1 {
		return nil, fmt.Errorf("experiment: runs must be >= 1, got %d", exp.Def.Runs)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	policies, err := src.Policies(exp.Model)
	if err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		return nil, errors.New("experiment: policy source returned no policies")
	}
	seen := make(map[string]bool, len(policies))
	for _, p := range policies {
		if seen[p.ID()] {
			return nil, fmt.Errorf("experiment: duplicate policy id %q", p.ID())
		}
		seen[p.ID()] = true
	}

	r := &runner{
		exp:         exp,
		policies:    policies,
		constraints: sortedKeys(exp.Model.CompiledConstraints),
		runs:        exp.Def.Runs,
		chunks:      (exp.Def.Runs + ChunkSize - 1) / ChunkSize,
	}
	results, err := r.run(ctx, workerCount(opts, exp), opts.Progress)
	if err != nil {
		return nil, err
	}
	return r.report(results, topCount(opts, exp), opts.All), nil
}

func workerCount(opts Options, exp *CompiledExperiment) int {
	switch {
	case opts.Workers > 0:
		return opts.Workers
	case exp.Def.Workers > 0:
		return exp.Def.Workers
	default:
		return runtime.GOMAXPROCS(0)
	}
}

func topCount(opts Options, exp *CompiledExperiment) int {
	switch {
	case opts.Top > 0:
		return opts.Top
	case exp.Def.Top > 0:
		return exp.Def.Top
	default:
		return DefaultTop
	}
}

// runner holds the immutable state shared by the workers of one Run.
type runner struct {
	exp         *CompiledExperiment
	policies    []sim.Policy
	constraints []string
	runs        int
	chunks      int
}

// run evaluates every job on a pool of workers and returns the chunk
// summaries indexed by [world][chunk].
func (r *runner) run(parent context.Context, workers int, progress func(done, total int)) ([][]*chunkResult, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	jobs := len(r.exp.Worlds) * r.chunks
	workers = min(workers, jobs)
	out := make(chan chunkResult, workers)
	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for ctx.Err() == nil {
				i := int(next.Add(1) - 1)
				if i >= jobs {
					return
				}
				j := job{world: i / r.chunks, chunk: i % r.chunks}
				res := r.runChunk(ctx, j)
				out <- res
				if res.err != nil {
					return
				}
			}
		})
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	results := make([][]*chunkResult, len(r.exp.Worlds))
	for w := range results {
		results[w] = make([]*chunkResult, r.chunks)
	}
	total := len(r.exp.Worlds) * r.runs * len(r.policies)
	done := 0
	var firstErr error
	for res := range out {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
				cancel()
			}
			continue
		}
		if firstErr != nil {
			continue
		}
		results[res.job.world][res.job.chunk] = &res
		done += res.episodes
		if progress != nil {
			progress(done, total)
		}
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// runChunk runs every policy over the replications of one chunk of one
// world, sequentially and in replication order.
func (r *runner) runChunk(ctx context.Context, j job) chunkResult {
	res := chunkResult{job: j, stats: make([]*policyStats, len(r.policies))}
	for i := range res.stats {
		res.stats[i] = newPolicyStats(len(r.constraints))
	}
	world := r.exp.Worlds[j.world]
	first := j.chunk * ChunkSize
	last := min(first+ChunkSize, r.runs)
	for rep := first; rep < last; rep++ {
		seed := sim.Seed(r.exp.Def.Seed, world.ID, rep)
		retain := rep < MaxRetained
		for i, p := range r.policies {
			if err := ctx.Err(); err != nil {
				res.err = err
				return res
			}
			rec, err := kernel.RunEpisode(ctx, r.exp.Model, world, p, sampler.NewMonteCarlo(seed), kernel.Options{})
			if err != nil {
				res.err = fmt.Errorf("experiment: world %q policy %q replication %d: %w", world.ID, p.ID(), rep, err)
				return res
			}
			res.stats[i].add(rec, r.constraints, retain)
			res.episodes++
		}
	}
	return res
}

// report merges the chunk summaries in chunk-index order and builds the
// sorted report.
func (r *runner) report(results [][]*chunkResult, top int, all bool) *Report {
	rep := &Report{
		Model:     r.exp.Model.Def.ID,
		Evaluator: EvaluatorMonteCarlo,
		Runs:      r.runs,
		Seed:      r.exp.Def.Seed,
		Top:       top,
		Worlds:    make([]WorldReport, 0, len(r.exp.Worlds)),
	}
	for w, world := range r.exp.Worlds {
		list := make([]PolicyResult, len(r.policies))
		for i, p := range r.policies {
			agg := newPolicyStats(len(r.constraints))
			for _, chunk := range results[w] {
				agg.merge(chunk.stats[i])
				chunk.stats[i] = nil // release retained values early
			}
			obj, dur := agg.estimates(r.constraints)
			list[i] = PolicyResult{PolicyID: p.ID(), Description: p.Describe(), Objective: obj, Duration: dur}
		}
		sortResults(list)
		wr := WorldReport{World: world.ID, Policies: list[:min(top, len(list))]}
		if all {
			wr.All = list
		}
		rep.Worlds = append(rep.Worlds, wr)
	}
	return rep
}

// sortResults orders results by objective mean descending, then policy id
// ascending.
func sortResults(list []PolicyResult) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Objective.Mean != list[j].Objective.Mean {
			return list[i].Objective.Mean > list[j].Objective.Mean
		}
		return list[i].PolicyID < list[j].PolicyID
	})
}
