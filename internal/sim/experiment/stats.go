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
// retained values. constraintProb is stored as is.
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

	sorted := append([]float64(nil), a.values...)
	sort.Float64s(sorted)
	e.P50 = percentile(sorted, 0.50)
	e.P95 = percentile(sorted, 0.95)
	return e
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
	// the experiment's sorted constraint names.
	satisfied []int
}

func newPolicyStats(constraints int) *policyStats {
	return &policyStats{satisfied: make([]int, constraints)}
}

// add records one run; retain controls value retention for percentiles.
func (s *policyStats) add(rec sim.RunRecord, constraints []string, retain bool) {
	s.objective.add(rec.Objective, retain)
	s.duration.add(rec.Duration.Seconds(), retain)
	for i, name := range constraints {
		if rec.Constraints[name] {
			s.satisfied[i]++
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
}

// estimates returns the objective estimate (with constraint probabilities)
// and the duration estimate in seconds.
func (s *policyStats) estimates(constraints []string) (sim.Estimate, sim.Estimate) {
	probs := make(map[string]float64, len(constraints))
	for i, name := range constraints {
		if s.objective.n > 0 {
			probs[name] = float64(s.satisfied[i]) / float64(s.objective.n)
		} else {
			probs[name] = 0
		}
	}
	return s.objective.estimate(probs), s.duration.estimate(nil)
}
