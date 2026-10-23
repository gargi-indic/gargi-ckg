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
}

// evaluateExact enumerates every chance branch of policy p in world w with
// sampler.NewEnumerator, re-running kernel.RunEpisode once per branch, and
// returns the exact objective (with constraint probabilities) and duration
// estimates. The summed branch weight must be 1 within weightTolerance.
func evaluateExact(ctx context.Context, m *model.CompiledModel, w sim.World, p sim.Policy, constraints []string) (sim.Estimate, sim.Estimate, error) {
	var outs []outcome
	enum := sampler.NewEnumerator()
	for enum.Next() {
		if err := ctx.Err(); err != nil {
			return sim.Estimate{}, sim.Estimate{}, err
		}
		rec, err := kernel.RunEpisode(ctx, m, w, p, enum.Sampler(), kernel.Options{})
		if err != nil {
			return sim.Estimate{}, sim.Estimate{}, fmt.Errorf("experiment: exact: world %q policy %q branch %d: %w", w.ID, p.ID(), len(outs), err)
		}
		o := outcome{
			weight:    enum.Weight(),
			objective: rec.Objective,
			duration:  rec.Duration.Seconds(),
			satisfied: make([]bool, len(constraints)),
		}
		for i, name := range constraints {
			o.satisfied[i] = rec.Constraints[name]
		}
		outs = append(outs, o)
	}

	total := 0.0
	for _, o := range outs {
		total += o.weight
	}
	if math.Abs(total-1) > weightTolerance {
		return sim.Estimate{}, sim.Estimate{}, fmt.Errorf("experiment: exact: world %q policy %q: branch weights sum to %.17g, want 1", w.ID, p.ID(), total)
	}

	probs := make(map[string]float64, len(constraints))
	for i, name := range constraints {
		sum := 0.0
		for _, o := range outs {
			if o.satisfied[i] {
				sum += o.weight
			}
		}
		probs[name] = sum / total
	}
	obj := exactEstimate(outs, total, func(o outcome) float64 { return o.objective }, probs)
	dur := exactEstimate(outs, total, func(o outcome) float64 { return o.duration }, nil)
	return obj, dur, nil
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

// estimatePair holds the objective and duration estimates of one (world,
// policy) pair, from either evaluator.
type estimatePair struct {
	objective, duration sim.Estimate
}

// runExact evaluates every (world, policy) pair exactly on a pool of
// workers. Results are stored by index, so they do not depend on the worker
// count. progress, when set, is called after every pair with the number of
// pairs done and the total number of pairs.
func runExact(parent context.Context, exp *CompiledExperiment, policies []sim.Policy, constraints []string, workers int, progress func(done, total int)) ([][]estimatePair, error) {
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
				obj, dur, err := evaluateExact(ctx, exp.Model, exp.Worlds[w], policies[pi], constraints)
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					mu.Unlock()
					return
				}
				results[w][pi] = estimatePair{objective: obj, duration: dur}
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
