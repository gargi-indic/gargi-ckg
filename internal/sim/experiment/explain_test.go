package experiment

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/kernel"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/internal/sim/report"
	"github.com/gargi-indic/gargi-flow/internal/sim/sampler"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

// loanExplain loads the loan model with the given policies and evaluator
// lines and reports list.
func loanExplain(t *testing.T, policies, evaluator, reports string) *CompiledExperiment {
	t.Helper()
	return loanVariant(t, "", "", "apiVersion: experiment/v1\nmodel: ./sim.yaml\nworlds: [base, recession]\n"+
		"policies: "+policies+"\n"+evaluator+"seed: 3\nreports: "+reports+"\n")
}

const (
	exactSearch    = "{search: exact}"
	exhaustive     = "{search: exhaustive}"
	threeFiles     = "{files: [recession.yaml, approve.yaml, reject.yaml]}"
	exactEvaluator = "evaluator: exact\nruns: 1\n"
)

func round1(x float64) float64 { return math.Round(x*10) / 10 }

func runReport(t *testing.T, exp *CompiledExperiment, workers int) *Report {
	t.Helper()
	rep, err := Run(context.Background(), exp, exp.PolicySource(), Options{Workers: workers})
	require.NoError(t, err)
	require.NotNil(t, rep.Reports)
	return rep
}

// TestExplain_LoanVOI checks the acceptance numbers: the VOI of the AI
// score is 158.0 in the base world and 130.3 (130.275) in a recession, the
// same as checkLoanDerived's definition (best EV with the score minus best
// EV without it) on the exhaustive policy list.
func TestExplain_LoanVOI(t *testing.T) {
	exp := loanExplain(t, exactSearch, exactEvaluator, "[voi:ai_score]")
	rep := runReport(t, exp, 0)
	v := rep.Reports.VOI
	require.Len(t, v, 2)
	require.Nil(t, rep.Reports.FlipPoints)
	require.Nil(t, rep.Reports.PathHeatmap)
	require.Nil(t, rep.Reports.PolicyTree)

	want := []struct {
		world         sim.WorldID
		with, without float64
		voi           float64
	}{{"base", 460, 302, 158.0}, {"recession", 202.525, 72.25, 130.275}}
	for w, e := range want {
		got := v[w]
		require.Equal(t, e.world, got.World)
		require.Equal(t, "ai_score", got.Node)
		require.Equal(t, rep.Worlds[w].Solution.PolicyID, got.WithObservation.PolicyID)
		require.InDelta(t, e.with, got.WithObservation.EV, 1e-9)
		require.InDelta(t, e.without, got.WithoutObservation.EV, 1e-9)
		require.InDelta(t, e.voi, got.VOI, 1e-9)
		require.Equal(t, got.WithObservation.EV-got.WithoutObservation.EV, got.VOI)
	}
	require.Equal(t, 158.0, round1(v[0].VOI))
	require.Equal(t, 130.3, round1(v[1].VOI))
	// Without observing the score, both worlds' optimum skips it.
	require.Equal(t, v[0].WithoutObservation.PolicyID, v[1].WithoutObservation.PolicyID)

	// The definition of checkLoanDerived on the 42 enumerated policies.
	ex := loanExplain(t, exhaustive, exactEvaluator, "[voi:ai_score]")
	policies, err := ex.PolicySource().Policies(ex.Model)
	require.NoError(t, err)
	exRep, err := Run(context.Background(), ex, staticSource(policies), Options{All: true})
	require.NoError(t, err)
	for w, wr := range exRep.Worlds {
		withScore, without := math.Inf(-1), math.Inf(-1)
		for _, r := range wr.All {
			if strings.HasPrefix(r.Description, "screen=score;") {
				withScore = max(withScore, r.Objective.Mean)
			} else {
				without = max(without, r.Objective.Mean)
			}
		}
		require.InDelta(t, withScore-without, v[w].VOI, 1e-9, wr.World)
	}
	// Exhaustive search solves the worlds for the report; the VOI is the
	// same.
	exJS, err := json.Marshal(exRep.Reports.VOI)
	require.NoError(t, err)
	js, err := json.Marshal(v)
	require.NoError(t, err)
	require.JSONEq(t, string(js), string(exJS))

	js = []byte(marshal(t, rep))
	require.Contains(t, string(js), `"reports":{"voi":[{"world":"base","node":"ai_score","withObservation":{"policyId":"`+v[0].WithObservation.PolicyID+`","ev":460}`)
}

// TestExplain_WithoutObservation checks the modified model and that the
// original is untouched.
func TestExplain_WithoutObservation(t *testing.T) {
	exp := loanExplain(t, exactSearch, exactEvaluator, "[voi:ai_score]")
	observes := func(m *model.CompiledModel) map[string][]any {
		out := map[string][]any{}
		for _, n := range m.Graph.Nodes {
			if n.Type == "decision" {
				out[string(n.ID)], _ = n.Config["observes"].([]any)
			}
		}
		return out
	}
	before := observes(exp.Model)
	m, err := WithoutObservation(exp.Model, "ai_score")
	require.NoError(t, err)
	require.NotSame(t, exp.Model, m)
	require.Equal(t, before, observes(exp.Model))
	require.Equal(t, []any{"screen.output.choice", "ai_score.output.band"}, before["route"])
	require.Equal(t, map[string][]any{
		"screen": {},
		"route":  {"screen.output.choice"},
		"final":  {"screen.output.choice", "human_review.output.result"},
	}, observes(m))
	require.NotNil(t, exp.Model.CompiledBehaviors["ai_score"].Cost)
	require.NotNil(t, exp.Model.Def.Behaviors["ai_score"].Cost)
	require.Nil(t, m.CompiledBehaviors["ai_score"].Cost)
	require.Equal(t, exp.Model.Def.Behaviors["ai_score"].Output, m.Def.Behaviors["ai_score"].Output)
	require.NotNil(t, m.CompiledBehaviors["human_review"].Cost)
	require.NotNil(t, m.CompiledBehaviors["human_review"].AbandonP)

	_, err = WithoutObservation(nil, "x")
	require.Error(t, err)
}

// voiGraph and voiSim are a model whose observed node always runs: a free
// signal would be worth having, but the decision cannot skip paying for
// it. good ~ Bernoulli(0.6); the perfect signal costs 20; accepting pays
// +100 when good and -100 otherwise. With the signal: 0.6·100 - 20 = 40.
// Without it (and without its cost): accept, 0.6·100 - 0.4·100 = 20. So
// VOI = 20; keeping the cost in the modified model would give 40.
const (
	voiGraph = `apiVersion: graph/v1
id: signal
nodes:
  - id: info
    type: http_call
    config: {url: "https://signal.example.internal", method: GET}
  - id: decide
    type: decision
    config: {options: [accept, decline], observes: [info.output.signal]}
  - {id: accept, type: passthrough}
  - {id: decline, type: passthrough}
edges:
  - {from: info, to: decide}
  - {from: decide, to: accept, condition: "upstream.output.choice == 'accept'"}
  - {from: decide, to: decline, condition: "upstream.output.choice == 'decline'"}
`
	voiSim = `apiVersion: sim/v1
id: signal
graph: ./graph.yaml
worlds:
  w: {p: 0.6}
latents:
  good: {bernoulli: {p: "world.p"}}
behaviors:
  info:
    cost: 20
    output:
      signal: {choice: {"yes": "latent.good ? 1.0 : 0.0", "no": "latent.good ? 0.0 : 1.0"}}
objective:
  name: payoff
  value: "-run.cost + (nodes.accept.status == 'Succeeded' ? (latent.good ? 100.0 : -100.0) : 0.0)"
  maximize: mean
`
)

func TestExplain_VOIRemovesCost(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "graph.yaml", voiGraph)
	writeFile(t, dir, "sim.yaml", voiSim)
	exp, err := LoadFile(writeFile(t, dir, "experiment.yaml",
		"apiVersion: experiment/v1\nmodel: ./sim.yaml\npolicies: {search: exact}\nevaluator: exact\nruns: 1\nseed: 1\nreports: [voi:info, policy_tree, path_heatmap]\n"), registry(t))
	require.NoError(t, err)
	rep := runReport(t, exp, 0)
	require.Len(t, rep.Reports.VOI, 1)
	v := rep.Reports.VOI[0]
	require.InDelta(t, 40.0, v.WithObservation.EV, 1e-9)
	require.InDelta(t, 20.0, v.WithoutObservation.EV, 1e-9)
	require.InDelta(t, 20.0, v.VOI, 1e-9)

	tree := rep.Reports.PolicyTree[0]
	require.Len(t, tree.Roots, 2)
	require.Equal(t, []string{"decide[no]", "decide[yes]"}, []string{tree.Roots[0].InfoSet, tree.Roots[1].InfoSet})
	require.Equal(t, "decline", tree.Roots[0].Choice)
	require.Equal(t, "accept", tree.Roots[1].Choice)

	hm := rep.Reports.PathHeatmap[0]
	require.Equal(t, []report.NodeMass{{ID: "info", Mass: 1}, {ID: "decide", Mass: 1}, {ID: "accept", Mass: 0.6}, {ID: "decline", Mass: 0.4}}, roundMasses(hm.Nodes))
}

func roundMasses(in []report.NodeMass) []report.NodeMass {
	out := make([]report.NodeMass, len(in))
	for i, n := range in {
		out[i] = report.NodeMass{ID: n.ID, Mass: math.Round(n.Mass*1e9) / 1e9}
	}
	return out
}

// TestExplain_LoanFlipPoint sweeps p_bad over the experiment's worlds
// (0.20 to 0.35, loss kept at the base world's 4000). The base optimum
// (EV 880 - 2100·p) gives way to the review policy (EV 443.5 - 628.5·p) at
// p = 436.5/1471.5 ≈ 0.29664.
func TestExplain_LoanFlipPoint(t *testing.T) {
	exp := loanExplain(t, exactSearch, exactEvaluator, "[flip_point:p_bad]")
	require.Equal(t, []report.Spec{{Kind: report.KindFlipPoint, Param: "p_bad", Lo: 0.2, Hi: 0.35}}, exp.Reports)
	rep := runReport(t, exp, 0)
	require.Len(t, rep.Reports.FlipPoints, 1)
	fp := rep.Reports.FlipPoints[0]
	require.Equal(t, "p_bad", fp.Param)
	require.Equal(t, sim.WorldID("base"), fp.BaseWorld)
	require.Equal(t, 0.2, fp.Lo)
	require.Equal(t, 0.35, fp.Hi)
	require.Len(t, fp.Sweep, FlipSweepPoints)
	require.Equal(t, 0.2, fp.Sweep[0].Value)
	require.Equal(t, 0.35, fp.Sweep[FlipSweepPoints-1].Value)
	for i := 1; i < len(fp.Sweep); i++ {
		require.Greater(t, fp.Sweep[i].Value, fp.Sweep[i-1].Value)
	}
	require.Equal(t, rep.Worlds[0].Solution.PolicyID, fp.Sweep[0].PolicyID)
	require.InDelta(t, 460, fp.Sweep[0].EV, 1e-9)
	// At p = 0.35 with loss 4000: 443.5 - 628.5·0.35.
	require.InDelta(t, 443.5-628.5*0.35, fp.Sweep[FlipSweepPoints-1].EV, 1e-9)
	require.Equal(t, rep.Worlds[1].Solution.PolicyID, fp.Sweep[FlipSweepPoints-1].PolicyID)

	require.Len(t, fp.Flips, 1)
	f := fp.Flips[0]
	require.Equal(t, bestBaseDesc, f.From)
	require.Equal(t, bestRecessionDesc, f.To)
	require.Equal(t, fp.Sweep[0].PolicyID, f.Left.PolicyID)
	require.Equal(t, fp.Sweep[FlipSweepPoints-1].PolicyID, f.Right.PolicyID)
	require.Less(t, f.Left.Value, f.Right.Value)
	require.LessOrEqual(t, f.Right.Value-f.Left.Value, FlipTolerance*(fp.Hi-fp.Lo))
	flip := 436.5 / 1471.5
	require.LessOrEqual(t, f.Left.Value, flip)
	require.GreaterOrEqual(t, f.Right.Value, flip)
	require.InDelta(t, 880-2100*f.Left.Value, f.Left.EV, 1e-9)
	require.InDelta(t, 443.5-628.5*f.Right.Value, f.Right.EV, 1e-9)
	checkFlipEnds(t, exp, fp)

	// Reproducible: the same input gives identical JSON.
	again := runReport(t, exp, 7)
	require.Equal(t, marshal(t, rep), marshal(t, again))
	t.Logf("flip of p_bad in [%.9f, %.9f]", f.Left.Value, f.Right.Value)
}

// checkFlipEnds solves the base world at each flip's ends independently.
func checkFlipEnds(t *testing.T, exp *CompiledExperiment, fp report.FlipPoint) {
	t.Helper()
	at := func(v float64) WorldSolution {
		params := map[string]any{}
		for k, val := range exp.Worlds[0].Params {
			params[k] = val
		}
		params[fp.Param] = v
		sol, err := ExactSource{}.Solve(context.Background(), exp.Model, []sim.World{{ID: "probe", Params: params}})
		require.NoError(t, err)
		return sol.Worlds[0]
	}
	for i, f := range fp.Flips {
		require.NotEqual(t, f.Left.PolicyID, f.Right.PolicyID)
		l, r := at(f.Left.Value), at(f.Right.Value)
		require.Equal(t, f.Left.PolicyID, l.PolicyID)
		require.Equal(t, f.From, l.Description)
		require.Equal(t, f.Right.PolicyID, r.PolicyID)
		require.Equal(t, f.To, r.Description)
		require.Equal(t, l.EV, f.Left.EV)
		require.Equal(t, r.EV, f.Right.EV)
		if i > 0 {
			require.Greater(t, f.Left.Value, fp.Flips[i-1].Right.Value)
		}
	}
}

// TestExplain_FlipPointRange sweeps an explicit range with several flips.
func TestExplain_FlipPointRange(t *testing.T) {
	exp := loanExplain(t, exhaustive, exactEvaluator, "['flip_point:p_bad:0.05,0.95', 'flip_point:loss:100,20000']")
	require.Equal(t, []report.Spec{
		{Kind: report.KindFlipPoint, Param: "p_bad", HasRange: true, Lo: 0.05, Hi: 0.95},
		{Kind: report.KindFlipPoint, Param: "loss", HasRange: true, Lo: 100, Hi: 20000},
	}, exp.Reports)
	rep := runReport(t, exp, 0)
	require.Len(t, rep.Reports.FlipPoints, 2)
	for _, fp := range rep.Reports.FlipPoints {
		require.Equal(t, sim.WorldID("base"), fp.BaseWorld)
		require.Len(t, fp.Sweep, FlipSweepPoints)
		require.NotEmpty(t, fp.Flips, fp.Param)
		for _, f := range fp.Flips {
			require.LessOrEqual(t, f.Right.Value-f.Left.Value, FlipTolerance*(fp.Hi-fp.Lo))
			t.Logf("%s in [%.6g, %.6g]: %s -> %s", fp.Param, f.Left.Value, f.Right.Value, f.From, f.To)
		}
		checkFlipEnds(t, exp, fp)
	}
	// p_bad: review high scores, then approve low and reject high scores
	// (the base optimum), then review low scores (the recession optimum),
	// then reject everything without paying for the score.
	pBad := rep.Reports.FlipPoints[0]
	require.Len(t, pBad.Flips, 3)
	require.Equal(t, bestBaseDesc, pBad.Flips[0].To)
	require.Equal(t, bestBaseDesc, pBad.Flips[1].From)
	require.Equal(t, bestRecessionDesc, pBad.Flips[1].To)
	require.Equal(t, bestRecessionDesc, pBad.Flips[2].From)
	require.Equal(t, "screen=skip; route[skip,nil]=reject", pBad.Flips[2].To)
	require.LessOrEqual(t, pBad.Flips[1].Left.Value, 436.5/1471.5)
	require.GreaterOrEqual(t, pBad.Flips[1].Right.Value, 436.5/1471.5)
}

// TestExplain_LoanHeatmapExact checks the masses under each world's best
// policy against hand-computed probabilities and the decision rule: the
// outgoing edges of every decision carry exactly the decision's mass.
func TestExplain_LoanHeatmapExact(t *testing.T) {
	exp := loanExplain(t, exhaustive, exactEvaluator, "[path_heatmap]")
	policies, err := exp.PolicySource().Policies(exp.Model)
	require.NoError(t, err)
	rep, err := Run(context.Background(), exp, staticSource(policies), Options{})
	require.NoError(t, err)
	hms := rep.Reports.PathHeatmap
	require.Len(t, hms, 2)
	for w, hm := range hms {
		require.Equal(t, rep.Worlds[w].World, hm.World)
		require.Equal(t, rep.Worlds[w].Policies[0].PolicyID, hm.PolicyID)
		checkHeatmapShape(t, exp.Model, hm)
		require.Equal(t, 3, checkDecisionSums(t, exp.Model, hm, 1e-9), "screen, route and final")
	}
	mass := heatmapIndex(hms[0])
	// Base: score, approve low (0.3·0.2 + 0.9·0.8 = 0.78), reject high.
	require.InDelta(t, 1, mass["screen"], 1e-12)
	require.InDelta(t, 1, mass["ai_score"], 1e-12)
	require.InDelta(t, 0.78, mass["approve"], 1e-12)
	require.InDelta(t, 0.22, mass["reject"], 1e-12)
	require.InDelta(t, 0.78, mass["route->approve"], 1e-12)
	require.Zero(t, mass["human_review"])
	require.Zero(t, mass["screen->route"])
	// Recession: review low (0.3·0.35 + 0.9·0.65 = 0.69); good applicants
	// abandon 30% of reviews, so final runs with 0.105 + 0.585·0.7.
	mass = heatmapIndex(hms[1])
	require.InDelta(t, 0.69, mass["human_review"], 1e-12)
	require.InDelta(t, 0.69, mass["route->human_review"], 1e-12)
	require.InDelta(t, 0.5145, mass["final"], 1e-12)
	require.InDelta(t, 0.5145, mass["human_review->final"], 1e-12)
	require.InDelta(t, 0.31, mass["route->reject"], 1e-12)
	// Approval: good pass 0.585·0.7·0.95 plus bad pass 0.105·0.1.
	require.InDelta(t, 0.585*0.7*0.95+0.105*0.1, mass["approve"], 1e-12)
	require.InDelta(t, mass["approve"], mass["final->approve"], 1e-12)

	// Evaluator both uses the exact branch weights too.
	both := withEvaluator(exp, EvaluatorBoth, 20)
	repBoth, err := Run(context.Background(), both, staticSource(policies), Options{})
	require.NoError(t, err)
	js, err := json.Marshal(repBoth.Reports.PathHeatmap)
	require.NoError(t, err)
	want, err := json.Marshal(hms)
	require.NoError(t, err)
	require.Equal(t, string(want), string(js))
}

// TestExplain_LoanHeatmapMonteCarlo checks that Monte Carlo masses are run
// counts of the experiment's own replications, that every decision's
// outgoing edge counts sum to its count exactly, and that the counts do not
// depend on the worker count.
func TestExplain_LoanHeatmapMonteCarlo(t *testing.T) {
	runs := 1100 // a full chunk and a partial one
	if raceEnabled {
		runs = 1010
	}
	exp := loanExplain(t, threeFiles, "runs: "+itoa(runs)+"\n", "[path_heatmap]")
	r1 := runReport(t, exp, 1)
	r8 := runReport(t, exp, 8)
	require.Equal(t, marshal(t, r1), marshal(t, r8))
	hms := r1.Reports.PathHeatmap
	require.Len(t, hms, 2)

	counts := func(masses []float64) []int {
		out := make([]int, len(masses))
		for i, m := range masses {
			c := math.Round(m * float64(runs))
			require.InDelta(t, c, m*float64(runs), 1e-6)
			out[i] = int(c)
		}
		return out
	}
	for w, hm := range hms {
		require.Equal(t, r1.Worlds[w].Policies[0].PolicyID, hm.PolicyID)
		checkHeatmapShape(t, exp.Model, hm)
		nodeMass := make([]float64, len(hm.Nodes))
		for i, n := range hm.Nodes {
			nodeMass[i] = n.Mass
		}
		edgeMass := make([]float64, len(hm.Edges))
		for i, e := range hm.Edges {
			edgeMass[i] = e.Mass
		}
		nodes, edges := counts(nodeMass), counts(edgeMass)
		for i, n := range exp.Model.Graph.Nodes {
			if n.Type != "decision" {
				continue
			}
			sum := 0
			for j, e := range exp.Model.Graph.Edges {
				if e.From == n.ID {
					sum += edges[j]
				}
			}
			require.Equal(t, nodes[i], sum, "decision %s", n.ID)
		}

		// Recount the experiment's replications directly.
		policies, err := exp.FilesSource().Policies(exp.Model)
		require.NoError(t, err)
		var best sim.Policy
		for _, p := range policies {
			if p.ID() == hm.PolicyID {
				best = p
			}
		}
		require.NotNil(t, best)
		world := exp.Worlds[w]
		want := make([]int, len(exp.Model.Graph.Edges))
		for r := range runs {
			rec, err := kernel.RunEpisode(context.Background(), exp.Model, world, best, sampler.NewMonteCarlo(sim.Seed(exp.Def.Seed, world.ID, r)), kernel.Options{Trace: true})
			require.NoError(t, err)
			for _, i := range rec.Edges {
				want[i]++
			}
		}
		require.Equal(t, want, edges, world.ID)
	}
	// The recession policy reviews 69% of applications in a recession.
	mass := heatmapIndex(hms[1])
	require.Equal(t, recessionID, hms[1].PolicyID)
	se := math.Sqrt(0.69 * 0.31 / float64(runs))
	require.InDelta(t, 0.69, mass["human_review"], 4*se)
}

// heatmapIndex maps node ids and "from->to" edges to their mass.
func heatmapIndex(hm report.PathHeatmap) map[string]float64 {
	out := map[string]float64{}
	for _, n := range hm.Nodes {
		out[n.ID] = n.Mass
	}
	for _, e := range hm.Edges {
		out[e.From+"->"+e.To] += e.Mass
	}
	return out
}

// checkHeatmapShape checks that every node and edge is listed in graph
// order with masses in [0, 1].
func checkHeatmapShape(t *testing.T, m *model.CompiledModel, hm report.PathHeatmap) {
	t.Helper()
	require.Len(t, hm.Nodes, len(m.Graph.Nodes))
	require.Len(t, hm.Edges, len(m.Graph.Edges))
	for i, n := range m.Graph.Nodes {
		require.Equal(t, string(n.ID), hm.Nodes[i].ID)
		require.True(t, hm.Nodes[i].Mass >= 0 && hm.Nodes[i].Mass <= 1+1e-12)
	}
	for i, e := range m.Graph.Edges {
		require.Equal(t, report.EdgeMass{From: string(e.From), To: string(e.To), Index: i, Mass: hm.Edges[i].Mass}, hm.Edges[i])
		require.True(t, hm.Edges[i].Mass >= 0 && hm.Edges[i].Mass <= 1+1e-12)
	}
}

// checkDecisionSums checks that the outgoing edges of every decision that
// has any sum to the decision's mass (it returns how many it checked).
func checkDecisionSums(t *testing.T, m *model.CompiledModel, hm report.PathHeatmap, tol float64) int {
	t.Helper()
	checked := 0
	for i, n := range m.Graph.Nodes {
		if n.Type != "decision" {
			continue
		}
		sum, out := 0.0, 0
		for j, e := range m.Graph.Edges {
			if e.From == n.ID {
				sum += hm.Edges[j].Mass
				out++
			}
		}
		if out == 0 {
			continue
		}
		checked++
		require.InDelta(t, hm.Nodes[i].Mass, sum, tol, "decision %s in %s", n.ID, hm.World)
	}
	return checked
}

// treeChoices walks the tree and returns the on-path choices and the number
// of nodes, checking reach probabilities against the solution.
func treeChoices(t *testing.T, ws *WorldSolution, roots []report.TreeNode) (map[string]string, int) {
	t.Helper()
	byName := map[string]InformationSetValue{}
	for _, is := range ws.InformationSets {
		byName[is.InfoSet] = is
	}
	onPath := map[string]string{}
	count := 0
	var walk func(n report.TreeNode, parentOnPath bool)
	walk = func(n report.TreeNode, parentOnPath bool) {
		count++
		is, ok := byName[n.InfoSet]
		require.True(t, ok, n.InfoSet)
		require.Equal(t, is.Decision, n.Decision)
		require.Equal(t, is.Choice, n.Choice)
		require.Equal(t, is.ReachProbability, n.ReachProbability)
		require.Equal(t, is.Observed, n.Observed)
		require.Equal(t, parentOnPath, n.OnPath, n.InfoSet)
		require.Equal(t, n.OnPath, n.ReachProbability > 0, n.InfoSet)
		require.Len(t, n.Actions, len(is.Actions))
		if n.OnPath {
			onPath[n.InfoSet] = n.Choice
		}
		for a, act := range n.Actions {
			require.Equal(t, is.Actions[a].Option, act.Option)
			require.Equal(t, is.Actions[a].Value, act.Value)
			require.NotNil(t, act.Children)
			for _, c := range act.Children {
				parent := byName[c.InfoSet].Parent
				require.Equal(t, &InfoSetChoice{InfoSet: n.InfoSet, Option: act.Option}, parent)
				walk(c, n.OnPath && act.Option == n.Choice)
			}
		}
	}
	for _, r := range roots {
		require.Nil(t, byName[r.InfoSet].Parent)
		walk(r, true)
	}
	return onPath, count
}

// requireTreeRoundTrips rebuilds the solved table policy from the tree's
// on-path choices and compares it with WorldSolution.Policy().
func requireTreeRoundTrips(t *testing.T, ws *WorldSolution, tree report.PolicyTree) {
	t.Helper()
	require.Equal(t, ws.World, tree.World)
	require.Equal(t, ws.PolicyID, tree.PolicyID)
	require.Equal(t, ws.EV, tree.EV)
	choices, count := treeChoices(t, ws, tree.Roots)
	require.Equal(t, len(ws.InformationSets), count, "every information set, on and off the path, is in the tree")
	// Describe lists the reachable sets in the solution's order (decision
	// graph order, then label).
	var parts []string
	for _, is := range ws.InformationSets {
		if c, ok := choices[is.InfoSet]; ok {
			parts = append(parts, is.InfoSet+"="+c)
		}
	}
	require.Len(t, parts, len(choices))
	require.Equal(t, ws.Policy().Describe(), strings.Join(parts, "; "))
	require.Equal(t, ws.Description, strings.Join(parts, "; "))
}

func TestExplain_LoanPolicyTree(t *testing.T) {
	exp := loanExplain(t, exactSearch, exactEvaluator, "[policy_tree]")
	rep := runReport(t, exp, 0)
	trees := rep.Reports.PolicyTree
	require.Len(t, trees, 2)
	for w, tree := range trees {
		requireTreeRoundTrips(t, rep.Worlds[w].Solution, tree)
	}
	base := trees[0]
	require.Len(t, base.Roots, 1)
	require.Equal(t, "screen", base.Roots[0].InfoSet)
	require.Equal(t, "score", base.Roots[0].Choice)
	score := base.Roots[0].Actions[0]
	require.Equal(t, "score", score.Option)
	require.InDelta(t, 460, score.Value, 1e-9)
	require.Equal(t, []string{"route[score,high]", "route[score,low]"}, []string{score.Children[0].InfoSet, score.Children[1].InfoSet})
	skip := base.Roots[0].Actions[1]
	require.Len(t, skip.Children, 1)
	require.Equal(t, "route[skip,nil]", skip.Children[0].InfoSet)
	require.False(t, skip.Children[0].OnPath)
	require.InDelta(t, 302, skip.Value, 1e-9)

	// Without search: exact, the report solves the worlds itself and gets
	// the same trees.
	ex := loanExplain(t, threeFiles, exactEvaluator, "[policy_tree]")
	exRep := runReport(t, ex, 0)
	got, err := json.Marshal(exRep.Reports.PolicyTree)
	require.NoError(t, err)
	want, err := json.Marshal(trees)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

// TestExplain_GeneratedTreeAndHeatmap checks the tree round trip and the
// heatmap sums on a generated model with two layers of decisions.
func TestExplain_GeneratedTreeAndHeatmap(t *testing.T) {
	exp := generatedExperiment(t, genSpec{layers: 2, outcomes: 3, options: 2, seed: 11})
	exp.Reports = []report.Spec{{Kind: report.KindPolicyTree}, {Kind: report.KindPathHeatmap}}
	rep := runReport(t, exp, 4)
	ws := rep.Worlds[0].Solution
	tree := rep.Reports.PolicyTree[0]
	requireTreeRoundTrips(t, ws, tree)
	require.Len(t, tree.Roots, 3, "one d1 information set per outcome of c1")
	hm := rep.Reports.PathHeatmap[0]
	checkHeatmapShape(t, exp.Model, hm)
	require.Equal(t, 1, checkDecisionSums(t, exp.Model, hm, 1e-9), "d1; the last decision has no outgoing edge")
	for _, n := range hm.Nodes {
		require.InDelta(t, 1, n.Mass, 1e-12, n.ID)
	}
}

// TestExplain_RejectsNonDiscreteModel adds a continuous latent: voi,
// flip_point and policy_tree are rejected at compile time, once per kind;
// path_heatmap with Monte Carlo is fine.
func TestExplain_RejectsNonDiscreteModel(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "graph.yaml", readLoan(t, "graph.yaml"))
	s := strings.Replace(readLoan(t, "sim.yaml"), "  bad: {bernoulli: {p: \"world.p_bad\"}}\n",
		"  bad: {bernoulli: {p: \"world.p_bad\"}}\n  income: {normal: {mean: 50000, sd: 10000}}\n", 1)
	writeFile(t, dir, "sim.yaml", s)
	head := "apiVersion: experiment/v1\nmodel: ./sim.yaml\nworlds: [base, recession]\npolicies: {files: [recession.yaml]}\nruns: 50\nseed: 1\n"
	writeFile(t, dir, "recession.yaml", readLoan(t, recessionFile))
	_, err := LoadFile(writeFile(t, dir, "experiment.yaml", head+"reports: [voi:ai_score, path_heatmap, flip_point:p_bad, policy_tree, voi:screen]\n"), registry(t))
	var ve *engine.ValidationErrors
	require.ErrorAs(t, err, &ve)
	at := "$.model:$.latents.income.normal"
	require.Equal(t, []engine.ValidationIssue{
		{Path: at, Code: sim.CodeModelNotDiscrete, Message: "normal distribution is continuous; flip_point report requires a discrete model"},
		{Path: at, Code: sim.CodeModelNotDiscrete, Message: "normal distribution is continuous; policy_tree report requires a discrete model"},
		{Path: at, Code: sim.CodeModelNotDiscrete, Message: "normal distribution is continuous; voi report requires a discrete model"},
	}, ve.Issues())

	exp, err := LoadFile(writeFile(t, dir, "experiment.yaml", head+"reports: [path_heatmap]\n"), registry(t))
	require.NoError(t, err)
	rep := runReport(t, exp, 2)
	require.Len(t, rep.Reports.PathHeatmap, 2)
	checkDecisionSums(t, exp.Model, rep.Reports.PathHeatmap[0], 1e-9)
}

func TestCompile_ExplainReportsValidation(t *testing.T) {
	reg := registry(t)
	dir := t.TempDir()
	writeFile(t, dir, "graph.yaml", readLoan(t, "graph.yaml"))
	s := readLoan(t, "sim.yaml")
	withTier := strings.Replace(s, "base: {p_bad: 0.20, loss: 4000}", "base: {p_bad: 0.20, loss: 4000, tier: gold}", 1)
	require.NotEqual(t, s, withTier)
	writeFile(t, dir, "sim.yaml", withTier)
	writeFile(t, dir, "recession.yaml", readLoan(t, recessionFile))
	compile := func(worlds []string, reports ...string) (*CompiledExperiment, []engine.ValidationIssue) {
		def := &model.Experiment{
			APIVersion: "experiment/v1", Model: "./sim.yaml", Worlds: worlds,
			Policies: model.Policies{Files: []string{"recession.yaml"}}, Runs: 1, Seed: 1, Reports: reports,
		}
		exp, errs := Compile(def, dir, reg)
		if errs == nil {
			return exp, nil
		}
		return exp, stripped(t, errs.Issues())
	}
	both := []string{"base", "recession"}

	exp, issues := compile(both, "voi:ai_score", "voi:screen", "voi:human_review", "flip_point:p_bad", "flip_point:p_bad:0,1", "flip_point:loss", "path_heatmap", "policy_tree")
	require.Nil(t, issues)
	require.Equal(t, []report.Spec{
		{Kind: report.KindVOI, Node: "ai_score"},
		{Kind: report.KindVOI, Node: "screen"},
		{Kind: report.KindVOI, Node: "human_review"},
		{Kind: report.KindFlipPoint, Param: "p_bad", Lo: 0.2, Hi: 0.35},
		{Kind: report.KindFlipPoint, Param: "p_bad", HasRange: true, Lo: 0, Hi: 1},
		{Kind: report.KindFlipPoint, Param: "loss", Lo: 4000, Hi: 6000},
		{Kind: report.KindPathHeatmap},
		{Kind: report.KindPolicyTree},
	}, exp.Reports)

	// One world: a flip_point needs an explicit range.
	exp, issues = compile([]string{"recession"}, "flip_point:p_bad:0.1,0.5", "voi:ai_score")
	require.Nil(t, issues)
	require.Len(t, exp.Reports, 2)

	for _, tc := range []struct {
		name    string
		worlds  []string
		reports []string
		want    []engine.ValidationIssue
	}{
		{"unknown node", both, []string{"voi:ghost"}, []engine.ValidationIssue{{Path: "$.reports[0]", Code: engine.CodeUnresolvedReference}}},
		{"unobserved node", both, []string{"policy_tree", "voi:approve"}, []engine.ValidationIssue{{Path: "$.reports[1]", Code: engine.CodeInvalidValue}}},
		{"bad syntax", both, []string{"voi:", "flip_point:p_bad:0.3,0.1", "flip_point:p-bad"}, []engine.ValidationIssue{
			{Path: "$.reports[0]", Code: engine.CodeInvalidValue},
			{Path: "$.reports[1]", Code: engine.CodeInvalidValue},
			{Path: "$.reports[2]", Code: engine.CodeInvalidValue},
		}},
		{"missing param", both, []string{"flip_point:ghost"}, []engine.ValidationIssue{
			{Path: "$.reports[0]", Code: engine.CodeInvalidValue},
			{Path: "$.reports[0]", Code: engine.CodeInvalidValue},
		}},
		{"string param", both, []string{"flip_point:tier:0,1"}, []engine.ValidationIssue{
			{Path: "$.reports[0]", Code: engine.CodeInvalidValue},
			{Path: "$.reports[0]", Code: engine.CodeInvalidValue},
		}},
		{"one world, no range", []string{"base"}, []string{"flip_point:p_bad"}, []engine.ValidationIssue{{Path: "$.reports[0]", Code: engine.CodeInvalidValue}}},
		{"duplicates", both, []string{"voi:ai_score", "path_heatmap", "policy_tree", "voi:ai_score", "policy_tree", "path_heatmap", "flip_point:p_bad", "flip_point:p_bad", "flip_point:p_bad:0.2,0.35"}, []engine.ValidationIssue{
			{Path: "$.reports[3]", Code: engine.CodeDuplicateID},
			{Path: "$.reports[4]", Code: engine.CodeDuplicateID},
			{Path: "$.reports[5]", Code: engine.CodeDuplicateID},
			{Path: "$.reports[7]", Code: engine.CodeDuplicateID},
		}},
	} {
		_, issues := compile(tc.worlds, tc.reports...)
		require.Equal(t, tc.want, issues, tc.name)
	}

	// Messages name the problem.
	_, err := LoadFile(writeFile(t, dir, "experiment.yaml",
		"apiVersion: experiment/v1\nmodel: ./sim.yaml\nworlds: [base]\npolicies: {files: [recession.yaml]}\nruns: 1\nseed: 1\nreports: [voi:approve, voi:ghost, flip_point:p_bad, flip_point:tier]\n"), reg)
	var ve *engine.ValidationErrors
	require.ErrorAs(t, err, &ve)
	var msgs []string
	for _, is := range ve.Issues() {
		msgs = append(msgs, is.Path+": "+is.Message)
	}
	require.Equal(t, []string{
		`$.reports[0]: voi: node "approve" is not observed by any decision (no observes path starts with "approve.")`,
		`$.reports[1]: voi: unknown node "ghost"`,
		`$.reports[2]: flip_point: the sweep range needs lo < hi, but every experiment world has p_bad = 0.2; give a range: flip_point:p_bad:<lo>,<hi>`,
		`$.reports[3]: flip_point: param "tier" of world "base" is gold, not a number; the swept param must be a number in every experiment world`,
	}, msgs)
}

// TestExplain_WorkerCountIndependent runs every report kind under
// evaluator both and monte_carlo with 1 and 8 workers.
func TestExplain_WorkerCountIndependent(t *testing.T) {
	runs := 300
	if raceEnabled {
		runs = 100
	}
	all := "[regret, robust, 'pareto:cost:min,objective', voi:ai_score, voi:human_review, flip_point:p_bad, path_heatmap, policy_tree]"
	for _, evaluator := range []string{EvaluatorBoth, EvaluatorMonteCarlo} {
		exp := loanExplain(t, threeFiles, "evaluator: "+evaluator+"\nruns: "+itoa(runs)+"\n", all)
		r1 := runReport(t, exp, 1)
		r8 := runReport(t, exp, 8)
		js := marshal(t, r1)
		require.Equal(t, js, marshal(t, r8), evaluator)
		rs := r1.Reports
		require.NotNil(t, rs.Regret)
		require.NotNil(t, rs.Robust)
		require.Len(t, rs.Pareto, 1)
		require.Len(t, rs.VOI, 4)
		require.Len(t, rs.FlipPoints, 1)
		require.Len(t, rs.PathHeatmap, 2)
		require.Len(t, rs.PolicyTree, 2)
		for _, key := range []string{`"voi":[`, `"flipPoints":[`, `"pathHeatmap":[`, `"policyTree":[`} {
			require.Contains(t, js, key)
		}
		for _, v := range rs.VOI {
			require.GreaterOrEqual(t, v.VOI, -1e-9, "information is never worth less than nothing when it is free")
		}
	}

	// Without these reports nothing changes.
	exp := loanExplain(t, threeFiles, "evaluator: both\nruns: 50\n", "[regret]")
	js := marshal(t, runReport(t, exp, 2))
	for _, key := range []string{`"voi"`, `"flipPoints"`, `"pathHeatmap"`, `"policyTree"`} {
		require.NotContains(t, js, key)
	}
}

func TestExplain_Cancellation(t *testing.T) {
	exp := loanExplain(t, threeFiles, exactEvaluator, "[flip_point:p_bad]")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	x := &explainer{exp: exp}
	_, err := x.flipPoint(ctx, exp.Reports[0])
	require.ErrorIs(t, err, context.Canceled)
	_, _, err = exactTraffic(ctx, exp.Model, exp.Worlds[0], nil)
	require.ErrorIs(t, err, context.Canceled)
	set := &ReportSet{}
	err = buildExplainReports(ctx, set, x)
	require.ErrorIs(t, err, context.Canceled)
	_, _, err = monteCarloTraffic(ctx, withEvaluator(exp, EvaluatorMonteCarlo, 10), exp.Worlds[0], nil, 2)
	require.ErrorIs(t, err, context.Canceled)
}
