package report

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

func TestParseSpec(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Spec
	}{
		{"regret", Spec{Kind: KindRegret}},
		{"robust", Spec{Kind: KindRobust}},
		{"pareto:cost,objective", Spec{Kind: KindPareto, X: Axis{"cost", GoalMax}, Y: Axis{"objective", GoalMax}}},
		{"pareto:cost:min,objective:max", Spec{Kind: KindPareto, X: Axis{"cost", GoalMin}, Y: Axis{"objective", GoalMax}}},
		{"pareto: wait_hours:min , approval ", Spec{Kind: KindPareto, X: Axis{"wait_hours", GoalMin}, Y: Axis{"approval", GoalMax}}},
	} {
		got, err := ParseSpec(tc.in)
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.want, got, tc.in)
	}
	require.Equal(t, 2, Spec{Kind: KindRegret}.MinWorlds())
	require.Equal(t, 2, Spec{Kind: KindRobust}.MinWorlds())
	require.Equal(t, 1, Spec{Kind: KindPareto}.MinWorlds())
	require.Equal(t, []string{"cost", "objective"}, Spec{Kind: KindPareto, X: Axis{Metric: "cost"}, Y: Axis{Metric: "objective"}}.Metrics())
	require.Nil(t, Spec{Kind: KindRegret}.Metrics())

	for in, msg := range map[string]string{
		"":                         `unknown report ""`,
		"voi":                      `unknown report "voi"`,
		"Regret":                   `unknown report "Regret"`,
		"regret:x":                 `report "regret" takes no arguments`,
		"robust:":                  `report "robust" takes no arguments`,
		"pareto":                   "pareto needs two metrics",
		"pareto:cost":              "pareto needs exactly two metrics, got 1",
		"pareto:a,b,c":             "pareto needs exactly two metrics, got 3",
		"pareto:cost,cost:min":     `pareto needs two different metrics, got "cost" twice`,
		"pareto:Cost,objective":    `pareto metric "Cost" must match`,
		"pareto:,objective":        `pareto metric "" must match`,
		"pareto:cost:best,profit":  `pareto goal "best" of metric "cost" must be min or max`,
		"pareto:cost,profit:max:x": `pareto goal "max:x" of metric "profit" must be min or max`,
	} {
		_, err := ParseSpec(in)
		require.ErrorContains(t, err, msg, in)
	}
}

func TestCVaR_HandBuilt(t *testing.T) {
	atoms := []Atom{{Value: 10, Weight: 0.48}, {Value: -10, Weight: 0.02}, {Value: 0, Weight: 0.5}}
	// Worst 5%: all of -10 (0.02) and 0.03 of the atom at 0, a fractional
	// atom at the boundary: (-10*0.02 + 0*0.03) / 0.05.
	require.InDelta(t, -4.0, CVaR(atoms, 0.05), 1e-12)
	// The boundary falls exactly at the end of the first atom.
	require.InDelta(t, -10.0, CVaR(atoms, 0.02), 1e-12)
	// Half of the 10 atom is needed at alpha 0.76: (-0.2 + 0 + 10*0.24)/0.76.
	require.InDelta(t, 2.2/0.76, CVaR(atoms, 0.76), 1e-12)
	// alpha 1 is the mean.
	require.InDelta(t, 4.6, CVaR(atoms, 1), 1e-12)
	// A truncated lower tail gives the same CVaR as the full distribution.
	require.InDelta(t, -4.0, CVaR(atoms[1:], 0.05), 1e-12)
	// Less mass than alpha (rounding at alpha 1): the weighted mean.
	require.InDelta(t, 5.0, CVaR([]Atom{{Value: 5, Weight: 0.999999999}}, 1), 1e-12)
	require.True(t, math.IsNaN(CVaR(nil, 0.05)))

	// A fractional atom inside a single-atom distribution.
	require.InDelta(t, 7.0, CVaR([]Atom{{Value: 7, Weight: 1}}, 0.05), 1e-12)
	// Equal values in separate atoms behave like one atom.
	require.InDelta(t, (1*0.1+2*0.05)/0.15, CVaR([]Atom{{2, 0.3}, {1, 0.05}, {1, 0.05}, {2, 0.6}}, 0.15), 1e-12)
}

func TestTailCover(t *testing.T) {
	require.Equal(t, 0.0, TailCover(0, 0.05))
	require.InDelta(t, 0.1, TailCover(0.5, 0.05), 1e-15)
	require.Equal(t, 1.0, TailCover(0.01, 0.05))
	require.Equal(t, 1.0, TailCover(1, 1))
}

func ids(points []Point) []string {
	out := make([]string, len(points))
	for i, p := range points {
		out[i] = p.PolicyID
	}
	return out
}

func TestFrontier(t *testing.T) {
	pts := []Point{
		{"a", 1, 1}, // dominated by c (x better, y equal) under max/max
		{"b", 3, 1},
		{"c", 2, 1},
		{"d", 1, 5},
		{"e", 2, 4},
		{"f", 0, 0},
	}
	// max x, max y: b(3,1), e(2,4), d(1,5); c is dominated by b (equal y,
	// worse x) and by e; a and f are dominated.
	require.Equal(t, []string{"b", "e", "d"}, ids(Frontier(pts, GoalMax, GoalMax)))
	// min x, max y: f(0,0) and d(1,5); a(1,1) is dominated by d (equal x).
	require.Equal(t, []string{"f", "d"}, ids(Frontier(pts, GoalMin, GoalMax)))
	// min x, min y: f dominates everything.
	require.Equal(t, []string{"f"}, ids(Frontier(pts, GoalMin, GoalMin)))
	// max x, min y: b(3,1) then f(0,0); c and a tie b on y with worse x.
	require.Equal(t, []string{"b", "f"}, ids(Frontier(pts, GoalMax, GoalMin)))

	// Equal points are all kept, ordered by policy id.
	eq := []Point{{"z", 1, 1}, {"y", 1, 1}, {"x", 0, 2}, {"w", 0, 0}}
	require.Equal(t, []string{"y", "z", "x"}, ids(Frontier(eq, GoalMax, GoalMax)))
	require.Equal(t, []Point{{"y", 1, 1}, {"z", 1, 1}, {"x", 0, 2}}, Frontier(eq, GoalMax, GoalMax))

	// A single point is the frontier; no points give an empty, non-nil one.
	require.Equal(t, []Point{{"only", 3, -2}}, Frontier([]Point{{"only", 3, -2}}, GoalMin, GoalMin))
	require.Equal(t, []Point{}, Frontier(nil, GoalMax, GoalMax))

	// All points on the frontier (a strict trade-off), sorted by x in goal
	// order.
	line := []Point{{"p1", 1, 4}, {"p3", 3, 2}, {"p2", 2, 3}, {"p4", 4, 1}}
	require.Equal(t, []string{"p4", "p3", "p2", "p1"}, ids(Frontier(line, GoalMax, GoalMax)))
	require.Equal(t, []string{"p1", "p2", "p3", "p4"}, ids(Frontier(line, GoalMin, GoalMin)))

	// Equal x: only the best y of the group survives.
	sameX := []Point{{"lo", 1, 1}, {"hi", 1, 2}}
	require.Equal(t, []string{"hi"}, ids(Frontier(sameX, GoalMax, GoalMax)))
	require.Equal(t, []string{"lo"}, ids(Frontier(sameX, GoalMax, GoalMin)))
}

// table builds a 2-world table from per-policy means.
func table(policies []string, mean [][]float64, feasible [][]bool) Table {
	if feasible == nil {
		feasible = make([][]bool, len(policies))
		for p := range policies {
			feasible[p] = []bool{true, true}
		}
	}
	return Table{Worlds: []sim.WorldID{"base", "recession"}, Policies: policies, Mean: mean, Feasible: feasible}
}

func TestNewPareto_ExcludesInfeasible(t *testing.T) {
	tb := table([]string{"a", "b", "c"}, [][]float64{{0, 0}, {0, 0}, {0, 0}}, [][]bool{{true, true}, {false, true}, {true, true}})
	tb.Metrics = map[string][][]float64{
		"cost":   {{1, 1}, {0, 0}, {2, 2}},
		"profit": {{5, 5}, {9, 9}, {6, 6}},
	}
	p, err := NewPareto(tb, Axis{"cost", GoalMin}, Axis{"profit", GoalMax})
	require.NoError(t, err)
	// base: b (0, 9) dominates everything but is infeasible.
	require.Equal(t, []Point{{"a", 1, 5}, {"c", 2, 6}}, p.Worlds[0].Points)
	require.Equal(t, []Point{{"b", 0, 9}}, p.Worlds[1].Points)
	require.Equal(t, sim.WorldID("recession"), p.Worlds[1].World)

	// Every policy infeasible: an empty frontier, serialized as [].
	tb.Feasible = [][]bool{{false, false}, {false, false}, {false, false}}
	p, err = NewPareto(tb, Axis{"cost", GoalMin}, Axis{"profit", GoalMax})
	require.NoError(t, err)
	data, err := json.Marshal(p)
	require.NoError(t, err)
	require.JSONEq(t, `{"x":{"metric":"cost","goal":"min"},"y":{"metric":"profit","goal":"max"},
		"worlds":[{"world":"base","points":[]},{"world":"recession","points":[]}]}`, string(data))

	_, err = NewPareto(tb, Axis{"cost", GoalMin}, Axis{"missing", GoalMax})
	require.ErrorContains(t, err, "missing")
}

func TestNewRegret(t *testing.T) {
	// a is best in base, b in recession; c ties b in recession but has a
	// higher id; d has the highest recession mean but is infeasible there.
	tb := table([]string{"d", "c", "b", "a"},
		[][]float64{{0, 300}, {10, 50}, {40, 50}, {100, -20}},
		[][]bool{{true, false}, {true, true}, {true, true}, {true, true}})
	r, err := NewRegret(tb)
	require.NoError(t, err)
	f := func(x float64) *float64 { return &x }
	require.Equal(t, &Regret{
		Worlds: []sim.WorldID{"base", "recession"},
		Rows: []RegretRow{
			{TunedFor: "base", PolicyID: "a", MaxRegret: 70, Cells: []RegretCell{
				{World: "base", PolicyID: "a", Value: 100, Regret: f(0), Feasible: true},
				{World: "recession", PolicyID: "a", Value: -20, Regret: f(70), Feasible: true},
			}},
			{TunedFor: "recession", PolicyID: "b", MaxRegret: 60, Cells: []RegretCell{
				{World: "base", PolicyID: "b", Value: 40, Regret: f(60), Feasible: true},
				{World: "recession", PolicyID: "b", Value: 50, Regret: f(0), Feasible: true},
			}},
		},
	}, r)

	// No feasible policy in recession: no row, no regret in its column.
	tb.Feasible = [][]bool{{true, false}, {true, false}, {true, false}, {true, false}}
	r, err = NewRegret(tb)
	require.NoError(t, err)
	require.Equal(t, []sim.WorldID{"recession"}, r.NoFeasiblePolicy)
	require.Len(t, r.Rows, 1)
	require.Nil(t, r.Rows[0].Cells[1].Regret)
	require.False(t, r.Rows[0].Cells[1].Feasible)
	require.Equal(t, 0.0, r.Rows[0].MaxRegret)
	data, err := json.Marshal(r.Rows[0].Cells[1])
	require.NoError(t, err)
	require.JSONEq(t, `{"world":"recession","policyId":"a","value":-20,"feasible":false}`, string(data))

	_, err = NewRegret(Table{})
	require.Error(t, err)
	_, err = NewRegret(table([]string{"a"}, [][]float64{{1}}, [][]bool{{true}}))
	require.Error(t, err)
}

func TestNewRobust(t *testing.T) {
	// Three policies in two worlds, prior 0.7/0.3, alpha 0.1.
	//   safe:  base 50 for sure; recession 40 for sure.
	//   bold:  base 100 for sure; recession 90% 0, 10% -100 (mean -10).
	//   wild:  base 90% 200, 10% -500 (mean 130); recession 30 for sure;
	//          infeasible in recession, so never picked.
	tb := table([]string{"wild", "bold", "safe"},
		[][]float64{{130, 30}, {100, -10}, {50, 40}},
		[][]bool{{true, false}, {true, true}, {true, true}})
	tb.Tails = [][][]Atom{
		{{{-500, 0.1}, {200, 0.9}}, {{30, 1}}},
		{{{100, 1}}, {{-100, 0.1}, {0, 0.9}}},
		{{{50, 1}}, {{40, 1}}},
	}
	r, err := NewRobust(tb, []float64{0.7, 0.3}, 0.1)
	require.NoError(t, err)
	require.Equal(t, map[sim.WorldID]float64{"base": 0.7, "recession": 0.3}, r.Prior)
	require.Equal(t, 0.1, r.CVaRAlpha)
	require.Equal(t, 2, r.Eligible)

	// bold: EV 0.7*100 + 0.3*-10 = 67; worst -10; the mixture's worst 10%
	// is -100 (mass 0.03) then 0 (mass 0.07): CVaR -3/0.1 = -30.
	// safe: EV 0.7*50 + 0.3*40 = 47; worst 40; CVaR 40.
	bold := RobustScores{ExpectedValue: 67, WorstCase: -10, CVaR: -30}
	safe := RobustScores{ExpectedValue: 47, WorstCase: 40, CVaR: 40}
	requireScores := func(want, got RobustScores) {
		t.Helper()
		require.InDelta(t, want.ExpectedValue, got.ExpectedValue, 1e-9)
		require.InDelta(t, want.WorstCase, got.WorstCase, 1e-9)
		require.InDelta(t, want.CVaR, got.CVaR, 1e-9)
	}
	require.Equal(t, "bold", r.ExpectedValue.PolicyID)
	require.InDelta(t, 67, r.ExpectedValue.Score, 1e-9)
	requireScores(bold, r.ExpectedValue.Scores)
	require.Equal(t, "safe", r.Maximin.PolicyID)
	require.InDelta(t, 40, r.Maximin.Score, 1e-9)
	requireScores(safe, r.Maximin.Scores)
	require.Equal(t, "safe", r.CVaR.PolicyID)
	require.InDelta(t, 40, r.CVaR.Score, 1e-9)

	// The prior weights the mixture: with recession at 0.05 the -100 atom
	// has mass 0.005, so bold's CVaR(0.1) is (-0.5 + 0*0.045 + 100*0.05)/0.1
	// = 45 > 40 and its EV 0.95*100 - 0.5 = 94.5.
	r, err = NewRobust(tb, []float64{0.95, 0.05}, 0.1)
	require.NoError(t, err)
	require.Equal(t, "bold", r.CVaR.PolicyID)
	require.InDelta(t, 45, r.CVaR.Score, 1e-9)
	require.InDelta(t, 94.5, r.ExpectedValue.Score, 1e-9)
	require.Equal(t, "safe", r.Maximin.PolicyID, "maximin ignores the prior")

	// A world with prior 0 adds nothing to EV or CVaR but still counts for
	// maximin.
	r, err = NewRobust(tb, []float64{1, 0}, 0.1)
	require.NoError(t, err)
	require.InDelta(t, 100, r.CVaR.Scores.CVaR, 1e-9)
	require.InDelta(t, -10, r.CVaR.Scores.WorstCase, 1e-9)

	// Ties go to the lower policy id.
	tie := table([]string{"b", "a"}, [][]float64{{1, 1}, {1, 1}}, nil)
	tie.Tails = [][][]Atom{{{{1, 1}}, {{1, 1}}}, {{{1, 1}}, {{1, 1}}}}
	r, err = NewRobust(tie, []float64{0.5, 0.5}, 0.05)
	require.NoError(t, err)
	require.Equal(t, "a", r.ExpectedValue.PolicyID)
	require.Equal(t, "a", r.Maximin.PolicyID)
	require.Equal(t, "a", r.CVaR.PolicyID)

	// No eligible policy: no picks.
	tb.Feasible = [][]bool{{true, false}, {false, true}, {true, false}}
	r, err = NewRobust(tb, []float64{0.7, 0.3}, 0.1)
	require.NoError(t, err)
	require.Zero(t, r.Eligible)
	require.Nil(t, r.ExpectedValue)
	require.Nil(t, r.Maximin)
	require.Nil(t, r.CVaR)

	_, err = NewRobust(tb, []float64{1}, 0.1)
	require.Error(t, err)
	_, err = NewRobust(tb, []float64{0.5, 0.5}, 0)
	require.Error(t, err)
}
