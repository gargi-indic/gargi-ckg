package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/kernel"
	"github.com/gargi-indic/gargi-flow/internal/sim/sampler"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

// update regenerates testdata/loan_golden.json:
//
//	go test ./internal/sim/experiment -run TestLoanGolden -update
var update = flag.Bool("update", false, "regenerate testdata/loan_golden.json")

const (
	loanGoldenFile = "testdata/loan_golden.json"
	// approvedConstraint and abandonedConstraint are added to a copy of the
	// loan model so that the exact evaluator reports the approval and
	// abandonment probabilities.
	approvedConstraint  = "approved"
	abandonedConstraint = "abandoned"
	// goldenMCRuns is the number of Monte Carlo replications of the top
	// policy of each world (the P0 exit criterion: 200k runs, seed 42).
	goldenMCRuns = 200_000
	// goldenMCRunsRace keeps the check bounded under the race detector.
	goldenMCRunsRace = 20_000
)

// loanGolden is the schema of testdata/loan_golden.json. Entries are sorted
// by world, then policy (the TablePolicy.Describe string); floats are stored
// with full precision (shortest round-trip representation).
type loanGolden struct {
	// Model is the sim/v1 model id.
	Model string `json:"model"`
	// Regenerate is the command that rewrites the file.
	Regenerate string `json:"regenerate"`
	// Entries holds one row per (world, policy): 42 policies x 2 worlds.
	Entries []loanGoldenEntry `json:"entries"`
}

type loanGoldenEntry struct {
	// World is the world id.
	World string `json:"world"`
	// Policy is the policy's Describe() string.
	Policy string `json:"policy"`
	// EV is the exact expected profit per application.
	EV float64 `json:"ev"`
	// ApprovalRate is the exact probability that the approve node succeeds.
	ApprovalRate float64 `json:"approvalRate"`
	// MeanDurationSeconds is the exact expected simulated duration. An
	// applicant who abandons the human review ends the episode when the
	// review starts, so the 48h wait is not counted for them.
	MeanDurationSeconds float64 `json:"meanDurationSeconds"`
	// AbandonRate is the exact probability that the applicant abandons the
	// human review. The prototype counts the full review wait for those
	// applicants: its days = MeanDurationSeconds/86400 + 2*AbandonRate.
	AbandonRate float64 `json:"abandonRate"`
}

// loanWithApproval compiles the shipped loan experiment (search:
// exhaustive, both worlds) over a copy of the loan model that adds the
// "approved" and "abandoned" constraints, so estimates carry those
// probabilities. Constraints change neither the objective nor the policy
// space.
func loanWithApproval(t testing.TB, evaluator string, runs int) *CompiledExperiment {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "graph.yaml", readLoan(t, "graph.yaml"))
	s := readLoan(t, "sim.yaml")
	withC := strings.Replace(s, "  maximize: mean\n",
		"  maximize: mean\n  constraints:\n"+
			"    "+approvedConstraint+": \"nodes.approve.status == 'Succeeded'\"\n"+
			"    "+abandonedConstraint+": \"run.abandoned\"\n", 1)
	require.NotEqual(t, s, withC, "the loan objective changed; update loanWithApproval")
	writeFile(t, dir, "sim.yaml", withC)
	writeFile(t, dir, "experiment.yaml", readLoan(t, "experiment.yaml"))
	exp, err := LoadFile(filepath.Join(dir, "experiment.yaml"), registry(t))
	require.NoError(t, err)
	return withEvaluator(exp, evaluator, runs)
}

// TestLoanGolden locks the P0 exit criterion: the exact EV, approval rate,
// mean duration and abandon rate of all 42 loan policies in both worlds match
// testdata/loan_golden.json, the derived regret and value of information
// match the prototype (docs/simulation/prototypes/loan_sim.py), and a 200k
// run Monte Carlo estimate (seed 42) of each world's top policy lies within
// 3 standard errors of its exact value.
func TestLoanGolden(t *testing.T) {
	exp := loanWithApproval(t, EvaluatorExact, 1)
	policies, err := exp.PolicySource().Policies(exp.Model)
	require.NoError(t, err)
	require.Len(t, policies, 42)
	rep, err := Run(context.Background(), exp, staticSource(policies), Options{All: true})
	require.NoError(t, err)
	require.Equal(t, []sim.WorldID{"base", "recession"}, []sim.WorldID{rep.Worlds[0].World, rep.Worlds[1].World})

	got := loanGolden{
		Model:      rep.Model,
		Regenerate: "go test ./internal/sim/experiment -run TestLoanGolden -update",
	}
	for _, w := range rep.Worlds {
		require.Len(t, w.All, 42)
		for _, r := range w.All {
			require.True(t, r.Objective.Exact)
			got.Entries = append(got.Entries, loanGoldenEntry{
				World:               string(w.World),
				Policy:              r.Description,
				EV:                  r.Objective.Mean,
				ApprovalRate:        r.Objective.ConstraintProb[approvedConstraint],
				MeanDurationSeconds: r.Duration.Mean,
				AbandonRate:         r.Objective.ConstraintProb[abandonedConstraint],
			})
		}
	}
	sort.Slice(got.Entries, func(i, j int) bool {
		a, b := got.Entries[i], got.Entries[j]
		if a.World != b.World {
			return a.World < b.World
		}
		return a.Policy < b.Policy
	})

	if *update {
		data, err := json.MarshalIndent(got, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(loanGoldenFile), 0o755))
		require.NoError(t, os.WriteFile(loanGoldenFile, append(data, '\n'), 0o600))
		t.Logf("wrote %s (%d entries)", loanGoldenFile, len(got.Entries))
	}

	data, err := os.ReadFile(loanGoldenFile)
	require.NoError(t, err, "regenerate with: go test ./internal/sim/experiment -run TestLoanGolden -update")
	var want loanGolden
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(&want))
	require.Equal(t, want.Model, got.Model)
	require.Len(t, want.Entries, 84)
	require.Len(t, got.Entries, len(want.Entries))
	for i, w := range want.Entries {
		g := got.Entries[i]
		require.Equal(t, w.World+" | "+w.Policy, g.World+" | "+g.Policy, "entry %d", i)
		require.InDelta(t, w.EV, g.EV, 1e-9, "EV %s | %s", w.World, w.Policy)
		require.InDelta(t, w.ApprovalRate, g.ApprovalRate, 1e-9, "approval rate %s | %s", w.World, w.Policy)
		require.InDelta(t, w.MeanDurationSeconds, g.MeanDurationSeconds, 1e-9, "mean duration %s | %s", w.World, w.Policy)
		require.InDelta(t, w.AbandonRate, g.AbandonRate, 1e-9, "abandon rate %s | %s", w.World, w.Policy)
	}

	// The golden EVs agree with the prototype's exact() as well.
	ev := map[string]map[string]float64{}
	for _, e := range want.Entries {
		if ev[e.World] == nil {
			ev[e.World] = map[string]float64{}
		}
		ev[e.World][e.Policy] = e.EV
	}
	for _, row := range prototypeEV {
		d := prototypeToDescribe(t, row.desc)
		require.InDelta(t, row.base, ev["base"][d], 1e-9, d)
		require.InDelta(t, row.recession, ev["recession"][d], 1e-9, d)
	}

	checkLoanDerived(t, ev)

	// Monte Carlo cross-check of the top policy of each world.
	runs := goldenMCRuns
	if raceEnabled {
		runs = goldenMCRunsRace
	}
	byID := map[string]sim.Policy{}
	for _, p := range policies {
		byID[p.ID()] = p
	}
	for w, wr := range rep.Worlds {
		best := wr.Policies[0]
		one := withEvaluator(exp, EvaluatorMonteCarlo, runs)
		one.Worlds = []sim.World{exp.Worlds[w]}
		mcRep, err := Run(context.Background(), one, staticSource{byID[best.PolicyID]}, Options{})
		require.NoError(t, err)
		mc := mcRep.Worlds[0].Policies[0].Objective
		require.Equal(t, uint64(42), mcRep.Seed)
		require.Equal(t, runs, mc.N)
		se := mc.StdDev / math.Sqrt(float64(mc.N))
		t.Logf("%s: best %q exact EV %.4f, Monte Carlo %.4f (n=%d, SE %.4f, %.2f SE)",
			wr.World, best.Description, best.Objective.Mean, mc.Mean, mc.N, se, math.Abs(mc.Mean-best.Objective.Mean)/se)
		require.InDelta(t, best.Objective.Mean, mc.Mean, 3*se, "Monte Carlo mean of %s in %s", best.Description, wr.World)

		p := best.Objective.ConstraintProb[approvedConstraint]
		pse := math.Sqrt(p * (1 - p) / float64(mc.N))
		require.InDelta(t, p, mc.ConstraintProb[approvedConstraint], 3*pse, "Monte Carlo approval rate of %s in %s", best.Description, wr.World)
	}
}

// checkLoanDerived recomputes the cross-world regret and the value of
// information of the AI score the way loan_sim.py prints them:
//
//   - regret of applying world A's optimum in world B is
//     max EV in B - EV in B of A's optimum;
//   - VOI of the score in a world is the best EV among policies that buy
//     the score (screen=score) minus the best EV among those that skip it.
func checkLoanDerived(t *testing.T, ev map[string]map[string]float64) {
	t.Helper()
	argmax := func(m map[string]float64, keep func(string) bool) (string, float64) {
		best, bestEV := "", math.Inf(-1)
		for d, v := range m {
			if keep(d) && (v > bestEV || (v == bestEV && d < best)) {
				best, bestEV = d, v
			}
		}
		return best, bestEV
	}
	all := func(string) bool { return true }
	bestBase, evBase := argmax(ev["base"], all)
	bestRec, evRec := argmax(ev["recession"], all)
	require.Equal(t, bestBaseDesc, bestBase)
	require.Equal(t, bestRecessionDesc, bestRec)

	round1 := func(x float64) float64 { return math.Round(x*10) / 10 }
	regretBaseInRecession := evRec - ev["recession"][bestBase]
	regretRecessionInBase := evBase - ev["base"][bestRec]
	require.InDelta(t, 267.5, regretBaseInRecession, 0.1, "regret of the base-tuned policy in a recession")
	require.InDelta(t, 142.2, regretRecessionInBase, 0.1, "regret of the recession-tuned policy in the base world")
	require.Equal(t, 267.5, round1(regretBaseInRecession))
	require.Equal(t, 142.2, round1(regretRecessionInBase))

	voi := func(world string) float64 {
		_, withScore := argmax(ev[world], func(d string) bool { return strings.HasPrefix(d, "screen=score;") })
		_, without := argmax(ev[world], func(d string) bool { return strings.HasPrefix(d, "screen=skip;") })
		return withScore - without
	}
	require.InDelta(t, 158.0, voi("base"), 0.1, "VOI of the score in base")
	require.InDelta(t, 130.3, voi("recession"), 0.1, "VOI of the score in recession")
	require.Equal(t, 158.0, round1(voi("base")))
	require.Equal(t, 130.3, round1(voi("recession")))
}

// BenchmarkLoanEpisode measures one Monte Carlo episode of the loan model
// with the recession-v1 policy in the recession world.
func BenchmarkLoanEpisode(b *testing.B) {
	exp := loanExperiment(b, loanDir, 1, "recession")
	policies, err := exp.FilesSource().Policies(exp.Model)
	require.NoError(b, err)
	p, world := policies[0], exp.Worlds[0]
	ctx := context.Background()
	b.ReportAllocs()
	var seed uint64
	for b.Loop() {
		seed++
		if _, err := kernel.RunEpisode(ctx, exp.Model, world, p, sampler.NewMonteCarlo(seed), kernel.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoanExperiment measures a Monte Carlo experiment of 5,000 runs of
// the recession-v1 policy in the recession world on one worker.
func BenchmarkLoanExperiment(b *testing.B) {
	const runs = 5000
	exp := loanExperiment(b, loanDir, runs, "recession")
	src := exp.FilesSource()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Run(ctx, exp, src, Options{Workers: 1}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*runs), "ns/episode")
}
