package model

import (
	"os"
	"regexp"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

func TestParseRequirement(t *testing.T) {
	for in, want := range map[string]Requirement{
		"mean(objective) >= 100":               {Agg: AggMean, Arg: "objective", Op: OpGE, Bound: 100},
		"p50(cost)<=20":                        {Agg: AggP50, Arg: "cost", Op: OpLE, Bound: 20},
		"  p95( duration ) <  duration('72h')": {Agg: AggP95, Arg: "duration", Op: OpLT, Bound: 72 * 3600},
		`p95(duration) <= duration("1h30m")`:   {Agg: AggP95, Arg: "duration", Op: OpLE, Bound: 5400},
		"mean(approved) > -0.5e-1":             {Agg: AggMean, Arg: "approved", Op: OpGT, Bound: -0.05},
		"mean(x) == 0":                         {Agg: AggMean, Arg: "x", Op: OpEQ, Bound: 0},
		"prob(run.abandoned) <= 0.1":           {Agg: AggProb, Arg: "run.abandoned", Op: OpLE, Bound: 0.1},
		"prob((run.cost > 10.0) && nodes.approve.status == 'Succeeded') >= 0.25": {
			Agg: AggProb, Arg: "(run.cost > 10.0) && nodes.approve.status == 'Succeeded'", Op: OpGE, Bound: 0.25,
		},
		// Parentheses and quotes inside string literals do not end the argument.
		`prob(nodes.a.errorMessage == ")'(" || nodes.b.errorMessage == 'it\'s )') < 1`: {
			Agg: AggProb, Arg: `nodes.a.errorMessage == ")'(" || nodes.b.errorMessage == 'it\'s )'`, Op: OpLT, Bound: 1,
		},
	} {
		got, err := ParseRequirement(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}

	for in, msg := range map[string]string{
		"":                                "requirement must not be empty",
		"   ":                             "requirement must not be empty",
		">= 3":                            "requirement must start with mean, p50, p95 or prob",
		"max(objective) >= 1":             `unknown aggregate "max"`,
		"Mean(objective) >= 1":            "requirement must start with mean, p50, p95 or prob",
		"mean objective >= 1":             "expected ( after mean",
		"mean(objective >= 1":             "unbalanced parentheses",
		"prob(nodes.a.status == 'x) > 0":  "unterminated string literal",
		"mean() >= 1":                     "mean() needs an argument",
		"prob(  ) >= 1":                   "prob() needs an argument",
		"mean(run.cost) >= 1":             "mean(run.cost): argument must be a metric name",
		"p95(Duration) >= 1":              "p95(Duration): argument must be a metric name",
		"mean(objective)":                 "missing comparison operator",
		"mean(objective) != 1":            `unknown comparison operator at "!= 1"`,
		"mean(objective) = 1":             `unknown comparison operator at "= 1"`,
		"mean(objective) >=":              "missing right-hand side",
		"mean(objective) >= abc":          `invalid bound "abc"`,
		"mean(objective) >= 1 2":          `invalid bound "1 2"`,
		"mean(objective) >= NaN":          `invalid bound "NaN"`,
		"mean(objective) >= +Inf":         `invalid bound "+Inf"`,
		"mean(objective) >= 1) ":          `invalid bound "1)"`,
		"p95(duration) <= duration(72h)":  `want duration('<Go duration>')`,
		"p95(duration) <= duration('72h'": `want duration('<Go duration>')`,
		"p95(duration) <= duration('3d')": `invalid bound "duration('3d')"`,
		`p95(duration) <= duration('1h")`: `want duration('<Go duration>')`,
	} {
		_, err := ParseRequirement(in)
		require.ErrorContains(t, err, msg, in)
	}
}

func TestOpHolds(t *testing.T) {
	require.True(t, OpLE.Holds(1, 1))
	require.False(t, OpLT.Holds(1, 1))
	require.True(t, OpGE.Holds(1, 1))
	require.False(t, OpGT.Holds(1, 1))
	require.True(t, OpEQ.Holds(1, 1))
	require.False(t, OpEQ.Holds(1, 1.5))
	require.True(t, OpLT.Holds(-1, 0))
	require.True(t, OpGT.Holds(2, 1))
	require.False(t, Op("!=").Holds(1, 2))
}

const loanObjectiveLine = "  maximize: mean\n"

func TestCompile_MetricsAndRequirements(t *testing.T) {
	m, errs := compileLoanVariant(t, nil, []string{loanObjectiveLine, loanObjectiveLine +
		"  requirements:\n" +
		"    profit_floor: \"mean(objective) >= -100\"\n" +
		"    fast: \"p95(duration) <= duration('72h')\"\n" +
		"    approvals: \"prob(nodes.approve.status == 'Succeeded') >= 0.5\"\n" +
		"    cheap: \"p50(cost) < 200\"\n" +
		"    reviewed: \"mean(reviewed) <= 1\"\n" +
		"metrics:\n" +
		"  reviewed: \"nodes.human_review.status == 'Succeeded' ? 1.0 : 0.0\"\n" +
		"  approvals: \"nodes.approve.status == 'Succeeded' ? 1 : 0\"\n" +
		"  wait_hours: \"run.duration / 3600.0\"\n"})
	require.Nil(t, errs)
	require.Equal(t, []string{"approvals", "reviewed", "wait_hours"}, m.MetricOrder)
	require.Len(t, m.CompiledMetrics, 3)
	require.True(t, m.HasMetric("wait_hours"))
	require.True(t, m.HasMetric("cost"))
	require.False(t, m.HasMetric("nope"))

	names := make([]string, len(m.Requirements))
	for i, r := range m.Requirements {
		names[i] = r.Name
	}
	require.Equal(t, []string{"approvals", "cheap", "fast", "profit_floor", "reviewed"}, names)
	require.NotNil(t, m.Requirements[0].Event, "prob requirements compile their condition")
	require.Equal(t, AggProb, m.Requirements[0].Agg)
	require.Nil(t, m.Requirements[2].Event)
	require.Equal(t, 72*3600.0, m.Requirements[2].Bound)
	require.Equal(t, "p95(duration) <= duration('72h')", m.Requirements[2].Source)

	// The loan model alone has none.
	plain := loanModel(t)
	require.Empty(t, plain.MetricOrder)
	require.Empty(t, plain.Requirements)
}

func TestCompile_MetricErrors(t *testing.T) {
	_, errs := compileLoanVariant(t, nil, []string{loanObjectiveLine, loanObjectiveLine +
		"metrics:\n" +
		"  cost: \"run.cost\"\n" +
		"  objective: \"1.0\"\n" +
		"  Bad-Name: \"1.0\"\n" +
		"  label: \"'high'\"\n" +
		"  ghost: \"foo.bar + 1.0\"\n" +
		"  flag: \"run.abandoned == true\"\n" +
		"  empty: \"\"\n" +
		"  ok: \"run.cost * 2.0\"\n"})
	require.NotNil(t, errs)
	codes := map[string]engine.ErrorCode{}
	for _, is := range errs.Issues() {
		codes[is.Path] = is.Code
	}
	require.Equal(t, map[string]engine.ErrorCode{
		"$.metrics.cost":        engine.CodeInvalidValue,
		"$.metrics.objective":   engine.CodeInvalidValue,
		"$.metrics['Bad-Name']": engine.CodeInvalidValue,
		"$.metrics.label":       sim.CodeInvalidExpression,
		"$.metrics.ghost":       sim.CodeInvalidExpression,
		"$.metrics.flag":        sim.CodeInvalidExpression,
		"$.metrics.empty":       sim.CodeInvalidExpression,
	}, codes)
	issues := errs.Issues()
	for i := 1; i < len(issues); i++ {
		require.LessOrEqual(t, issues[i-1].Path, issues[i].Path, "issues are sorted")
	}
	require.Contains(t, issues[len(issues)-1].Message, "Bad-Name")
}

func TestCompile_RequirementErrors(t *testing.T) {
	_, errs := compileLoanVariant(t, nil, []string{loanObjectiveLine, loanObjectiveLine +
		"  requirements:\n" +
		"    syntax: \"mean(objective) => 1\"\n" +
		"    unknown_metric: \"mean(approvals) >= 0.5\"\n" +
		"    not_bool: \"prob(1.0 + run.cost) >= 0.5\"\n" +
		"    bad_ref: \"prob(nodes.missing.status == 'x' && foo) >= 0.5\"\n" +
		"    duration_bound: \"p95(duration) <= duration('2 days')\"\n" +
		"    ok: \"mean(cost) <= 100\"\n"})
	require.NotNil(t, errs)
	require.Equal(t, []engine.ValidationIssue{
		{Path: "$.objective.requirements.bad_ref", Code: sim.CodeInvalidExpression},
		{Path: "$.objective.requirements.duration_bound", Code: sim.CodeInvalidExpression},
		{Path: "$.objective.requirements.not_bool", Code: sim.CodeInvalidExpression},
		{Path: "$.objective.requirements.syntax", Code: sim.CodeInvalidExpression},
		{Path: "$.objective.requirements.unknown_metric", Code: engine.CodeUnresolvedReference},
	}, stripMessages(errs.Issues()))
	for _, is := range errs.Issues() {
		require.NotEmpty(t, is.Message)
	}
}

func stripMessages(issues []engine.ValidationIssue) []engine.ValidationIssue {
	out := make([]engine.ValidationIssue, len(issues))
	for i, is := range issues {
		out[i] = engine.ValidationIssue{Path: is.Path, Code: is.Code}
	}
	return out
}

func TestParseSimModel_MetricsBinding(t *testing.T) {
	base := readLoan(t, "sim.yaml")
	for _, tc := range []struct {
		name, repl string
		want       []engine.ValidationIssue
	}{
		{"metrics not object", "metrics: [a]\n", []engine.ValidationIssue{{Path: "$.metrics", Code: engine.CodeInvalidType}}},
		{"metric not string", "metrics: {a: 1}\n", []engine.ValidationIssue{{Path: "$.metrics.a", Code: engine.CodeInvalidType}}},
		{"requirements not object", "", []engine.ValidationIssue{{Path: "$.objective.requirements", Code: engine.CodeInvalidType}}},
	} {
		doc := base + tc.repl
		if tc.repl == "" {
			doc = base + "  requirements: \"mean(objective) > 0\"\n"
		}
		_, err := ParseSimModel([]byte(doc), engine.FormatYAML)
		var ve *engine.ValidationErrors
		require.ErrorAs(t, err, &ve, tc.name)
		require.Equal(t, tc.want, stripMessages(ve.Issues()), tc.name)
	}

	def, err := ParseSimModel([]byte(base+"  requirements: {floor: \"mean(objective) > 0\"}\nmetrics: {wait: \"run.duration\"}\n"), engine.FormatYAML)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"floor": "mean(objective) > 0"}, def.Objective.Requirements)
	require.Equal(t, map[string]string{"wait": "run.duration"}, def.Metrics)

	// Unknown fields are still rejected next to the new ones.
	_, err = ParseSimModel([]byte(base+"  requirement: {}\nmetric: {}\n"), engine.FormatYAML)
	var ve *engine.ValidationErrors
	require.ErrorAs(t, err, &ve)
	require.Equal(t, []engine.ValidationIssue{
		{Path: "$.metric", Code: engine.CodeUnknownField},
		{Path: "$.objective.requirement", Code: engine.CodeUnknownField},
	}, stripMessages(ve.Issues()))
}

func TestParseExperiment_ReportsBinding(t *testing.T) {
	doc := func(extra string) []byte {
		return []byte("apiVersion: experiment/v1\nmodel: ./sim.yaml\npolicies: {search: exhaustive}\nruns: 1\nseed: 1\n" + extra)
	}
	exp, err := ParseExperiment(doc("reports: [regret, robust, 'pareto:cost:min,objective']\nworld_prior: {base: 0.7, recession: 0.3}\ncvar_alpha: 0.1\n"), engine.FormatYAML)
	require.NoError(t, err)
	require.Equal(t, []string{"regret", "robust", "pareto:cost:min,objective"}, exp.Reports)
	require.Equal(t, map[string]float64{"base": 0.7, "recession": 0.3}, exp.WorldPrior)
	require.Equal(t, 0.1, exp.CVaRAlpha)

	exp, err = ParseExperiment(doc("cvar_alpha: 1\n"), engine.FormatYAML)
	require.NoError(t, err)
	require.Equal(t, 1.0, exp.CVaRAlpha)
	exp, err = ParseExperiment(doc(""), engine.FormatYAML)
	require.NoError(t, err)
	require.Zero(t, exp.CVaRAlpha)
	require.Nil(t, exp.Reports)
	require.Nil(t, exp.WorldPrior)

	for extra, want := range map[string][]engine.ValidationIssue{
		"reports: regret\n":                 {{Path: "$.reports", Code: engine.CodeInvalidType}},
		"reports: [regret, 3]\n":            {{Path: "$.reports[1]", Code: engine.CodeInvalidType}},
		"world_prior: [0.5]\n":              {{Path: "$.world_prior", Code: engine.CodeInvalidType}},
		"world_prior: {base: high}\n":       {{Path: "$.world_prior.base", Code: engine.CodeInvalidType}},
		"cvar_alpha: x\n":                   {{Path: "$.cvar_alpha", Code: engine.CodeInvalidType}},
		"cvar_alpha: 0\n":                   {{Path: "$.cvar_alpha", Code: engine.CodeInvalidValue}},
		"cvar_alpha: -0.1\n":                {{Path: "$.cvar_alpha", Code: engine.CodeInvalidValue}},
		"cvar_alpha: 1.5\n":                 {{Path: "$.cvar_alpha", Code: engine.CodeInvalidValue}},
		"report: [regret]\ncvar_alpha: 2\n": {{Path: "$.cvar_alpha", Code: engine.CodeInvalidValue}, {Path: "$.report", Code: engine.CodeUnknownField}},
	} {
		_, err := ParseExperiment(doc(extra), engine.FormatYAML)
		var ve *engine.ValidationErrors
		require.ErrorAs(t, err, &ve, extra)
		require.Equal(t, want, stripMessages(ve.Issues()), extra)
	}
}

// TestSchema_NewFieldsParse checks that the schema examples using metrics,
// requirements, reports, world_prior and cvar_alpha parse, and that the
// schema's requirement pattern accepts what ParseRequirement accepts.
func TestSchema_NewFieldsParse(t *testing.T) {
	data, err := os.ReadFile("../../../schemas/sim/v1/example.yaml")
	require.NoError(t, err)
	def, err := ParseSimModel(data, engine.FormatYAML)
	require.NoError(t, err)
	require.NotEmpty(t, def.Metrics)
	for name, req := range def.Objective.Requirements {
		_, err := ParseRequirement(req)
		require.NoError(t, err, name)
	}
	data, err = os.ReadFile("../../../schemas/experiment/v1/example.yaml")
	require.NoError(t, err)
	exp, err := ParseExperiment(data, engine.FormatYAML)
	require.NoError(t, err)
	require.NotEmpty(t, exp.Reports)

	schema := readSchemaJSON(t, "../../../schemas/sim/v1/schema.json")
	reqs := schema["properties"].(map[string]any)["objective"].(map[string]any)["properties"].(map[string]any)["requirements"].(map[string]any)
	pattern := reqs["patternProperties"].(map[string]any)["^[a-zA-Z0-9_-]+$"].(map[string]any)["pattern"].(string)
	re := regexp.MustCompile(pattern)
	for _, s := range []string{
		"mean(objective) >= 100", "p50(cost)<=20", "  p95( duration ) <  duration('72h')",
		"prob(run.abandoned) <= 0.1", "prob((run.cost > 10.0) && x == 'y') >= 0.25",
	} {
		_, err := ParseRequirement(s)
		require.NoError(t, err, s)
		require.Regexp(t, re, s)
	}
	for _, s := range []string{"max(objective) >= 1", "mean(objective)", "mean(run.cost) >= 1", "mean(objective) != 1"} {
		_, err := ParseRequirement(s)
		require.Error(t, err, s)
		require.NotRegexp(t, re, s)
	}
}
