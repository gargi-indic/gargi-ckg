package experiment

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gargi-indic/gargi-flow/internal/sim/kernel"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/internal/sim/report"
	"github.com/gargi-indic/gargi-flow/internal/sim/sampler"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// weightTolerance is the largest accepted deviation of the summed branch
// weights of one exact enumeration from 1.
const weightTolerance = 1e-12

// requireDiscrete returns nil when m can be enumerated exactly, otherwise a
// *engine.ValidationErrors with one sim.CodeModelNotDiscrete issue per reason
// reported by CompiledModel.Discrete. Reasons have the form
// "<path>: <message>"; the path is reported relative to the model document
// ("$.model:<path>"), the convention used by Compile.
func requireDiscrete(m *model.CompiledModel, what string) error {
	ok, reasons := m.Discrete()
	if ok {
		return nil
	}
	errs := &engine.ValidationErrors{}
	modelAt := engine.PathRoot.Field("model").String()
	for _, r := range reasons {
		path, msg, found := strings.Cut(r, ": ")
		if !found {
			path, msg = "", r
		}
		errs.Add(modelAt+":"+path, sim.CodeModelNotDiscrete, fmt.Sprintf("%s; %s requires a discrete model", msg, what))
	}
	return sortIssues(errs)
}

// outcome is one enumerated chance branch of an episode.
type outcome struct {
	weight    float64
	objective float64
	duration  float64
	satisfied []bool
	// metrics holds the tracked metric values (evalSpec.metrics order).
	metrics []float64
	// events holds the prob() conditions (evalSpec.events order).
	events []bool
}

// evaluateExact enumerates every chance branch of policy p in world w with
// sampler.NewEnumerator, re-running kernel.RunEpisode once per branch, and
// returns the exact objective (with constraint probabilities) and duration
// estimates. The summed branch weight must be 1 within weightTolerance.
func evaluateExact(ctx context.Context, m *model.CompiledModel, w sim.World, p sim.Policy, constraints []string) (sim.Estimate, sim.Estimate, error) {
	pair, err := evaluateExactPair(ctx, m, w, p, &evalSpec{constraints: constraints}, 0)
	return pair.objective, pair.duration, err
}

// evaluateExactPair is evaluateExact with the tracked metrics, the prob()
// event probabilities and, when cover > 0, the lower tail of the objective
// distribution covering that fraction of its probability.
func evaluateExactPair(ctx context.Context, m *model.CompiledModel, w sim.World, p sim.Policy, spec *evalSpec, cover float64) (estimatePair, error) {
	var outs []outcome
	enum := sampler.NewEnumerator()
	for enum.Next() {
		if err := ctx.Err(); err != nil {
			return estimatePair{}, err
		}
		rec, err := kernel.RunEpisode(ctx, m, w, p, enum.Sampler(), kernel.Options{})
		if err != nil {
			return estimatePair{}, fmt.Errorf("experiment: exact: world %q policy %q branch %d: %w", w.ID, p.ID(), len(outs), err)
		}
		o := outcome{
			weight:    enum.Weight(),
			objective: rec.Objective,
			duration:  rec.Duration.Seconds(),
			satisfied: make([]bool, len(spec.constraints)),
		}
		for i, name := range spec.constraints {
			o.satisfied[i] = rec.Constraints[name]
		}
		if len(spec.metrics) > 0 {
			o.metrics = make([]float64, len(spec.metrics))
			for i := range spec.metrics {
				o.metrics[i] = spec.metricValue(rec, i)
			}
		}
		if len(spec.events) > 0 {
			o.events = make([]bool, len(spec.events))
			for i, name := range spec.events {
				o.events[i] = rec.Events[name]
			}
		}
		outs = append(outs, o)
	}

	total := 0.0
	for _, o := range outs {
		total += o.weight
	}
	if math.Abs(total-1) > weightTolerance {
		return estimatePair{}, fmt.Errorf("experiment: exact: world %q policy %q: branch weights sum to %.17g, want 1", w.ID, p.ID(), total)
	}

	prob := func(holds func(outcome) bool) float64 {
		sum := 0.0
		for _, o := range outs {
			if holds(o) {
				sum += o.weight
			}
		}
		return sum / total
	}
	probs := make(map[string]float64, len(spec.constraints))
	for i, name := range spec.constraints {
		probs[name] = prob(func(o outcome) bool { return o.satisfied[i] })
	}
	pair := estimatePair{
		objective: exactEstimate(outs, total, func(o outcome) float64 { return o.objective }, probs),
		duration:  exactEstimate(outs, total, func(o outcome) float64 { return o.duration }, nil),
	}
	if len(spec.metrics) > 0 {
		pair.metrics = make([]sim.Estimate, len(spec.metrics))
		for i := range spec.metrics {
			pair.metrics[i] = exactEstimate(outs, total, func(o outcome) float64 { return o.metrics[i] }, nil)
		}
	}
	if len(spec.events) > 0 {
		pair.events = make([]float64, len(spec.events))
		for i := range spec.events {
			pair.events[i] = prob(func(o outcome) bool { return o.events[i] })
		}
	}
	if cover > 0 {
		pair.tail = exactTail(outs, total, cover)
	}
	return pair, nil
}

// weighted is one value of a discrete distribution with its probability mass.
type weighted struct {
	value, weight float64
}

// exactEstimate summarizes the discrete distribution of value over outs.
// Mean and StdDev are the probability-weighted mean and population standard
// deviation; the confidence interval collapses to the mean because there is
// no sampling error. P50 and P95 are weighted nearest-rank percentiles: the
// smallest value whose cumulative probability reaches p. Min and Max range
// over the branches (all of which have positive probability).
func exactEstimate(outs []outcome, total float64, value func(outcome) float64, constraintProb map[string]float64) sim.Estimate {
	e := sim.Estimate{N: len(outs), TotalWeight: total, ConstraintProb: constraintProb, Exact: true}
	if len(outs) == 0 {
		return e
	}
	dist := make([]weighted, len(outs))
	mean := 0.0
	for i, o := range outs {
		v := value(o)
		dist[i] = weighted{value: v, weight: o.weight}
		mean += o.weight * v
	}
	mean /= total
	variance := 0.0
	for _, d := range dist {
		dv := d.value - mean
		variance += d.weight * dv * dv
	}
	variance /= total

	sort.SliceStable(dist, func(i, j int) bool { return dist[i].value < dist[j].value })
	e.Mean = mean
	e.StdDev = math.Sqrt(variance)
	e.CI95Low, e.CI95High = mean, mean
	e.Min, e.Max = dist[0].value, dist[len(dist)-1].value
	e.P50 = weightedPercentile(dist, total, 0.50)
	e.P95 = weightedPercentile(dist, total, 0.95)
	return e
}

// weightedPercentile returns the smallest value of sorted whose cumulative
// weight reaches p·total (with weightTolerance slack for rounding).
func weightedPercentile(sorted []weighted, total, p float64) float64 {
	target := p*total - weightTolerance
	cum := 0.0
	for _, d := range sorted {
		cum += d.weight
		if cum >= target {
			return d.value
		}
	}
	return sorted[len(sorted)-1].value
}

// estimatePair holds the estimates of one (world, policy) pair, from either
// evaluator.
type estimatePair struct {
	objective, duration sim.Estimate
	// metrics holds the tracked metric estimates (evalSpec.metrics order).
	metrics []sim.Estimate
	// events holds the prob() requirement probabilities (evalSpec.events
	// order).
	events []float64
	// tail is the lower tail of the objective distribution for the robust
	// report's CVaR; nil unless requested.
	tail []report.Atom
}

// runExact evaluates every (world, policy) pair exactly on a pool of
// workers. Results are stored by index, so they do not depend on the worker
// count. progress, when set, is called after every pair with the number of
// pairs done and the total number of pairs.
// covers, when set, holds the CVaR tail cover of each world.
func runExact(parent context.Context, exp *CompiledExperiment, policies []sim.Policy, spec *evalSpec, covers []float64, workers int, progress func(done, total int)) ([][]estimatePair, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	np := len(policies)
	jobs := len(exp.Worlds) * np
	results := make([][]estimatePair, len(exp.Worlds))
	for w := range results {
		results[w] = make([]estimatePair, np)
	}

	var (
		next     atomic.Int64
		wg       sync.WaitGroup
		mu       sync.Mutex
		done     int
		firstErr error
	)
	for range min(workers, jobs) {
		wg.Go(func() {
			for ctx.Err() == nil {
				i := int(next.Add(1) - 1)
				if i >= jobs {
					return
				}
				w, pi := i/np, i%np
				cover := 0.0
				if covers != nil {
					cover = covers[w]
				}
				pair, err := evaluateExactPair(ctx, exp.Model, exp.Worlds[w], policies[pi], spec, cover)
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					mu.Unlock()
					return
				}
				results[w][pi] = pair
				done++
				if progress != nil && firstErr == nil {
					progress(done, jobs)
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}
