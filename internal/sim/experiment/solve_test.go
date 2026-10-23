package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

// exhaustiveBest evaluates every exhaustive policy exactly and returns, per
// world, the sorted results (best first).
func exhaustiveBest(t *testing.T, exp *CompiledExperiment) ([]WorldReport, []sim.Policy) {
	t.Helper()
	ex := withEvaluator(exp, EvaluatorExact, 1)
	policies, err := ex.ExhaustiveSource().Policies(ex.Model)
	require.NoError(t, err)
	rep, err := Run(context.Background(), ex, staticSource(policies), Options{All: true})
	require.NoError(t, err)
	return rep.Worlds, policies
}

// TestExactSearch_LoanMatchesExhaustive solves the loan model in both worlds
// and compares the solution with exhaustive search scored by the exact
// evaluator: same best policy (id and table), same EV to 1e-9.
func TestExactSearch_LoanMatchesExhaustive(t *testing.T) {
	exp := withEvaluator(loadLoanFile(t), EvaluatorExact, 1)
	exhaustive, _ := exhaustiveBest(t, exp)

	sol, err := exp.ExactSource().Solve(context.Background(), exp.Model, exp.Worlds)
	require.NoError(t, err)
	require.Len(t, sol.Worlds, 2)
	want := map[sim.WorldID]struct {
		desc string
		ev   float64
		skip float64 // best EV among screen=skip policies (prototype)
	}{
		"base":      {bestBaseDesc, 460.0, 301.99999999999994},
		"recession": {bestRecessionDesc, 202.525, 72.24999999999997},
	}
	for i, ws := range sol.Worlds {
		ex := exhaustive[i]
		require.Equal(t, ex.World, ws.World)
		best := ex.All[0]
		// The exhaustive optimum is unique, so the comparison does not depend
		// on tie-breaking.
		require.Less(t, ex.All[1].Objective.Mean, best.Objective.Mean-1e-6, "exhaustive optimum of %s is not unique", ws.World)
		require.Equal(t, best.PolicyID, ws.PolicyID, ws.World)
		require.Equal(t, best.Description, ws.Description, ws.World)
		require.Equal(t, best.PolicyID, ws.Policy().ID())
		require.InDelta(t, best.Objective.Mean, ws.EV, 1e-9, ws.World)
		require.InDelta(t, want[ws.World].ev, ws.EV, 1e-9, ws.World)
		require.Equal(t, want[ws.World].desc, ws.Description)
		require.Equal(t, SolverBackwardInduction, ws.Solver)
		require.Equal(t, big.NewInt(42), ws.ReducedPolicies(), "reduced policies of %s", ws.World)
		t.Logf("%s: %s EV %.4f from %d histories (42 reduced policies)", ws.World, ws.Description, ws.EV, ws.Histories)

		// The root decision's action values are the best EV with and
		// without the score (the prototype's numbers).
		screen := ws.InformationSets[0]
		require.Equal(t, "screen", screen.InfoSet)
		require.Nil(t, screen.Parent)
		require.Equal(t, map[string]any{}, screen.Observed)
		require.Equal(t, "score", screen.Choice)
		require.InDelta(t, 1.0, screen.ChanceProbability, 1e-12)
		require.InDelta(t, 1.0, screen.ReachProbability, 1e-12)
		require.Equal(t, []string{"score", "skip"}, []string{screen.Actions[0].Option, screen.Actions[1].Option})
		require.InDelta(t, want[ws.World].ev, screen.Actions[0].Value, 1e-9)
		require.InDelta(t, want[ws.World].skip, screen.Actions[1].Value, 1e-9)
	}

	// Distinct optima of the two worlds become two policies.
	policies, err := sol.Policies(exp.Model)
	require.NoError(t, err)
	require.Len(t, policies, 2)
	require.Equal(t, sol.Worlds[0].PolicyID, policies[0].ID())
	require.Equal(t, sol.Worlds[1].PolicyID, policies[1].ID())
}

// TestExactSearch_LoanActionValues checks the information sets of the base
// world against hand-computed values.
func TestExactSearch_LoanActionValues(t *testing.T) {
	exp := loadLoanFile(t)
	sol, err := exp.ExactSource().Solve(context.Background(), exp.Model, exp.Worlds)
	require.NoError(t, err)
	base, recession := sol.Worlds[0], sol.Worlds[1]

	names := func(ws WorldSolution) []string {
		var out []string
		for _, is := range ws.InformationSets {
			out = append(out, is.InfoSet)
		}
		return out
	}
	wantSets := []string{
		"screen",
		"route[score,high]", "route[score,low]", "route[skip,nil]",
		"final[score,high,fail]", "final[score,high,pass]",
		"final[score,low,fail]", "final[score,low,pass]",
		"final[skip,nil,fail]", "final[skip,nil,pass]",
	}
	require.Equal(t, wantSets, names(base))
	require.Equal(t, wantSets, names(recession))

	byName := map[string]InformationSetValue{}
	for _, is := range base.InformationSets {
		byName[is.InfoSet] = is
	}
	// Base: P(bad) = 0.2; P(low) = 0.8·0.9 + 0.2·0.3 = 0.78.
	low := byName["route[score,low]"]
	require.Equal(t, "route", low.Decision)
	require.Equal(t, map[string]any{"screen.output.choice": "score", "ai_score.output.band": "low"}, low.Observed)
	require.Equal(t, &InfoSetChoice{InfoSet: "screen", Option: "score"}, low.Parent)
	require.InDelta(t, 0.78, low.ChanceProbability, 1e-12)
	require.InDelta(t, 0.78, low.ReachProbability, 1e-12)
	require.Equal(t, "approve", low.Choice)
	// P(bad | low) = 0.06/0.78; approve pays 1000 if good, -4000 if bad,
	// after the $20 score.
	pBad := 0.06 / 0.78
	require.InDelta(t, -20+(1-pBad)*1000-pBad*4000, low.Actions[0].Value, 1e-9)
	require.Equal(t, "reject", low.Actions[1].Option)
	require.InDelta(t, -20.0, low.Actions[1].Value, 1e-9)

	skip := byName["route[skip,nil]"]
	require.Equal(t, map[string]any{"screen.output.choice": "skip", "ai_score.output.band": nil}, skip.Observed)
	require.InDelta(t, 1.0, skip.ChanceProbability, 1e-12)
	require.Zero(t, skip.ReachProbability, "off the optimal path")
	require.Equal(t, "review", skip.Choice)
	require.InDelta(t, 301.99999999999994, skip.Actions[2].Value, 1e-9)

	for _, name := range wantSets[4:] {
		require.Zero(t, byName[name].ReachProbability, "base never reviews: %s", name)
		require.Equal(t, "review", byName[name].Parent.Option)
	}
	// Recession reviews low scores and reaches final[score,low,*].
	for _, is := range recession.InformationSets {
		reached := is.ReachProbability > 0
		wantReached := is.InfoSet == "screen" || strings.HasPrefix(is.InfoSet, "route[score,") || strings.HasPrefix(is.InfoSet, "final[score,low,")
		require.Equal(t, wantReached, reached, is.InfoSet)
		if reached {
			require.InDelta(t, is.ChanceProbability, is.ReachProbability, 1e-12, is.InfoSet)
		}
	}
}

// TestExactSearch_RunReport runs search: exact through Run with every
// evaluator: the solved EV agrees with the exact evaluator, the report JSON
// carries the action values, and the report does not depend on the worker
// count.
func TestExactSearch_RunReport(t *testing.T) {
	exp := withEvaluator(loanWithApproval(t, EvaluatorExact, 1), EvaluatorExact, 1)
	def := *exp.Def
	def.Policies = model.Policies{Search: "exact"}
	exp.Def = &def
	src, ok := exp.PolicySource().(ExactSource)
	require.True(t, ok)

	rep, err := Run(context.Background(), exp, src, Options{Workers: 8, All: true})
	require.NoError(t, err)
	require.Len(t, rep.Worlds, 2)
	for _, wr := range rep.Worlds {
		require.NotNil(t, wr.Solution)
		require.Len(t, wr.All, 2, "one policy per distinct world optimum")
		require.Equal(t, wr.Solution.PolicyID, wr.Policies[0].PolicyID)
		require.True(t, wr.Policies[0].Objective.Exact)
		require.InDelta(t, wr.Policies[0].Objective.Mean, wr.Solution.EV, 1e-9, "solver EV vs exact evaluator in %s", wr.World)
	}
	require.InDelta(t, 460.0, rep.Worlds[0].Solution.EV, 1e-9)
	require.InDelta(t, 202.525, rep.Worlds[1].Solution.EV, 1e-9)
	// Cross-world values: the recession optimum in the base world and vice
	// versa (the regret numbers of the golden test).
	require.InDelta(t, 460.0-142.2, rep.Worlds[0].Policies[1].Objective.Mean, 0.1)
	require.InDelta(t, 202.525-267.5, rep.Worlds[1].Policies[1].Objective.Mean, 0.1)

	one, err := Run(context.Background(), exp, src, Options{Workers: 1, All: true})
	require.NoError(t, err)
	js := marshal(t, rep)
	require.Equal(t, marshal(t, one), js)
	again, err := Run(context.Background(), exp, src, Options{Workers: 3, All: true})
	require.NoError(t, err)
	require.Equal(t, js, marshal(t, again))

	require.Contains(t, js, `"solution":{"solver":"backward_induction","policyId":"`+rep.Worlds[0].Solution.PolicyID+`","description":"`+bestBaseDesc+`","ev":460,"histories":39,"informationSets":[{"infoSet":"screen","decision":"screen","observed":{},"chanceProbability":1,"reachProbability":1,"choice":"score","actions":[{"option":"score","value":460},{"option":"skip","value":302}]}`)
	require.Contains(t, js, `{"infoSet":"route[score,low]","decision":"route","observed":{"ai_score.output.band":"low","screen.output.choice":"score"},"parent":{"infoSet":"screen","option":"score"},"chanceProbability":0.78`)
	require.Contains(t, js, `"actions":[{"option":"approve","value":`)

	// The JSON decodes into the documented shape.
	var decoded struct {
		Worlds []struct {
			World    string `json:"world"`
			Solution struct {
				Solver          string  `json:"solver"`
				PolicyID        string  `json:"policyId"`
				EV              float64 `json:"ev"`
				Histories       int     `json:"histories"`
				InformationSets []struct {
					InfoSet           string         `json:"infoSet"`
					Decision          string         `json:"decision"`
					Observed          map[string]any `json:"observed"`
					Parent            *InfoSetChoice `json:"parent"`
					ChanceProbability float64        `json:"chanceProbability"`
					ReachProbability  float64        `json:"reachProbability"`
					Choice            string         `json:"choice"`
					Actions           []ActionValue  `json:"actions"`
				} `json:"informationSets"`
			} `json:"solution"`
		} `json:"worlds"`
	}
	dec := json.NewDecoder(strings.NewReader(js))
	require.NoError(t, dec.Decode(&decoded))
	require.Len(t, decoded.Worlds[1].Solution.InformationSets, 10)
	require.Equal(t, "recession", decoded.Worlds[1].World)
	require.Positive(t, decoded.Worlds[1].Solution.Histories)

	// Monte Carlo and both still score the solved policies; the solution is
	// the same.
	runs := 2000
	if raceEnabled {
		runs = 200
	}
	for _, ev := range []string{EvaluatorMonteCarlo, EvaluatorBoth} {
		r, err := Run(context.Background(), withEvaluator(exp, ev, runs), src, Options{Workers: 4})
		require.NoError(t, err, ev)
		for w, wr := range r.Worlds {
			require.NotNil(t, wr.Solution, ev)
			require.Equal(t, rep.Worlds[w].Solution.PolicyID, wr.Solution.PolicyID, ev)
			require.Equal(t, runs, r.Runs)
			if ev == EvaluatorBoth {
				require.Equal(t, wr.Solution.PolicyID, wr.Policies[0].PolicyID)
				require.InDelta(t, wr.Solution.EV, wr.Policies[0].Objective.Mean, 1e-9)
			}
		}
	}

	// Other sources report no solution.
	exh, err := Run(context.Background(), withEvaluator(loadLoanFile(t), EvaluatorExact, 1), loadLoanFile(t).ExhaustiveSource(), Options{})
	require.NoError(t, err)
	require.NotContains(t, marshal(t, exh), `"solution"`)
}

// TestExactSearch_SolvedPolicyRoundTrips emits each world's solved policy,
// re-parses and compiles it, and evaluates it exactly.
func TestExactSearch_SolvedPolicyRoundTrips(t *testing.T) {
	exp := loadLoanFile(t)
	sol, err := exp.ExactSource().Solve(context.Background(), exp.Model, exp.Worlds)
	require.NoError(t, err)
	for i, ws := range sol.Worlds {
		var buf bytes.Buffer
		require.NoError(t, EmitPolicy(ws.Policy(), &buf))
		require.True(t, strings.HasPrefix(buf.String(), "# "+ws.Description+"\n"))
		def, err := model.ParsePolicy(buf.Bytes(), engine.FormatYAML)
		require.NoError(t, err)
		compiled, errs := model.CompilePolicy(def, exp.Model)
		require.Zero(t, errs.Len(), "%v", errs)
		require.Equal(t, ws.PolicyID, compiled.ID())
		obj, _, err := evaluateExact(context.Background(), exp.Model, exp.Worlds[i], compiled, nil)
		require.NoError(t, err)
		require.InDelta(t, ws.EV, obj.Mean, 1e-9)
	}
}

// TestExactSearch_PoliciesAndSolutionChecks covers ExactSource.Policies and
// the checks of a reused Solution.
func TestExactSearch_PoliciesAndSolutionChecks(t *testing.T) {
	exp := loadLoanFile(t)
	policies, err := ExactSource{}.Policies(exp.Model)
	require.NoError(t, err)
	require.Len(t, policies, 2)
	require.Equal(t, bestBaseDesc, policies[0].Describe())
	require.Equal(t, bestRecessionDesc, policies[1].Describe())

	one, err := ExactSource{Worlds: exp.Worlds[1:]}.Policies(exp.Model)
	require.NoError(t, err)
	require.Len(t, one, 1)
	require.Equal(t, bestRecessionDesc, one[0].Describe())

	sol, err := exp.ExactSource().Solve(context.Background(), exp.Model, exp.Worlds)
	require.NoError(t, err)
	same, err := sol.Solve(context.Background(), exp.Model, exp.Worlds)
	require.NoError(t, err)
	require.Same(t, sol, same)
	_, err = sol.Solve(context.Background(), exp.Model, exp.Worlds[:1])
	require.ErrorContains(t, err, "covers 2 worlds, want 1")
	_, err = sol.Solve(context.Background(), exp.Model, []sim.World{exp.Worlds[1], exp.Worlds[0]})
	require.ErrorContains(t, err, `solution world 0 is "base", want "recession"`)
	other := loadLoanFile(t)
	_, err = sol.Solve(context.Background(), other.Model, exp.Worlds)
	require.ErrorContains(t, err, "different model")
	_, err = sol.Policies(other.Model)
	require.ErrorContains(t, err, "different model")

	// Run accepts a precomputed solution (the CLI solves once).
	rep, err := Run(context.Background(), withEvaluator(exp, EvaluatorExact, 1), sol, Options{})
	require.NoError(t, err)
	require.Same(t, &sol.Worlds[0], rep.Worlds[0].Solution)

	_, err = ExactSource{}.Solve(context.Background(), exp.Model, nil)
	require.ErrorContains(t, err, "no worlds")
	_, err = ExactSource{}.Policies(nil)
	require.Error(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ExactSource{}.Solve(ctx, exp.Model, exp.Worlds)
	require.ErrorIs(t, err, context.Canceled)
}

// TestExactSearch_RejectsContinuousModel checks the discrete-model check.
func TestExactSearch_RejectsContinuousModel(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "graph.yaml", readLoan(t, "graph.yaml"))
	s := readLoan(t, "sim.yaml")
	s = strings.Replace(s, "  bad: {bernoulli: {p: \"world.p_bad\"}}\n",
		"  bad: {bernoulli: {p: \"world.p_bad\"}}\n  income: {normal: {mean: 50000, sd: 10000}}\n", 1)
	s = strings.Replace(s, "    cost: 20\n", "    cost: 20\n    duration: {lognormal: {mu: 1, sigma: 0.5}}\n", 1)
	writeFile(t, dir, "sim.yaml", s)
	exp := withEvaluator(loanExperiment(t, dir, 10), EvaluatorMonteCarlo, 10)

	want := []engine.ValidationIssue{
		{Path: "$.model:$.behaviors.ai_score.duration.lognormal", Code: sim.CodeModelNotDiscrete, Message: "lognormal distribution is continuous; exact policy search requires a discrete model"},
		{Path: "$.model:$.latents.income.normal", Code: sim.CodeModelNotDiscrete, Message: "normal distribution is continuous; exact policy search requires a discrete model"},
	}
	var ve *engine.ValidationErrors
	_, err := ExactSource{}.Policies(exp.Model)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, want, ve.Issues())
	// Even with the Monte Carlo evaluator, solving needs a discrete model.
	_, err = Run(context.Background(), exp, exp.ExactSource(), Options{})
	require.ErrorAs(t, err, &ve)
	require.Equal(t, want, ve.Issues())
}

// TestExactSearch_RejectsImperfectRecall makes loan decisions forget earlier
// choices and observations.
func TestExactSearch_RejectsImperfectRecall(t *testing.T) {
	loanVariant := func(t *testing.T, from, to string) *CompiledExperiment {
		t.Helper()
		dir := t.TempDir()
		g := readLoan(t, "graph.yaml")
		forgetful := strings.Replace(g, from, to, 1)
		require.NotEqual(t, g, forgetful)
		writeFile(t, dir, "graph.yaml", forgetful)
		writeFile(t, dir, "sim.yaml", readLoan(t, "sim.yaml"))
		return withEvaluator(loanExperiment(t, dir, 1), EvaluatorExact, 1)
	}
	const suffix = "; exact policy search requires every decision to observe what earlier decisions observed and chose"

	t.Run("route forgets the screen choice", func(t *testing.T) {
		// route observes nothing: its only information set follows both
		// screen=score and screen=skip.
		exp := loanVariant(t, "observes: [screen.output.choice, ai_score.output.band]}", "observes: []}")
		_, err := Run(context.Background(), exp, exp.ExactSource(), Options{})
		var ve *engine.ValidationErrors
		require.ErrorAs(t, err, &ve)
		require.Equal(t, []engine.ValidationIssue{{
			Path: "$.model:$.graph:$.nodes[2].config.observes", Code: sim.CodeNotPerfectRecall,
			Message: `decision "route" does not have perfect recall: in world "base" its information set route follows screen=score in one history and screen=skip in another (1 information set(s) affected)` + suffix,
		}}, ve.Issues())

		// Exhaustive search and the exact evaluator do not need perfect
		// recall.
		_, err = Run(context.Background(), exp, exp.ExhaustiveSource(), Options{})
		require.NoError(t, err)
	})

	t.Run("final forgets the score", func(t *testing.T) {
		// final observes only the review result: final[fail] and
		// final[pass] follow route=review at three route information sets.
		exp := loanVariant(t, "observes: [screen.output.choice, ai_score.output.band, human_review.output.result]}", "observes: [human_review.output.result]}")
		_, err := ExactSource{}.Policies(exp.Model)
		var ve *engine.ValidationErrors
		require.ErrorAs(t, err, &ve)
		require.Len(t, ve.Issues(), 1)
		is := ve.Issues()[0]
		require.Equal(t, "$.model:$.graph:$.nodes[4].config.observes", is.Path)
		require.Equal(t, sim.CodeNotPerfectRecall, is.Code)
		require.Contains(t, is.Message, `decision "final" does not have perfect recall`)
		require.Contains(t, is.Message, "(2 information set(s) affected)")
		require.Regexp(t, `follows route\[[a-z]+,[a-z]+\]=review in one history and route\[[a-z]+,[a-z]+\]=review in another`, is.Message)
	})

	t.Run("generated model reports every decision, sorted", func(t *testing.T) {
		exp := generatedExperiment(t, genSpec{layers: 3, outcomes: 2, options: 2, seed: 9, forget: true})
		_, err := Run(context.Background(), exp, exp.PolicySource(), Options{})
		var ve *engine.ValidationErrors
		require.ErrorAs(t, err, &ve)
		var got []string
		for _, is := range ve.Issues() {
			require.Equal(t, sim.CodeNotPerfectRecall, is.Code)
			got = append(got, is.Path)
		}
		// d1 (nodes[1]) is the first decision and always has perfect recall.
		require.Equal(t, []string{
			"$.model:$.graph:$.nodes[3].config.observes",
			"$.model:$.graph:$.nodes[5].config.observes",
		}, got)
		require.Contains(t, ve.Issues()[0].Message, `decision "d2"`)
		require.Contains(t, ve.Issues()[1].Message, `decision "d3"`)
	})
}

// genSpec describes a generated sequential decision model: an unobserved
// latent z ("0" or "1"), then layers of a chance node c<i> with outcomes
// "0".."<outcomes-1>" (whose weights depend on z) followed by a decision
// d<i> with options "0".."<options-1>" that observes every earlier chance
// outcome and choice (perfect recall). Every information set therefore holds
// two histories, one per value of z. The objective sums, per layer, a payoff
// of (c<i>, d<i>), of (z, d<i>), of (c<i-1>, d<i>) and of (d<i-1>, d<i>),
// drawn from a seeded generator (all zero when zero is set). With forget,
// each decision observes only its own chance node, which breaks perfect
// recall.
type genSpec struct {
	layers, outcomes, options int
	seed                      uint64
	zero, forget              bool
}

// reducedPolicies returns options^(outcomes + outcomes² + … + outcomes^layers):
// decision d<i> has outcomes^i reachable information sets under any policy.
func (g genSpec) reducedPolicies() *big.Int {
	exp := int64(0)
	pow := int64(1)
	for range g.layers {
		pow *= int64(g.outcomes)
		exp += pow
	}
	return new(big.Int).Exp(big.NewInt(int64(g.options)), big.NewInt(exp), nil)
}

// generatedExperiment writes the model of g into a temporary directory and
// compiles an experiment over it (one world, search: exact, evaluator exact).
//
// Outcomes and options are numeric strings so that the objective can index
// CEL list literals with int(...): payoff tables written as map literals
// would exceed the CEL cost limit.
func generatedExperiment(t *testing.T, g genSpec) *CompiledExperiment {
	t.Helper()
	rng := rand.New(rand.NewPCG(g.seed, 0x9e3779b97f4a7c15))
	val := func() string {
		if g.zero {
			return "0.0"
		}
		return fmt.Sprintf("%.3f", float64(rng.IntN(20001)-10000)/1000)
	}
	// table renders a CEL list literal [rows][options] of payoffs indexed by
	// the int values of row and choice.
	table := func(rows int, row, choice string) string {
		var b strings.Builder
		b.WriteString("[")
		for r := range rows {
			if r > 0 {
				b.WriteString(", ")
			}
			b.WriteString("[")
			for o := range g.options {
				if o > 0 {
					b.WriteString(", ")
				}
				b.WriteString(val())
			}
			b.WriteString("]")
		}
		fmt.Fprintf(&b, "][int(%s)][int(%s)]", row, choice)
		return b.String()
	}
	quoted := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("'%d'", i)
		}
		return out
	}
	options := strings.Join(quoted(g.options), ", ")

	var graph, behaviors strings.Builder
	graph.WriteString("apiVersion: graph/v1\nid: generated\nnodes:\n")
	var edges, terms, observes []string
	for i := 1; i <= g.layers; i++ {
		c, d := fmt.Sprintf("c%d", i), fmt.Sprintf("d%d", i)
		if g.forget {
			observes = nil
		}
		observes = append(observes, c+".output.v")
		fmt.Fprintf(&graph, "  - id: %s\n    type: http_call\n    config: {url: \"https://chance.example.internal/%s\", method: GET}\n", c, c)
		fmt.Fprintf(&graph, "  - id: %s\n    type: decision\n    config: {options: [%s], observes: [%s]}\n", d, options, strings.Join(observes, ", "))
		observes = append(observes, d+".output.choice")
		if i > 1 {
			edges = append(edges, fmt.Sprintf("  - {from: d%d, to: %s}", i-1, c))
		}
		edges = append(edges, fmt.Sprintf("  - {from: %s, to: %s}", c, d))

		weights := make([]string, g.outcomes)
		for j := range weights {
			weights[j] = fmt.Sprintf("'%d': \"latent.z == '0' ? %d.0 : %d.0\"", j, 1+rng.IntN(9), 1+rng.IntN(9))
		}
		fmt.Fprintf(&behaviors, "  %s:\n    cost: 0\n    output:\n      v: {choice: {%s}}\n", c, strings.Join(weights, ", "))

		choice := "nodes." + d + ".output.choice"
		terms = append(terms,
			table(g.outcomes, "nodes."+c+".output.v", choice),
			table(2, "latent.z", choice))
		if i > 1 {
			terms = append(terms,
				table(g.outcomes, fmt.Sprintf("nodes.c%d.output.v", i-1), choice),
				table(g.options, fmt.Sprintf("nodes.d%d.output.choice", i-1), choice))
		}
	}
	graph.WriteString("edges:\n" + strings.Join(edges, "\n") + "\n")
	sim := fmt.Sprintf("apiVersion: sim/v1\nid: generated\ngraph: ./graph.yaml\nworlds:\n  only: {}\nlatents:\n  z: {choice: {'0': %d, '1': %d}}\nbehaviors:\n%sobjective:\n  name: payoff\n  value: \"%s\"\n  maximize: mean\n",
		1+rng.IntN(9), 1+rng.IntN(9), behaviors.String(), strings.Join(terms, " + "))

	dir := t.TempDir()
	writeFile(t, dir, "graph.yaml", graph.String())
	writeFile(t, dir, "sim.yaml", sim)
	writeFile(t, dir, "experiment.yaml", "apiVersion: experiment/v1\nmodel: ./sim.yaml\npolicies: {search: exact}\nevaluator: exact\nruns: 1\nseed: 1\n")
	exp, err := LoadFile(filepath.Join(dir, "experiment.yaml"), registry(t))
	require.NoError(t, err)
	return exp
}

// TestExactSearch_GeneratedLarge solves a generated model with 3^39 (about
// 4·10^18) reduced policies, far beyond enumeration, in well under 5 s.
func TestExactSearch_GeneratedLarge(t *testing.T) {
	g := genSpec{layers: 3, outcomes: 3, options: 3, seed: 7}
	exp := generatedExperiment(t, g)
	require.Equal(t, "exact", exp.Def.Policies.Search)

	start := time.Now()
	rep, err := Run(context.Background(), exp, exp.PolicySource(), Options{})
	elapsed := time.Since(start)
	require.NoError(t, err)

	ws := rep.Worlds[0].Solution
	require.NotNil(t, ws)
	want := g.reducedPolicies()
	require.Equal(t, want, ws.ReducedPolicies())
	require.Positive(t, ws.ReducedPolicies().Cmp(big.NewInt(1_000_000)))
	require.Equal(t, 2*729, ws.Histories, "2 latent values · (3 outcomes · 3 options)^3 histories")
	require.Len(t, ws.InformationSets, 3+27+243, "d1: 3, d2: 3·3·3, d3: 27·3·3 information sets")
	require.InDelta(t, rep.Worlds[0].Policies[0].Objective.Mean, ws.EV, 1e-9, "solver EV vs exact evaluator")
	t.Logf("generated model: %s reduced policies, %d histories, solved and evaluated in %s (race %v)", ws.ReducedPolicies(), ws.Histories, elapsed, raceEnabled)
	if !raceEnabled {
		require.Less(t, elapsed, 5*time.Second)
	}

	// Every reachable information set is in the table: 3 + 9 + 27.
	reached := 0
	for _, is := range ws.InformationSets {
		if is.ReachProbability > 0 {
			reached++
		}
	}
	require.Equal(t, 3+9+27, reached)
	require.Equal(t, reached, strings.Count(ws.Description, ";")+1)
}

// TestExactSearch_GeneratedMatchesExhaustive compares the solver with
// exhaustive search on small variants of the generator.
func TestExactSearch_GeneratedMatchesExhaustive(t *testing.T) {
	specs := []genSpec{
		{layers: 2, outcomes: 2, options: 2, seed: 1},
		{layers: 2, outcomes: 2, options: 2, seed: 2},
		{layers: 1, outcomes: 4, options: 3, seed: 3},
	}
	if !raceEnabled {
		specs = append(specs, genSpec{layers: 2, outcomes: 2, options: 3, seed: 4})
	}
	for _, g := range specs {
		t.Run(fmt.Sprintf("L%d_m%d_k%d_seed%d", g.layers, g.outcomes, g.options, g.seed), func(t *testing.T) {
			exp := generatedExperiment(t, g)
			worlds, policies := exhaustiveBest(t, exp)
			require.Len(t, policies, int(g.reducedPolicies().Int64()))
			require.Less(t, len(policies), DefaultMaxPolicies)

			sol, err := exp.ExactSource().Solve(context.Background(), exp.Model, exp.Worlds)
			require.NoError(t, err)
			ws := sol.Worlds[0]
			require.Equal(t, g.reducedPolicies(), ws.ReducedPolicies())
			best := worlds[0].All[0]
			require.InDelta(t, best.Objective.Mean, ws.EV, 1e-9)
			// The seeds are chosen so that the optimum is unique; then the
			// policies must be equal, not only their EVs.
			require.Less(t, worlds[0].All[1].Objective.Mean, best.Objective.Mean-1e-6, "optimum not unique")
			require.Equal(t, best.PolicyID, ws.PolicyID)
			require.Equal(t, best.Description, ws.Description)
			t.Logf("%d policies, best %.3f: %s", len(policies), ws.EV, ws.Description)
		})
	}
}

// TestExactSearch_TiesPickFirstOption checks the tie-breaking rule on a
// model where every option is worth 0: the first option wins everywhere.
func TestExactSearch_TiesPickFirstOption(t *testing.T) {
	exp := generatedExperiment(t, genSpec{layers: 2, outcomes: 2, options: 3, seed: 5, zero: true})
	sol, err := exp.ExactSource().Solve(context.Background(), exp.Model, exp.Worlds)
	require.NoError(t, err)
	ws := sol.Worlds[0]
	require.Zero(t, ws.EV)
	for _, is := range ws.InformationSets {
		require.Equal(t, "0", is.Choice, is.InfoSet)
	}
	require.NotContains(t, ws.Description, "]=1")
	require.NotContains(t, ws.Description, "]=2")
	// Only information sets after option "0" are reached.
	for _, is := range ws.InformationSets {
		if is.Parent != nil && is.Parent.Option != "0" {
			require.Zero(t, is.ReachProbability, is.InfoSet)
		}
	}
	// Values within TieTolerance are ties; a larger gap is not.
	leaf := func(v float64) *tnode { return &tnode{kind: kindLeaf, value: v, done: true} }
	h := &tnode{kind: kindDecision, prob: 0.5, children: []*tnode{leaf(100), leaf(100 + 1e-8), leaf(100 + 1e-6)}}
	s := &solveSet{is: infoSet{options: []string{"a", "b", "c"}}, histories: []*tnode{h}}
	require.NoError(t, solveSetChoice(s))
	require.Equal(t, 2, s.choice)
	require.InDelta(t, 0.5, s.chance, 0)
	require.Equal(t, []float64{100, 100 + 1e-8, 100 + 1e-6}, s.values, "values are conditional on reaching the set")
	h.children = h.children[:2]
	s.is.options = s.is.options[:2]
	require.NoError(t, solveSetChoice(s))
	require.Equal(t, 0, s.choice, "1e-8 apart at 100 is within TieTolerance·100")
}

// TestExactSearch_GeneratedDeterministic solves a generated model with 1
// and 8 workers and compares the JSON.
func TestExactSearch_GeneratedDeterministic(t *testing.T) {
	g := genSpec{layers: 2, outcomes: 3, options: 2, seed: 11}
	exp := generatedExperiment(t, g)
	r1, err := Run(context.Background(), exp, exp.PolicySource(), Options{Workers: 1})
	require.NoError(t, err)
	r8, err := Run(context.Background(), exp, exp.PolicySource(), Options{Workers: 8})
	require.NoError(t, err)
	require.Equal(t, marshal(t, r1), marshal(t, r8))
	require.False(t, math.IsNaN(r1.Worlds[0].Solution.EV))
}
