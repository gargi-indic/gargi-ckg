package experiment

import (
	"math"
	"sort"

	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// z95 is the two-sided 95% quantile of the standard normal distribution.
const z95 = 1.959963984540054

// accumulator summarizes a stream of values with Welford's algorithm and
// retains values for exact percentiles. Accumulators are merged with Chan's
// parallel update; merging in a fixed order gives bit-identical results.
type accumulator struct {
	n        int
	mean, m2 float64
	min, max float64
	values   []float64
}

// add records x, retaining it for percentiles when retain is true.
func (a *accumulator) add(x float64, retain bool) {
	a.n++
	if a.n == 1 {
		a.min, a.max = x, x
	} else {
		a.min = math.Min(a.min, x)
		a.max = math.Max(a.max, x)
	}
	delta := x - a.mean
	a.mean += delta / float64(a.n)
	a.m2 += delta * (x - a.mean)
	if retain {
		a.values = append(a.values, x)
	}
}

// merge folds b into a (Chan et al. parallel variance update).
func (a *accumulator) merge(b *accumulator) {
	if b.n == 0 {
		return
	}
	a.values = append(a.values, b.values...)
	if a.n == 0 {
		a.n, a.mean, a.m2, a.min, a.max = b.n, b.mean, b.m2, b.min, b.max
		return
	}
	na, nb := float64(a.n), float64(b.n)
	n := na + nb
	delta := b.mean - a.mean
	a.mean += delta * nb / n
	a.m2 += b.m2 + delta*delta*na*nb/n
	a.n += b.n
	a.min = math.Min(a.min, b.min)
	a.max = math.Max(a.max, b.max)
}

// estimate converts the accumulator into a Monte Carlo sim.Estimate. StdDev
// is the sample standard deviation (n-1 denominator, 0 when n < 2); the 95%
// confidence interval of the mean is the normal approximation
// mean ± 1.96·StdDev/√n. P50 and P95 are nearest-rank percentiles of the
// retained values (sorted in place). constraintProb is stored as is.
func (a *accumulator) estimate(constraintProb map[string]float64) sim.Estimate {
	e := sim.Estimate{
		N:              a.n,
		TotalWeight:    float64(a.n),
		ConstraintProb: constraintProb,
	}
	if a.n == 0 {
		return e
	}
	e.Mean = a.mean
	if a.n > 1 {
		e.StdDev = math.Sqrt(a.m2 / float64(a.n-1))
	}
	half := z95 * e.StdDev / math.Sqrt(float64(a.n))
	e.CI95Low, e.CI95High = a.mean-half, a.mean+half
	e.Min, e.Max = a.min, a.max

	sorted := a.sortedValues()
	e.P50 = percentile(sorted, 0.50)
	e.P95 = percentile(sorted, 0.95)
	return e
}

// sortedValues sorts the retained values in place and returns them. The
// accumulator's statistics do not depend on their order.
func (a *accumulator) sortedValues() []float64 {
	sort.Float64s(a.values)
	return a.values
}

// percentile returns the nearest-rank p-th percentile of sorted: the
// smallest value such that at least p·len(sorted) values are less than or
// equal to it. It returns 0 for an empty slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p * float64(len(sorted))))
	rank = max(rank, 1)
	rank = min(rank, len(sorted))
	return sorted[rank-1]
}

// policyStats aggregates the run records of one policy in one world.
type policyStats struct {
	objective accumulator
	duration  accumulator
	// satisfied counts the runs that satisfied each constraint, indexed like
	// evalSpec.constraints.
	satisfied []int
	// metrics accumulates each tracked metric, indexed like
	// evalSpec.metrics.
	metrics []accumulator
	// events counts the runs in which each prob() requirement condition
	// held, indexed like evalSpec.events.
	events []int
}

func newPolicyStats(spec *evalSpec) *policyStats {
	return &policyStats{
		satisfied: make([]int, len(spec.constraints)),
		metrics:   make([]accumulator, len(spec.metrics)),
		events:    make([]int, len(spec.events)),
	}
}

// add records one run; retain controls value retention for percentiles.
func (s *policyStats) add(rec sim.RunRecord, spec *evalSpec, retain bool) {
	s.objective.add(rec.Objective, retain)
	s.duration.add(rec.Duration.Seconds(), retain)
	for i, name := range spec.constraints {
		if rec.Constraints[name] {
			s.satisfied[i]++
		}
	}
	for i := range spec.metrics {
		s.metrics[i].add(spec.metricValue(rec, i), retain)
	}
	for i, name := range spec.events {
		if rec.Events[name] {
			s.events[i]++
		}
	}
}

// merge folds o into s.
func (s *policyStats) merge(o *policyStats) {
	s.objective.merge(&o.objective)
	s.duration.merge(&o.duration)
	for i, c := range o.satisfied {
		s.satisfied[i] += c
	}
	for i := range o.metrics {
		s.metrics[i].merge(&o.metrics[i])
	}
	for i, c := range o.events {
		s.events[i] += c
	}
}

// estimates returns the objective estimate (with constraint probabilities),
// the duration estimate in seconds, the tracked metric estimates and the
// prob() event probabilities. When cover > 0 it also returns the lower tail
// of the retained objective values covering that fraction of them, each
// value weighted 1/(retained values), for the robust report's CVaR.
func (s *policyStats) estimates(spec *evalSpec, cover float64) estimatePair {
	n := float64(s.objective.n)
	frac := func(c int) float64 {
		if n == 0 {
			return 0
		}
		return float64(c) / n
	}
	probs := make(map[string]float64, len(spec.constraints))
	for i, name := range spec.constraints {
		probs[name] = frac(s.satisfied[i])
	}
	pair := estimatePair{objective: s.objective.estimate(probs), duration: s.duration.estimate(nil)}
	if len(spec.metrics) > 0 {
		pair.metrics = make([]sim.Estimate, len(spec.metrics))
		for i := range s.metrics {
			pair.metrics[i] = s.metrics[i].estimate(nil)
		}
	}
	if len(spec.events) > 0 {
		pair.events = make([]float64, len(spec.events))
		for i, c := range s.events {
			pair.events[i] = frac(c)
		}
	}
	if cover > 0 {
		pair.tail = sortedTail(s.objective.sortedValues(), cover)
	}
	return pair
}
