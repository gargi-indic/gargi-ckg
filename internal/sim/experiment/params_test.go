package experiment

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

// thresholdDir holds the synthetic threshold model: accepting a uniform
// score s pays 100*s - 40, so accepting when s >= theta has
// EV(theta) = 50(1 - theta^2) - 40(1 - theta), maximised at theta = 0.4.
const thresholdDir = "testdata/threshold"

// thresholdEV is the analytic expected payoff of accepting when the score
// is at least theta.
func thresholdEV(theta float64) float64 {
	theta = min(max(theta, 0), 1)
	return 50*(1-theta*theta) - 40*(1-theta)
}

func thresholdExperiment(t testing.TB) *CompiledExperiment {
	t.Helper()
	exp, err := LoadFile(filepath.Join(thresholdDir, "experiment.yaml"), registry(t))
	require.NoError(t, err)
	return exp
}

// thresholdPolicy compiles a policy over the threshold model with the given
// params block (indented by two spaces) and rule when.
func thresholdPolicy(t testing.TB, m *model.CompiledModel, params, when string) model.Parameterized {
	t.Helper()
	doc := "apiVersion: policy/v1\nid: threshold\nversion: t\nparams:\n" + params +
		"decisions:\n  act:\n    rules:\n      - when: \"" + when + "\"\n        choose: accept\n      - else: reject\n"
	def, err := model.ParsePolicy([]byte(doc), engine.FormatYAML)
	require.NoError(t, err)
	p, errs := model.CompilePolicy(def, m)
	require.Nil(t, errs)
	return p.(model.Parameterized)
}

// paramValues returns the bound value of name for every policy.
func paramValues(t *testing.T, policies []sim.Policy, name string) []any {
	t.Helper()
	out := make([]any, len(policies))
	for i, p := range policies {
		out[i] = p.(model.Parameterized).Params()[name]
	}
	return out
}

func policyIDs(policies []sim.Policy) []string {
	ids := make([]string, len(policies))
	for i, p := range policies {
		ids[i] = p.ID()
	}
	return ids
}

func TestCompile_GridPolicy(t *testing.T) {
	exp := thresholdExperiment(t)
	require.NotNil(t, exp.Policy)
	require.Equal(t, "threshold@theta[theta=0]", exp.Policy.ID())
	src, ok := exp.PolicySource().(GridSource)
	require.True(t, ok)
	require.Equal(t, exp.Policy, src.Policy)
	require.Equal(t, 0, src.MaxPolicies)

	// Policy files are resolved against the experiment and their issues are
	// prefixed with $.policies.policy.
	reg := registry(t)
	dir := t.TempDir()
	for _, f := range []string{"graph.yaml", "sim.yaml"} {
		data, err := os.ReadFile(filepath.Join(thresholdDir, f))
		require.NoError(t, err)
		writeFile(t, dir, f, string(data))
	}
	compile := func(policy string) *engine.ValidationErrors {
		def := &model.Experiment{
			Model: "./sim.yaml", Policies: model.Policies{Search: "grid", Policy: "theta.yaml"},
			Evaluator: "monte_carlo", Runs: 1,
		}
		if policy != "" {
			writeFile(t, dir, "theta.yaml", policy)
		} else {
			def.Policies.Policy = "missing.yaml"
		}
		_, errs := Compile(def, dir, reg)
		return errs
	}
	errs := compile("")
	require.Len(t, errs.Issues(), 1)
	require.Equal(t, "$.policies.policy", errs.Issues()[0].Path)
	require.Equal(t, engine.CodeUnresolvedReference, errs.Issues()[0].Code)

	base := "apiVersion: policy/v1\nid: threshold\nversion: t\nparams:\n  theta: {min: 0, max: 1, step: 0.1}\n" +
		"decisions:\n  act:\n    rules:\n      - when: \"signal.output.score >= params.theta\"\n        choose: accept\n      - else: reject\n"
	errs = compile(strings.Replace(base, "params.theta", "params.unknown", 1))
	require.Equal(t, []engine.ValidationIssue{{
		Path: "$.policies.policy:$.decisions.act.rules[0].when", Code: sim.CodePolicyUnknownParam, Message: `unknown param "unknown"`,
	}}, errs.Issues())
	errs = compile(strings.Replace(base, "max: 1", "max: -1", 1))
	require.Equal(t, []engine.ValidationIssue{{
		Path: "$.policies.policy:$.params.theta.max", Code: engine.CodeInvalidValue, Message: "max -1 must be >= min 0",
	}}, errs.Issues())
}

func TestGridSource_Expansion(t *testing.T) {
	m := thresholdExperiment(t).Model
	const when = "signal.output.score >= params.theta"

	policies, err := GridSource{Policy: thresholdPolicy(t, m, "  theta: {min: 0, max: 1, step: 0.1}\n", when)}.Policies(m)
	require.NoError(t, err)
	require.Equal(t, []any{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}, paramValues(t, policies, "theta"))
	require.Equal(t, "threshold@t[theta=0.3]", policies[3].ID())
	require.Equal(t, "policy threshold version t params theta=0.3", policies[3].Describe())

	// (max-min)/step = 2.9999999999999996 still includes max; a max off the
	// grid is never exceeded.
	policies, err = GridSource{Policy: thresholdPolicy(t, m, "  theta: {min: 0, max: 0.3, step: 0.1}\n", when)}.Policies(m)
	require.NoError(t, err)
	require.Equal(t, []any{0.0, 0.1, 0.2, 0.3}, paramValues(t, policies, "theta"))
	policies, err = GridSource{Policy: thresholdPolicy(t, m, "  theta: {min: 0.1, max: 0.45, step: 0.1}\n", when)}.Policies(m)
	require.NoError(t, err)
	require.Equal(t, []any{0.1, 0.2, 0.3, 0.4}, paramValues(t, policies, "theta"))
	// A max within the float tolerance below a grid point caps that point.
	policies, err = GridSource{Policy: thresholdPolicy(t, m, "  theta: {min: 0, max: 0.29999999999, step: 0.1}\n", when)}.Policies(m)
	require.NoError(t, err)
	require.Equal(t, []any{0.0, 0.1, 0.2, 0.29999999999}, paramValues(t, policies, "theta"))

	// Params in name order, the last name varying fastest; values lists in
	// document order.
	two := thresholdPolicy(t, m, "  b: {values: [y, x]}\n  a: {min: 0, max: 1, step: 0.5}\n", "signal.output.score >= params.a")
	policies, err = GridSource{Policy: two}.Policies(m)
	require.NoError(t, err)
	require.Equal(t, []string{
		"threshold@t[a=0,b=y]", "threshold@t[a=0,b=x]",
		"threshold@t[a=0.5,b=y]", "threshold@t[a=0.5,b=x]",
		"threshold@t[a=1,b=y]", "threshold@t[a=1,b=x]",
	}, policyIDs(policies))
}

func TestGridSource_Errors(t *testing.T) {
	m := thresholdExperiment(t).Model
	const when = "signal.output.score >= params.theta"
	tpl := thresholdPolicy(t, m, "  theta: {min: 0, max: 1, step: 0.1}\n", when)

	_, err := GridSource{Policy: tpl, MaxPolicies: 10}.Policies(m)
	var ve *engine.ValidationErrors
	require.ErrorAs(t, err, &ve)
	require.Equal(t, []engine.ValidationIssue{{
		Path: "$.policies.search", Code: sim.CodeTooManyPolicies, Message: "grid search has 11 candidates, more than max_policies (10)",
	}}, ve.Issues())
	policies, err := GridSource{Policy: tpl, MaxPolicies: 11}.Policies(m)
	require.NoError(t, err)
	require.Len(t, policies, 11)

	// The default cap applies when max_policies is unset, before any
	// candidate is built.
	huge := thresholdPolicy(t, m, "  theta: {min: 0, max: 1, step: 0.00001}\n", when)
	_, err = GridSource{Policy: huge}.Policies(m)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, sim.CodeTooManyPolicies, ve.Issues()[0].Code)

	// The experiment's max_policies reaches the grid source.
	exp := thresholdExperiment(t)
	def := *exp.Def
	def.MaxPolicies = 20
	exp.Def = &def
	_, err = exp.PolicySource().Policies(exp.Model)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, "grid search has 21 candidates, more than max_policies (20)", ve.Issues()[0].Message)

	// Grid search needs a step for every range param.
	noStep := thresholdPolicy(t, m, "  theta: {min: 0, max: 1}\n  zeta: {min: 0, max: 1}\n  mode: {values: [a]}\n", when)
	_, err = GridSource{Policy: noStep}.Policies(m)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, []engine.ValidationIssue{
		{Path: "$.policies.policy:$.params.theta.step", Code: engine.CodeInvalidValue, Message: "grid search requires step"},
		{Path: "$.policies.policy:$.params.zeta.step", Code: engine.CodeInvalidValue, Message: "grid search requires step"},
	}, ve.Issues())

	_, err = GridSource{}.Policies(m)
	require.Error(t, err)
}

func TestRandomSource(t *testing.T) {
	m := thresholdExperiment(t).Model
	tpl := thresholdPolicy(t, m, "  a: {min: 0, max: 1}\n  b: {min: 0, max: 1, step: 0.25}\n  c: {values: [x, y, z]}\n",
		"signal.output.score >= params.a")

	draw := func(seed uint64) []sim.Policy {
		policies, err := RandomSource{Policy: tpl, Samples: 30, Seed: seed}.Policies(m)
		require.NoError(t, err)
		return policies
	}
	first := draw(5)
	require.Len(t, first, 30)
	require.Equal(t, policyIDs(first), policyIDs(draw(5)), "same seed, same candidates in the same order")
	require.NotEqual(t, policyIDs(first), policyIDs(draw(6)))

	seenB, seenC := map[float64]bool{}, map[string]bool{}
	for _, p := range first {
		params := p.(model.Parameterized).Params()
		a := params["a"].(float64)
		require.True(t, a >= 0 && a <= 1, a)
		require.Equal(t, roundParam(a), a, "continuous draws are rounded like the grid")
		b := params["b"].(float64)
		require.Contains(t, []float64{0, 0.25, 0.5, 0.75, 1}, b)
		seenB[b] = true
		seenC[params["c"].(string)] = true
	}
	require.Len(t, seenB, 5)
	require.Len(t, seenC, 3)

	// Duplicate candidates are dropped, keeping the first occurrence.
	values := thresholdPolicy(t, m, "  a: {values: [0.2, 0.4]}\n", "signal.output.score >= params.a")
	policies, err := RandomSource{Policy: values, Samples: 50, Seed: 1}.Policies(m)
	require.NoError(t, err)
	require.Len(t, policies, 2)
	again, err := RandomSource{Policy: values, Samples: 50, Seed: 1}.Policies(m)
	require.NoError(t, err)
	require.Equal(t, policyIDs(policies), policyIDs(again))

	_, err = RandomSource{Policy: values}.Policies(m)
	require.Error(t, err)
	_, err = RandomSource{Samples: 1}.Policies(m)
	require.Error(t, err)
}

// TestRandomSource_Experiment runs random search from an experiment
// document; its seed defaults to the experiment seed.
func TestRandomSource_Experiment(t *testing.T) {
	exp := thresholdExperiment(t)
	def := *exp.Def
	def.Policies = model.Policies{Search: "random", Policy: def.Policies.Policy, Samples: 6}
	def.Runs = 200
	exp.Def = &def

	src, ok := exp.PolicySource().(RandomSource)
	require.True(t, ok)
	require.Equal(t, RandomSource{Policy: exp.Policy, Samples: 6, Seed: 7}, src)
	seed := uint64(99)
	def.Policies.Seed = &seed
	require.Equal(t, uint64(99), exp.RandomSource().Seed)

	a, err := Run(context.Background(), exp, exp.PolicySource(), Options{All: true})
	require.NoError(t, err)
	b, err := Run(context.Background(), exp, exp.PolicySource(), Options{All: true, Workers: 3})
	require.NoError(t, err)
	require.Equal(t, marshal(t, a), marshal(t, b))
	require.NotEmpty(t, a.Worlds[0].All)
	for _, r := range a.Worlds[0].All {
		require.Contains(t, r.Params, "theta")
	}
}

// TestGrid_ThresholdAcceptance is the acceptance test of grid search: over
// theta in {0, 0.05, ..., 1} the best candidate is within one step of the
// analytic optimum 0.4 and its mean is within 3 standard errors of the
// analytic EV.
func TestGrid_ThresholdAcceptance(t *testing.T) {
	runs := 20_000
	if raceEnabled {
		runs = 4_000
	}
	exp := withEvaluator(thresholdExperiment(t), EvaluatorMonteCarlo, runs)
	rep, err := Run(context.Background(), exp, exp.PolicySource(), Options{All: true})
	require.NoError(t, err)
	require.Len(t, rep.Worlds, 1)
	all := rep.Worlds[0].All
	require.Len(t, all, 21)
	for _, r := range all {
		theta := r.Params["theta"].(float64)
		se := r.Objective.StdDev / math.Sqrt(float64(r.Objective.N))
		require.InDelta(t, thresholdEV(theta), r.Objective.Mean, 4*se+1e-12, "theta %v", theta)
	}

	best := rep.Worlds[0].Policies[0]
	theta := best.Params["theta"].(float64)
	require.InDelta(t, 0.4, theta, 0.05+1e-12, "best %s", best.PolicyID)
	se := best.Objective.StdDev / math.Sqrt(float64(best.Objective.N))
	require.InDelta(t, thresholdEV(theta), best.Objective.Mean, 3*se)
	require.Equal(t, "threshold@theta[theta="+strconv.FormatFloat(theta, 'g', -1, 64)+"]", best.PolicyID)
	t.Logf("best theta %v: mean %.4f, analytic EV %.4f, SE %.4f (%d runs)", theta, best.Objective.Mean, thresholdEV(theta), se, runs)
}

// TestGrid_CommonRandomNumbers checks that candidates whose params make the
// same decision on every run get bit-identical estimates: every candidate
// replays replication r with the same seed.
func TestGrid_CommonRandomNumbers(t *testing.T) {
	exp := withEvaluator(thresholdExperiment(t), EvaluatorMonteCarlo, 2_500)
	tpl := thresholdPolicy(t, exp.Model, "  theta: {min: -1, max: 2, step: 0.5}\n", "signal.output.score >= params.theta")
	var src staticSource
	for _, theta := range []float64{-0.5, 0, 1.5, 2} {
		p, err := tpl.WithParams(map[string]any{"theta": theta})
		require.NoError(t, err)
		src = append(src, p)
	}
	rep, err := Run(context.Background(), exp, src, Options{All: true, Workers: 4})
	require.NoError(t, err)
	byTheta := map[float64]PolicyResult{}
	for _, r := range rep.Worlds[0].All {
		byTheta[r.Params["theta"].(float64)] = r
	}
	// theta -0.5 and 0 accept every score in [0, 1): a random payoff.
	acceptAll := byTheta[-0.5].Objective
	require.Positive(t, acceptAll.StdDev)
	require.InDelta(t, 10, acceptAll.Mean, 4*acceptAll.StdDev/math.Sqrt(float64(acceptAll.N)))
	require.Equal(t, acceptAll, byTheta[0].Objective)
	require.Equal(t, byTheta[-0.5].Duration, byTheta[0].Duration)
	// theta 1.5 and 2 reject every score.
	require.Equal(t, byTheta[1.5].Objective, byTheta[2].Objective)
}

// TestReport_Params checks that grid candidates report their params, that
// policies without params do not, and that the report does not depend on
// the worker count.
func TestReport_Params(t *testing.T) {
	// Two chunks, so the merge order is exercised.
	exp := withEvaluator(thresholdExperiment(t), EvaluatorMonteCarlo, ChunkSize+200)
	one, err := Run(context.Background(), exp, exp.PolicySource(), Options{Workers: 1, All: true})
	require.NoError(t, err)
	eight, err := Run(context.Background(), exp, exp.PolicySource(), Options{Workers: 8, All: true})
	require.NoError(t, err)
	require.Equal(t, marshal(t, one), marshal(t, eight))
	require.Contains(t, marshal(t, one), `"description":"policy threshold version theta params theta=0.4","params":{"theta":0.4}`)
	for _, r := range one.Worlds[0].All {
		require.Len(t, r.Params, 1)
	}

	loan := loanExperiment(t, loanDir, 50, "recession")
	rep, err := Run(context.Background(), loan, loan.FilesSource(), Options{})
	require.NoError(t, err)
	require.Nil(t, rep.Worlds[0].Policies[0].Params)
	require.NotContains(t, marshal(t, rep), `"params"`)
}
