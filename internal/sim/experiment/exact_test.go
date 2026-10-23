package experiment

import (
	"bytes"
	"context"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

// prototypeEV lists every policy of docs/simulation/prototypes/loan_sim.py
// (all_policies, describe) with its exact EV per world, as printed by the
// prototype's exact() with python3.
var prototypeEV = []struct {
	desc            string
	base, recession float64
}{
	{"D1=skip, none->approve", 0.0, -1450.0},
	{"D1=skip, none->reject", 0.0, 0.0},
	{"D1=skip, none->review, none/pass->approve, none/fail->approve", -390.00000000000017, -1795.0},
	{"D1=skip, none->review, none/pass->approve, none/fail->reject", 301.99999999999994, 72.24999999999997},
	{"D1=skip, none->review, none/pass->reject, none/fail->approve", -842.0000000000001, -2017.25},
	{"D1=skip, none->review, none/pass->reject, none/fail->reject", -150.0, -150.0},
	{"D1=score, low->approve, high->approve", -19.999999999999773, -1470.0},
	{"D1=score, low->approve, high->reject", 460.0000000000001, -64.99999999999991},
	{"D1=score, low->approve, high->review, high/pass->approve, high/fail->approve", -76.99999999999994, -1536.0},
	{"D1=score, low->approve, high->review, high/pass->approve, high/fail->reject", 424.20000000000005, -215.27499999999998},
	{"D1=score, low->approve, high->review, high/pass->reject, high/fail->approve", -74.19999999999987, -1432.225},
	{"D1=score, low->approve, high->review, high/pass->reject, high/fail->reject", 427.0000000000001, -111.5},
	{"D1=score, low->reject, high->approve", -500.0, -1424.9999999999998},
	{"D1=score, low->reject, high->reject", -20.0, -20.0},
	{"D1=score, low->reject, high->review, high/pass->approve, high/fail->approve", -557.0, -1491.0},
	{"D1=score, low->reject, high->review, high/pass->approve, high/fail->reject", -55.80000000000001, -170.27499999999998},
	{"D1=score, low->reject, high->review, high/pass->reject, high/fail->approve", -554.1999999999999, -1387.225},
	{"D1=score, low->reject, high->review, high/pass->reject, high/fail->reject", -53.0, -66.5},
	{"D1=score, low->review, high->approve, low/pass->approve, low/fail->approve", -353.0, -1749.0},
	{"D1=score, low->review, high->approve, low/pass->approve, low/fail->reject", -162.2, -1202.475},
	{"D1=score, low->review, high->approve, low/pass->reject, low/fail->approve", -807.8, -2075.025},
	{"D1=score, low->review, high->approve, low/pass->reject, low/fail->reject", -617.0, -1528.5},
	{"D1=score, low->review, high->reject, low/pass->approve, low/fail->approve", 126.99999999999996, -344.00000000000006},
	{"D1=score, low->review, high->reject, low/pass->approve, low/fail->reject", 317.79999999999995, 202.52499999999995},
	{"D1=score, low->review, high->reject, low/pass->reject, low/fail->approve", -327.8, -670.025},
	{"D1=score, low->review, high->reject, low/pass->reject, low/fail->reject", -137.00000000000003, -123.50000000000001},
	{"D1=score, low->review, high->review, low/pass->approve, low/fail->approve, high/pass->approve, high/fail->approve", -409.99999999999994, -1815.0},
	{"D1=score, low->review, high->review, low/pass->approve, low/fail->approve, high/pass->approve, high/fail->reject", 91.2, -494.2750000000001},
	{"D1=score, low->review, high->review, low/pass->approve, low/fail->approve, high/pass->reject, high/fail->approve", -407.19999999999993, -1711.225},
	{"D1=score, low->review, high->review, low/pass->approve, low/fail->approve, high/pass->reject, high/fail->reject", 94.00000000000001, -390.5000000000001},
	{"D1=score, low->review, high->review, low/pass->approve, low/fail->reject, high/pass->approve, high/fail->approve", -219.19999999999993, -1268.475},
	{"D1=score, low->review, high->review, low/pass->approve, low/fail->reject, high/pass->approve, high/fail->reject", 282.0, 52.24999999999999},
	{"D1=score, low->review, high->review, low/pass->approve, low/fail->reject, high/pass->reject, high/fail->approve", -216.39999999999992, -1164.6999999999998},
	{"D1=score, low->review, high->review, low/pass->approve, low/fail->reject, high/pass->reject, high/fail->reject", 284.8, 156.02499999999998},
	{"D1=score, low->review, high->review, low/pass->reject, low/fail->approve, high/pass->approve, high/fail->approve", -864.8, -2141.0249999999996},
	{"D1=score, low->review, high->review, low/pass->reject, low/fail->approve, high/pass->approve, high/fail->reject", -363.6, -820.3000000000001},
	{"D1=score, low->review, high->review, low/pass->reject, low/fail->approve, high/pass->reject, high/fail->approve", -862.0, -2037.25},
	{"D1=score, low->review, high->review, low/pass->reject, low/fail->approve, high/pass->reject, high/fail->reject", -360.8, -716.525},
	{"D1=score, low->review, high->review, low/pass->reject, low/fail->reject, high/pass->approve, high/fail->approve", -674.0, -1594.5},
	{"D1=score, low->review, high->review, low/pass->reject, low/fail->reject, high/pass->approve, high/fail->reject", -172.8, -273.77500000000003},
	{"D1=score, low->review, high->review, low/pass->reject, low/fail->reject, high/pass->reject, high/fail->approve", -671.1999999999999, -1490.725},
	{"D1=score, low->review, high->review, low/pass->reject, low/fail->reject, high/pass->reject, high/fail->reject", -170.0, -170.0},
}

const (
	bestBaseDesc      = "screen=score; route[score,high]=reject; route[score,low]=approve"
	bestRecessionDesc = "screen=score; route[score,high]=reject; route[score,low]=review; final[score,low,fail]=reject; final[score,low,pass]=approve"
)

// prototypeToDescribe translates a prototype description into the
// TablePolicy.Describe format: decisions in graph order (screen, route,
// final), information sets sorted by label.
func prototypeToDescribe(t *testing.T, desc string) string {
	t.Helper()
	items := strings.Split(desc, ", ")
	d1, ok := strings.CutPrefix(items[0], "D1=")
	require.True(t, ok, desc)
	band := func(o string) string {
		if o == "none" {
			return "nil"
		}
		return o
	}
	var route, final []string
	for _, it := range items[1:] {
		obs, act, ok := strings.Cut(it, "->")
		require.True(t, ok, desc)
		if o, r, review := strings.Cut(obs, "/"); review {
			final = append(final, "final["+d1+","+band(o)+","+r+"]="+act)
		} else {
			route = append(route, "route["+d1+","+band(obs)+"]="+act)
		}
	}
	sort.Strings(route)
	sort.Strings(final)
	return strings.Join(append(append([]string{"screen=" + d1}, route...), final...), "; ")
}

// withEvaluator returns a copy of exp using evaluator and runs.
func withEvaluator(exp *CompiledExperiment, evaluator string, runs int) *CompiledExperiment {
	c := *exp
	def := *exp.Def
	def.Evaluator = evaluator
	def.Runs = runs
	c.Def = &def
	return &c
}

func loadLoanFile(t *testing.T) *CompiledExperiment {
	t.Helper()
	exp, err := LoadFile(filepath.Join(loanDir, "experiment.yaml"), registry(t))
	require.NoError(t, err)
	return exp
}

func TestExhaustive_Loan42Policies(t *testing.T) {
	exp := loadLoanFile(t)
	src, ok := exp.PolicySource().(ExhaustiveSource)
	require.True(t, ok)
	require.Equal(t, 0, src.MaxPolicies)
	policies, err := src.Policies(exp.Model)
	require.NoError(t, err)
	require.Len(t, policies, 42)

	ids := map[string]bool{}
	descs := map[string]bool{}
	for _, p := range policies {
		tp, ok := p.(*TablePolicy)
		require.True(t, ok)
		require.True(t, strings.HasPrefix(tp.ID(), "loan-underwriting@exhaustive-"), tp.ID())
		ids[p.ID()] = true
		descs[p.Describe()] = true
	}
	require.Len(t, ids, 42)
	require.Len(t, descs, 42)

	want := map[string]bool{}
	for _, row := range prototypeEV {
		want[prototypeToDescribe(t, row.desc)] = true
	}
	require.Equal(t, want, descs)
	require.True(t, descs[bestBaseDesc])
	require.True(t, descs["screen=skip; route[skip,nil]=approve"])

	// Enumeration is deterministic: same order, ids and descriptions.
	again, err := src.Policies(exp.Model)
	require.NoError(t, err)
	for i := range policies {
		require.Equal(t, policies[i].ID(), again[i].ID())
		require.Equal(t, policies[i].Describe(), again[i].Describe())
	}

	// Without worlds every model world is covered.
	all, err := ExhaustiveSource{}.Policies(exp.Model)
	require.NoError(t, err)
	require.Len(t, all, 42)
}

func TestExhaustive_MaxPolicies(t *testing.T) {
	exp := loadLoanFile(t)
	_, err := ExhaustiveSource{MaxPolicies: 41}.Policies(exp.Model)
	var ve *engine.ValidationErrors
	require.ErrorAs(t, err, &ve)
	require.Len(t, ve.Issues(), 1)
	is := ve.Issues()[0]
	require.Equal(t, sim.CodeTooManyPolicies, is.Code)
	require.Equal(t, "$.policies.search", is.Path)
	require.Contains(t, is.Message, "more than 41 policies")
	require.Contains(t, is.Message, "exact solver (policies: {search: exact})")

	policies, err := ExhaustiveSource{MaxPolicies: 42}.Policies(exp.Model)
	require.NoError(t, err)
	require.Len(t, policies, 42)

	// max_policies is read from the experiment document.
	def, err := model.ParseExperiment([]byte("apiVersion: experiment/v1\nmodel: ./sim.yaml\npolicies: {search: exhaustive}\nevaluator: exact\nruns: 1\nseed: 1\nmax_policies: 7\n"), engine.FormatYAML)
	require.NoError(t, err)
	require.Equal(t, 7, def.MaxPolicies)
	cexp, errs := Compile(def, loanDir, registry(t))
	require.Zero(t, errs.Len())
	_, err = Run(context.Background(), cexp, cexp.PolicySource(), Options{})
	require.ErrorAs(t, err, &ve)
	require.Equal(t, sim.CodeTooManyPolicies, ve.Issues()[0].Code)

	_, err = model.ParseExperiment([]byte("apiVersion: experiment/v1\nmodel: ./sim.yaml\npolicies: {search: exhaustive}\nruns: 1\nseed: 1\nmax_policies: -1\n"), engine.FormatYAML)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, "$.max_policies", ve.Issues()[0].Path)
}

// TestExact_LoanMatchesPrototype runs the loan experiment (search:
// exhaustive, evaluator: exact) through Run and compares every policy's
// exact EV in both worlds with the prototype.
func TestExact_LoanMatchesPrototype(t *testing.T) {
	exp := withEvaluator(loadLoanFile(t), EvaluatorExact, 1)
	rep, err := Run(context.Background(), exp, exp.PolicySource(), Options{All: true, Workers: 4})
	require.NoError(t, err)
	require.Equal(t, EvaluatorExact, rep.Evaluator)
	require.Equal(t, 0, rep.Runs)
	require.Len(t, rep.Worlds, 2)

	byWorld := map[sim.WorldID]map[string]PolicyResult{}
	for _, w := range rep.Worlds {
		require.Len(t, w.All, 42)
		require.Len(t, w.Policies, 5)
		m := map[string]PolicyResult{}
		for _, r := range w.All {
			require.True(t, r.Objective.Exact)
			require.True(t, r.Duration.Exact)
			require.Nil(t, r.MonteCarlo)
			require.InDelta(t, 1.0, r.Objective.TotalWeight, 1e-12)
			m[r.Description] = r
		}
		byWorld[w.World] = m
	}
	for _, row := range prototypeEV {
		d := prototypeToDescribe(t, row.desc)
		require.InDelta(t, row.base, byWorld["base"][d].Objective.Mean, 1e-9, d)
		require.InDelta(t, row.recession, byWorld["recession"][d].Objective.Mean, 1e-9, d)
	}

	base, recession := rep.Worlds[0].Policies[0], rep.Worlds[1].Policies[0]
	require.Equal(t, bestBaseDesc, base.Description)
	require.InDelta(t, 460.0, base.Objective.Mean, 1e-9)
	require.Equal(t, bestRecessionDesc, recession.Description)
	require.InDelta(t, 202.525, recession.Objective.Mean, 1e-9)
	require.Equal(t, 202.5, math.Round(recession.Objective.Mean*10)/10, "the epic rounds to 202.5")

	// Worker count does not change the report.
	one, err := Run(context.Background(), exp, exp.PolicySource(), Options{All: true, Workers: 1})
	require.NoError(t, err)
	require.Equal(t, marshal(t, one), marshal(t, rep))
}

// TestExact_RecessionV1 checks the hand-written recession-v1 policy and the
// exact statistics of its outcome distribution.
func TestExact_RecessionV1(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "graph.yaml", readLoan(t, "graph.yaml"))
	s := strings.Replace(readLoan(t, "sim.yaml"), "  maximize: mean\n",
		"  maximize: mean\n  constraints:\n    approved: \"nodes.approve.status == 'Succeeded'\"\n", 1)
	writeFile(t, dir, "sim.yaml", s)
	exp := withEvaluator(loanExperiment(t, dir, 1, "recession"), EvaluatorExact, 1)
	src := FilesSource{Files: []string{filepath.Join(loanDir, recessionFile)}}

	var mu sync.Mutex
	var calls [][2]int
	rep, err := Run(context.Background(), exp, src, Options{Progress: func(done, total int) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, [2]int{done, total})
	}})
	require.NoError(t, err)
	require.Equal(t, [][2]int{{1, 1}}, calls)

	est := rep.Worlds[0].Policies[0].Objective
	require.Equal(t, recessionID, rep.Worlds[0].Policies[0].PolicyID)
	require.InDelta(t, recessionValue, est.Mean, 1e-9)
	require.InDelta(t, 0.399525, est.ConstraintProb["approved"], 1e-12)
	require.Equal(t, est.Mean, est.CI95Low)
	require.Equal(t, est.Mean, est.CI95High)
	// Outcomes: -6000-170 (bad, low, pass), -170 (review, rejected or
	// abandoned), -20 (high, rejected), 1000-170 (good, low, pass).
	require.InDelta(t, -6170.0, est.Min, 1e-9)
	require.InDelta(t, 830.0, est.Max, 1e-9)
	require.Greater(t, est.StdDev, 0.0)
	require.InDelta(t, -20.0, est.P50, 1e-9)
	require.InDelta(t, 830.0, est.P95, 1e-9)
	require.Positive(t, est.N)
	t.Logf("recession-v1 in recession: %d branches, %+v", est.N, est)

	dur := rep.Worlds[0].Policies[0].Duration
	require.True(t, dur.Exact)
	require.Nil(t, dur.ConstraintProb)
	require.InDelta(t, 0.0, dur.Min, 1e-9)
	require.InDelta(t, 48*3600.0, dur.Max, 1e-9)
}

func TestExact_WeightedStatistics(t *testing.T) {
	outs := []outcome{
		{weight: 0.25, objective: 10, duration: 1},
		{weight: 0.5, objective: -2, duration: 1},
		{weight: 0.25, objective: 4, duration: 3},
	}
	e := exactEstimate(outs, 1, func(o outcome) float64 { return o.objective }, map[string]float64{})
	require.True(t, e.Exact)
	require.Equal(t, 3, e.N)
	require.InDelta(t, 2.5, e.Mean, 1e-12)
	// E[x²] = 25 + 2 + 4 = 31; variance = 31 - 6.25.
	require.InDelta(t, math.Sqrt(24.75), e.StdDev, 1e-12)
	require.Equal(t, -2.0, e.Min)
	require.Equal(t, 10.0, e.Max)
	require.Equal(t, -2.0, e.P50, "cumulative 0.5 reaches the median exactly")
	require.Equal(t, 10.0, e.P95)

	d := exactEstimate(outs, 1, func(o outcome) float64 { return o.duration }, nil)
	require.InDelta(t, 1.5, d.Mean, 1e-12)
	require.Equal(t, 1.0, d.P50)
	require.Equal(t, 3.0, d.P95)

	require.Equal(t, sim.Estimate{Exact: true}, exactEstimate(nil, 0, func(o outcome) float64 { return o.objective }, nil))
}

// TestExact_EmittedBestPolicyRoundTrips emits the best policy of each world,
// re-parses and compiles it, and evaluates it exactly again.
func TestExact_EmittedBestPolicyRoundTrips(t *testing.T) {
	exp := withEvaluator(loadLoanFile(t), EvaluatorExact, 1)
	policies, err := exp.PolicySource().Policies(exp.Model)
	require.NoError(t, err)
	rep, err := Run(context.Background(), exp, staticSource(policies), Options{})
	require.NoError(t, err)

	byID := map[string]sim.Policy{}
	for _, p := range policies {
		byID[p.ID()] = p
	}
	for w, wr := range rep.Worlds {
		best := wr.Policies[0]
		var buf bytes.Buffer
		require.NoError(t, EmitPolicy(byID[best.PolicyID], &buf))
		doc := buf.String()
		t.Logf("best policy for %s:\n%s", wr.World, doc)
		require.True(t, strings.HasPrefix(doc, "# "+best.Description+"\n"), doc)
		require.Contains(t, doc, "apiVersion: policy/v1\n")

		def, err := model.ParsePolicy(buf.Bytes(), engine.FormatYAML)
		require.NoError(t, err)
		compiled, errs := model.CompilePolicy(def, exp.Model)
		require.Zero(t, errs.Len(), "%v", errs)
		require.Equal(t, best.PolicyID, compiled.ID())

		obj, _, err := evaluateExact(context.Background(), exp.Model, exp.Worlds[w], compiled, nil)
		require.NoError(t, err)
		require.InDelta(t, best.Objective.Mean, obj.Mean, 1e-9)
	}

	// The emitted documents of every enumerated policy re-compile to the
	// same choices in both worlds.
	for _, p := range policies {
		var buf bytes.Buffer
		require.NoError(t, EmitPolicy(p, &buf))
		def, err := model.ParsePolicy(buf.Bytes(), engine.FormatYAML)
		require.NoError(t, err, buf.String())
		compiled, errs := model.CompilePolicy(def, exp.Model)
		require.Zero(t, errs.Len(), "%v", errs)
		for _, world := range exp.Worlds {
			a, _, err := evaluateExact(context.Background(), exp.Model, world, p, nil)
			require.NoError(t, err)
			b, _, err := evaluateExact(context.Background(), exp.Model, world, compiled, nil)
			require.NoError(t, err)
			require.Equal(t, a.Mean, b.Mean, p.Describe())
		}
	}

	files := loanExperiment(t, loanDir, 1)
	recession, err := files.FilesSource().Policies(files.Model)
	require.NoError(t, err)
	require.ErrorContains(t, EmitPolicy(recession[0], &bytes.Buffer{}), "EmitPolicy supports policies enumerated by ExhaustiveSource")
}

// staticSource is a PolicySource returning a fixed list of policies.
type staticSource []sim.Policy

func (s staticSource) Policies(*model.CompiledModel) ([]sim.Policy, error) { return s, nil }

// TestExact_Both reports exact and Monte Carlo estimates side by side.
func TestExact_Both(t *testing.T) {
	runs := 4000
	if raceEnabled {
		runs = 1000
	}
	exp := withEvaluator(loanExperiment(t, loanDir, 1, "recession"), EvaluatorBoth, runs)
	src := FilesSource{Files: []string{filepath.Join(loanDir, recessionFile)}}
	var last [2]int
	rep, err := Run(context.Background(), exp, src, Options{Workers: 3, Progress: func(done, total int) { last = [2]int{done, total} }})
	require.NoError(t, err)
	require.Equal(t, [2]int{1 + runs, 1 + runs}, last)
	require.Equal(t, EvaluatorBoth, rep.Evaluator)
	require.Equal(t, runs, rep.Runs)

	r := rep.Worlds[0].Policies[0]
	require.True(t, r.Objective.Exact)
	require.InDelta(t, recessionValue, r.Objective.Mean, 1e-9)
	require.NotNil(t, r.MonteCarlo)
	mc := r.MonteCarlo.Objective
	require.False(t, mc.Exact)
	require.Equal(t, runs, mc.N)
	se := mc.StdDev / math.Sqrt(float64(mc.N))
	require.InDelta(t, r.Objective.Mean, mc.Mean, 4*se)

	// The Monte Carlo half equals a plain Monte Carlo run, whose JSON has no
	// monteCarlo block.
	mcOnly, err := Run(context.Background(), withEvaluator(exp, EvaluatorMonteCarlo, runs), src, Options{})
	require.NoError(t, err)
	require.Equal(t, mcOnly.Worlds[0].Policies[0].Objective, mc)
	require.Equal(t, mcOnly.Worlds[0].Policies[0].Duration, r.MonteCarlo.Duration)
	require.NotContains(t, marshal(t, mcOnly), "monteCarlo")
	require.Contains(t, marshal(t, rep), `"monteCarlo":{"objective":`)
}

// TestExact_RejectsContinuousModel checks that exact evaluation and
// exhaustive search reject a model with continuous distributions and list
// every reason.
func TestExact_RejectsContinuousModel(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "graph.yaml", readLoan(t, "graph.yaml"))
	s := readLoan(t, "sim.yaml")
	s = strings.Replace(s, "  bad: {bernoulli: {p: \"world.p_bad\"}}\n",
		"  bad: {bernoulli: {p: \"world.p_bad\"}}\n  income: {normal: {mean: 50000, sd: 10000}}\n", 1)
	s = strings.Replace(s, "    cost: 20\n", "    cost: 20\n    duration: {lognormal: {mu: 1, sigma: 0.5}}\n", 1)
	writeFile(t, dir, "sim.yaml", s)
	exp := withEvaluator(loanExperiment(t, dir, 1), EvaluatorExact, 1)

	check := func(err error) {
		t.Helper()
		var ve *engine.ValidationErrors
		require.ErrorAs(t, err, &ve)
		require.Equal(t, []engine.ValidationIssue{
			{Path: "$.model:$.behaviors.ai_score.duration.lognormal", Code: sim.CodeModelNotDiscrete, Message: "lognormal distribution is continuous; the exact evaluator requires a discrete model"},
			{Path: "$.model:$.latents.income.normal", Code: sim.CodeModelNotDiscrete, Message: "normal distribution is continuous; the exact evaluator requires a discrete model"},
		}, ve.Issues())
	}
	_, err := Run(context.Background(), exp, FilesSource{Files: []string{filepath.Join(loanDir, recessionFile)}}, Options{})
	check(err)
	_, err = Run(context.Background(), withEvaluator(exp, EvaluatorBoth, 10), FilesSource{Files: []string{filepath.Join(loanDir, recessionFile)}}, Options{})
	check(err)

	_, err = ExhaustiveSource{}.Policies(exp.Model)
	var ve *engine.ValidationErrors
	require.ErrorAs(t, err, &ve)
	require.Len(t, ve.Issues(), 2)
	require.Contains(t, ve.Error(), "latents.income.normal")
	require.Contains(t, ve.Error(), "continuous; exhaustive policy search requires a discrete model")

	// Monte Carlo still runs the continuous model.
	_, err = Run(context.Background(), withEvaluator(exp, EvaluatorMonteCarlo, 10), FilesSource{Files: []string{filepath.Join(loanDir, recessionFile)}}, Options{})
	require.NoError(t, err)
}

func TestExact_Cancellation(t *testing.T) {
	exp := withEvaluator(loadLoanFile(t), EvaluatorExact, 1)
	policies, err := exp.PolicySource().Policies(exp.Model)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runExact(ctx, exp, policies, newEvalSpec(exp), nil, 4, nil)
	require.ErrorIs(t, err, context.Canceled)
}
