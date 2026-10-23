package experiment

import (
	"math"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/sim/report"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

func TestPercentile_NearestRank(t *testing.T) {
	sorted := []float64{15, 20, 35, 40, 50}
	require.Equal(t, 35.0, percentile(sorted, 0.50))
	require.Equal(t, 50.0, percentile(sorted, 0.95))
	require.Equal(t, 15.0, percentile(sorted, 0.0))
	require.Equal(t, 50.0, percentile(sorted, 1.0))
	require.Equal(t, 40.0, percentile(sorted, 0.80))

	ten := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	require.Equal(t, 5.0, percentile(ten, 0.50))
	require.Equal(t, 10.0, percentile(ten, 0.95))

	require.Equal(t, 7.0, percentile([]float64{7}, 0.5))
	require.Equal(t, 0.0, percentile(nil, 0.5))
}

func TestEstimate_HandComputed(t *testing.T) {
	var a accumulator
	for _, x := range []float64{4, 1, 10, 3, 2} {
		a.add(x, true)
	}
	e := a.estimate(map[string]float64{})

	// mean 4; squared deviations 0+9+36+1+4 = 50; sample variance 50/4.
	sd := math.Sqrt(12.5)
	half := 1.959963984540054 * sd / math.Sqrt(5)
	require.Equal(t, 5, e.N)
	require.Equal(t, 5.0, e.TotalWeight)
	require.InDelta(t, 4.0, e.Mean, 1e-12)
	require.InDelta(t, sd, e.StdDev, 1e-12)
	require.InDelta(t, 4-half, e.CI95Low, 1e-12)
	require.InDelta(t, 4+half, e.CI95High, 1e-12)
	require.Equal(t, 3.0, e.P50)
	require.Equal(t, 10.0, e.P95)
	require.Equal(t, 1.0, e.Min)
	require.Equal(t, 10.0, e.Max)
	require.False(t, e.Exact)
	require.NotNil(t, e.ConstraintProb)

	var one accumulator
	one.add(-2.5, true)
	e1 := one.estimate(nil)
	require.Equal(t, -2.5, e1.Mean)
	require.Zero(t, e1.StdDev)
	require.Equal(t, -2.5, e1.CI95Low)
	require.Equal(t, -2.5, e1.CI95High)
	require.Equal(t, -2.5, e1.P50)
}

func TestAccumulator_MergeMatchesSequential(t *testing.T) {
	values := make([]float64, 2537)
	for i := range values {
		values[i] = math.Sin(float64(i)) * float64(i%97)
	}
	var seq accumulator
	for _, v := range values {
		seq.add(v, true)
	}
	var merged accumulator
	for start := 0; start < len(values); start += 300 {
		var c accumulator
		for _, v := range values[start:min(start+300, len(values))] {
			c.add(v, true)
		}
		merged.merge(&c)
	}
	a, b := seq.estimate(nil), merged.estimate(nil)
	require.Equal(t, a.N, b.N)
	require.InDelta(t, a.Mean, b.Mean, 1e-9)
	require.InDelta(t, a.StdDev, b.StdDev, 1e-9)
	require.Equal(t, a.P50, b.P50)
	require.Equal(t, a.P95, b.P95)
	require.Equal(t, a.Min, b.Min)
	require.Equal(t, a.Max, b.Max)
}

func TestAccumulator_RetentionCapOnlyAffectsPercentiles(t *testing.T) {
	var a accumulator
	a.add(1, true)
	a.add(2, true)
	a.add(100, false) // beyond the cap: counted, not retained
	e := a.estimate(nil)
	require.Equal(t, 3, e.N)
	require.InDelta(t, 103.0/3, e.Mean, 1e-12)
	require.Equal(t, 100.0, e.Max)
	require.Equal(t, 1.0, e.P50)
	require.Equal(t, 2.0, e.P95)
}

func TestPolicyStats_ConstraintProbAndDuration(t *testing.T) {
	spec := &evalSpec{constraints: []string{"cheap", "fast"}}
	s := newPolicyStats(spec)
	recs := []sim.RunRecord{
		{Objective: 1, Duration: time.Hour, Constraints: map[string]bool{"cheap": true, "fast": true}},
		{Objective: 2, Duration: 2 * time.Hour, Constraints: map[string]bool{"cheap": true, "fast": false}},
		{Objective: 3, Duration: 0, Constraints: map[string]bool{"cheap": false}},
		{Objective: 4, Duration: 0, Constraints: nil},
	}
	for _, r := range recs {
		s.add(r, spec, true)
	}
	pair := s.estimates(spec, 0)
	obj, dur := pair.objective, pair.duration
	require.Equal(t, map[string]float64{"cheap": 0.5, "fast": 0.25}, obj.ConstraintProb)
	require.InDelta(t, 2.5, obj.Mean, 1e-12)
	require.InDelta(t, (3600.0+7200.0)/4, dur.Mean, 1e-9)
	require.Equal(t, 7200.0, dur.Max)
	require.Nil(t, dur.ConstraintProb)
}

func TestSortResults_MeanDescThenID(t *testing.T) {
	list := []PolicyResult{
		{PolicyID: "b", Objective: sim.Estimate{Mean: 1}},
		{PolicyID: "c", Objective: sim.Estimate{Mean: 5}},
		{PolicyID: "a", Objective: sim.Estimate{Mean: 1}},
		{PolicyID: "d", Objective: sim.Estimate{Mean: -3}},
	}
	sortResults(list)
	ids := make([]string, len(list))
	for i, r := range list {
		ids[i] = r.PolicyID
	}
	require.Equal(t, []string{"c", "a", "b", "d"}, ids)
}

func TestSortResults_FeasibleFirst(t *testing.T) {
	yes, no := true, false
	list := []PolicyResult{
		{PolicyID: "a", Objective: sim.Estimate{Mean: 9}, Feasible: &no},
		{PolicyID: "b", Objective: sim.Estimate{Mean: 1}, Feasible: &yes},
		{PolicyID: "c", Objective: sim.Estimate{Mean: 5}, Feasible: &no},
		{PolicyID: "d", Objective: sim.Estimate{Mean: 2}, Feasible: &yes},
	}
	sortResults(list)
	ids := make([]string, len(list))
	for i, r := range list {
		ids[i] = r.PolicyID
	}
	require.Equal(t, []string{"d", "b", "a", "c"}, ids)
}

func TestSortedTail(t *testing.T) {
	sorted := []float64{1, 2, 2, 3, 4, 5, 6, 7, 8, 9}
	atoms := func(pairs ...float64) []report.Atom {
		var out []report.Atom
		for i := 0; i < len(pairs); i += 2 {
			out = append(out, report.Atom{Value: pairs[i], Weight: pairs[i+1]})
		}
		return out
	}
	requireAtoms := func(want, got []report.Atom) {
		t.Helper()
		require.Len(t, got, len(want))
		for i := range want {
			require.Equal(t, want[i].Value, got[i].Value)
			require.InDelta(t, want[i].Weight, got[i].Weight, 1e-12)
		}
	}
	// 25% of 10 values needs 3: 1, 2, 2.
	requireAtoms(atoms(1, 0.1, 2, 0.2), sortedTail(sorted, 0.25))
	// 15% needs 2, and the atom at 2 is completed.
	requireAtoms(atoms(1, 0.1, 2, 0.2), sortedTail(sorted, 0.15))
	requireAtoms(atoms(1, 0.1, 2, 0.2, 3, 0.1), sortedTail(sorted, 0.31))
	require.Len(t, sortedTail(sorted, 1), 9)
	require.Nil(t, sortedTail(sorted, 0))
	require.Nil(t, sortedTail(nil, 0.5))

	outs := []outcome{{weight: 0.5, objective: 10}, {weight: 0.3, objective: -5}, {weight: 0.2, objective: 0}}
	requireAtoms(atoms(-5, 0.3), exactTail(outs, 1, 0.3))
	requireAtoms(atoms(-5, 0.3, 0, 0.2), exactTail(outs, 1, 0.31))
	requireAtoms(atoms(-5, 0.3, 0, 0.2, 10, 0.5), exactTail(outs, 1, 1))
	// Weights are normalized by the total.
	requireAtoms(atoms(-5, 0.6), exactTail([]outcome{{weight: 0.3, objective: -5}, {weight: 0.2, objective: 0}}, 0.5, 0.1))
	require.Nil(t, exactTail(outs, 1, 0))
}

// TestPolicyStats_TailGivesFullCVaR checks that the truncated Monte Carlo
// tails of two worlds give the same CVaR as the full prior-weighted mixture.
func TestPolicyStats_TailGivesFullCVaR(t *testing.T) {
	const alpha = 0.1
	prior := []float64{0.7, 0.3}
	spec := &evalSpec{}
	var mixture, tails []report.Atom
	for w, n := range []int{997, 1500} {
		s := newPolicyStats(spec)
		for i := range n {
			v := math.Sin(float64(i*(w+3))) * 100 // distinct, unordered values
			s.add(sim.RunRecord{Objective: v}, spec, true)
			mixture = append(mixture, report.Atom{Value: v, Weight: prior[w] / float64(n)})
		}
		pair := s.estimates(spec, report.TailCover(prior[w], alpha))
		require.Less(t, len(pair.tail), n/2, "the tail is truncated")
		for _, a := range pair.tail {
			tails = append(tails, report.Atom{Value: a.Value, Weight: prior[w] * a.Weight})
		}
	}
	require.InDelta(t, report.CVaR(mixture, alpha), report.CVaR(tails, alpha), 1e-9)
}
