package model

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/require"
)

// paramsGraph has one effectful node feeding a decision.
const paramsGraph = `
apiVersion: graph/v1
id: params
nodes:
  - id: svc
    type: http_call
    config: {url: "http://test.example", method: GET}
  - id: pick
    type: decision
    config: {options: [a, b], observes: [svc.output.band]}
edges:
  - {from: svc, to: pick}
`

func compileInline(t *testing.T, graphYAML, simYAML string) (*CompiledModel, *engine.ValidationErrors) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphYAML), 0o600))
	def, err := ParseSimModel([]byte(simYAML), engine.FormatYAML)
	require.NoError(t, err)
	return Compile(def, dir, setupRegistry())
}

func simWith(latents, behavior string) string {
	return `
apiVersion: sim/v1
id: params
graph: graph.yaml
worlds:
  base: {mu: 1.0, sd: 0.5, lo: 0.0, hi: 2.0, p: 0.3}
latents:
` + latents + `
behaviors:
  svc:
` + behavior + `
objective:
  name: v
  value: "-run.cost"
  maximize: mean
`
}

func evalParam(t *testing.T, p CompiledParam, vars map[string]any) float64 {
	t.Helper()
	if p.IsLiteral() {
		return p.Literal
	}
	out, _, err := p.Program.Eval(vars)
	require.NoError(t, err)
	switch v := out.Value().(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	}
	t.Fatalf("unexpected param value %T", out.Value())
	return 0
}

func requireProgram(t *testing.T, p CompiledParam, name string) {
	t.Helper()
	require.False(t, p.IsLiteral(), "%s must be compiled to a program", name)
	require.NotNil(t, p.Program, name)
	require.NotEmpty(t, p.Source, name)
}

func TestCompile_EveryParamKindCompilesToProgram(t *testing.T) {
	latents := `
  b: {bernoulli: {p: "world.p"}}
  n: {normal: {mean: "world.mu", sd: "world.sd"}}
  ln: {lognormal: {mu: "world.mu", sigma: "world.sd"}}
  u: {uniform: {min: "world.lo", max: "world.hi"}}
  tr: {triangular: {min: "world.lo", mode: "world.mu", max: "world.hi"}}
  c: {choice: {x: "world.p", y: "1.0 - world.p"}}
  e: {expr: "world.mu * 2.0"}
`
	behavior := `
    cost: "latent.n * 10.0 + double(inputs.size)"
    duration: {normal: {mean: "latent.n", sd: "world.sd"}}
    abandon: {p: "latent.b ? 0.0 : 0.3"}
    fail: {p: "world.p / 2.0", class: retryable}
    output:
      band: {choice: {low: "latent.b ? 0.3 : 0.9", high: "latent.b ? 0.7 : 0.1"}}
      score: {uniform: {min: "double(inputs.size)", max: "latent.u + 1.0"}}
      tri: {triangular: {min: "0.0", mode: "inputs.mode", max: "10.0"}}
      flag: {bernoulli: {p: "inputs.p"}}
      ln: {lognormal: {mu: "latent.ln", sigma: "0.1"}}
      x: {expr: "inputs.size * 2"}
`
	m, errs := compileInline(t, paramsGraph, simWith(latents, behavior))
	require.Nil(t, errs)
	require.NotNil(t, m)

	require.Equal(t, []string{"b", "c", "e", "ln", "n", "tr", "u"}, m.LatentOrder)

	wantParams := map[string][]string{
		"b":  {ParamP},
		"n":  {ParamMean, ParamSD},
		"ln": {ParamMu, ParamSigma},
		"u":  {ParamMin, ParamMax},
		"tr": {ParamMin, ParamMode, ParamMax},
	}
	wantKind := map[string]ValueKind{
		"b": KindBernoulli, "n": KindNormal, "ln": KindLognormal, "u": KindUniform,
		"tr": KindTriangular, "c": KindChoice, "e": KindExpr,
	}
	for name, kind := range wantKind {
		require.Equal(t, kind, m.CompiledLatents[name].Kind, name)
	}
	for name, params := range wantParams {
		cv := m.CompiledLatents[name]
		require.Len(t, cv.Params, len(params), name)
		for _, p := range params {
			requireProgram(t, cv.Params[p], name+"."+p)
		}
	}
	c := m.CompiledLatents["c"]
	require.Len(t, c.Choice, 2)
	require.Equal(t, "x", c.Choice[0].Value)
	require.Equal(t, "y", c.Choice[1].Value)
	requireProgram(t, c.Choice[0].Weight, "c.x")
	requireProgram(t, c.Choice[1].Weight, "c.y")
	require.NotNil(t, m.CompiledLatents["e"].Expr)

	world := map[string]any{"mu": 1.0, "sd": 0.5, "lo": 0.0, "hi": 2.0, "p": 0.3}
	require.InDelta(t, 0.7, evalParam(t, c.Choice[1].Weight, map[string]any{"world": world}), 1e-9)
	require.InDelta(t, 1.0, evalParam(t, m.CompiledLatents["n"].Params[ParamMean], map[string]any{"world": world}), 1e-9)

	b := m.CompiledBehaviors["svc"]
	require.NotNil(t, b.Cost)
	requireProgram(t, *b.Cost, "cost")
	require.NotNil(t, b.AbandonP)
	requireProgram(t, *b.AbandonP, "abandon.p")
	require.NotNil(t, b.FailP)
	requireProgram(t, *b.FailP, "fail.p")
	require.NotNil(t, b.Duration)
	require.Equal(t, KindNormal, b.Duration.Kind)
	requireProgram(t, b.Duration.Params[ParamMean], "duration.mean")
	requireProgram(t, b.Duration.Params[ParamSD], "duration.sd")

	band := b.Outputs["band"]
	require.Equal(t, KindChoice, band.Kind)
	require.Equal(t, "high", band.Choice[0].Value)
	require.Equal(t, "low", band.Choice[1].Value)
	vars := map[string]any{"world": world, "latent": map[string]any{"b": true}, "inputs": map[string]any{}}
	require.InDelta(t, 0.7, evalParam(t, band.Choice[0].Weight, vars), 1e-9)
	require.InDelta(t, 0.3, evalParam(t, band.Choice[1].Weight, vars), 1e-9)

	requireProgram(t, b.Outputs["score"].Params[ParamMin], "score.min")
	requireProgram(t, b.Outputs["score"].Params[ParamMax], "score.max")
	requireProgram(t, b.Outputs["tri"].Params[ParamMode], "tri.mode")
	requireProgram(t, b.Outputs["flag"].Params[ParamP], "flag.p")
	requireProgram(t, b.Outputs["ln"].Params[ParamMu], "ln.mu")
	require.NotNil(t, b.Outputs["x"].Expr)

	costVars := map[string]any{"world": world, "latent": map[string]any{"n": 2.0}, "inputs": map[string]any{"size": 3}}
	require.InDelta(t, 23.0, evalParam(t, *b.Cost, costVars), 1e-9)
}

func TestCompile_NumericParamsAreLiterals(t *testing.T) {
	latents := `
  b: {bernoulli: {p: 0.25}}
  n: {normal: {mean: 10, sd: 2.5}}
  tr: {triangular: {min: 1, mode: 2, max: 3}}
  c: {choice: {x: 1, y: 3}}
  f: {fixed: 7}
  emp: {empirical: [1, 2, 3]}
`
	behavior := `
    cost: 20
    duration: 48h
    abandon: {p: 0.1}
    fail: {p: 0, class: critical}
    output:
      band: {choice: {low: 0.4, high: 0.6}}
      result: {fixed: pass}
`
	m, errs := compileInline(t, paramsGraph, simWith(latents, behavior))
	require.Nil(t, errs)

	literal := func(p CompiledParam, want float64, name string) {
		t.Helper()
		require.True(t, p.IsLiteral(), name)
		require.Nil(t, p.Program, name)
		require.InDelta(t, want, p.Literal, 1e-12, name)
	}
	literal(m.CompiledLatents["b"].Params[ParamP], 0.25, "b.p")
	literal(m.CompiledLatents["n"].Params[ParamMean], 10, "n.mean")
	literal(m.CompiledLatents["n"].Params[ParamSD], 2.5, "n.sd")
	literal(m.CompiledLatents["tr"].Params[ParamMin], 1, "tr.min")
	literal(m.CompiledLatents["tr"].Params[ParamMode], 2, "tr.mode")
	literal(m.CompiledLatents["tr"].Params[ParamMax], 3, "tr.max")
	literal(m.CompiledLatents["c"].Choice[0].Weight, 1, "c.x")
	literal(m.CompiledLatents["c"].Choice[1].Weight, 3, "c.y")

	require.Equal(t, KindFixed, m.CompiledLatents["f"].Kind)
	require.InDelta(t, 7.0, normalizeNumber(m.CompiledLatents["f"].Fixed), 1e-12)
	require.Equal(t, KindEmpirical, m.CompiledLatents["emp"].Kind)
	require.Equal(t, []float64{1, 2, 3}, m.CompiledLatents["emp"].Empirical)

	b := m.CompiledBehaviors["svc"]
	literal(*b.Cost, 20, "cost")
	literal(*b.AbandonP, 0.1, "abandon.p")
	literal(*b.FailP, 0, "fail.p")
	require.Equal(t, &CompiledValue{Kind: KindFixed, Fixed: "48h"}, b.Duration)
	literal(b.Outputs["band"].Choice[0].Weight, 0.6, "band.high")
	require.Equal(t, KindFixed, b.Outputs["result"].Kind)
	require.Equal(t, "pass", b.Outputs["result"].Fixed)
}

func TestCompile_ParamErrors(t *testing.T) {
	okLatents := `
  b: {bernoulli: {p: 0.5}}
`
	okBehavior := `
    cost: 1
`
	cases := []struct {
		name     string
		latents  string
		behavior string
		path     string
	}{
		{"latent weight type error", "  c: {choice: {x: \"'heavy'\", y: 1}}\n", okBehavior, "$.latents.c.choice.x"},
		{"latent references latent", "  n: {normal: {mean: \"latent.b\", sd: 1}}\n", okBehavior, "$.latents.n.normal.mean"},
		{"latent references inputs", "  u: {uniform: {min: 0, max: \"inputs.x\"}}\n", okBehavior, "$.latents.u.uniform.max"},
		{"latent bool param", "  b: {bernoulli: {p: \"world.p > 0.1\"}}\n", okBehavior, "$.latents.b.bernoulli.p"},
		{"behavior weight type error", okLatents, "    output:\n      band: {choice: {low: \"'x'\", high: 0.5}}\n", "$.behaviors.svc.output.band.choice.low"},
		{"behavior references nodes", okLatents, "    cost: \"nodes.pick.output\"\n", "$.behaviors.svc.cost"},
		{"behavior syntax error", okLatents, "    abandon: {p: \"1 +\"}\n", "$.behaviors.svc.abandon.p"},
		{"fail p string output", okLatents, "    fail: {p: \"'0.1'\", class: retryable}\n", "$.behaviors.svc.fail.p"},
		{"duration param", okLatents, "    duration: {lognormal: {mu: \"run.cost\", sigma: 1}}\n", "$.behaviors.svc.duration.lognormal.mu"},
		{"triangular mode", okLatents, "    output:\n      t: {triangular: {min: 0, mode: \"true\", max: 1}}\n", "$.behaviors.svc.output.t.triangular.mode"},
		{"constraint not bool", okLatents, okBehavior, "$.objective.constraints.c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := simWith(c.latents, c.behavior)
			if c.name == "constraint not bool" {
				s += "  constraints: {c: \"run.cost + 1.0\"}\n"
			}
			m, errs := compileInline(t, paramsGraph, s)
			require.Nil(t, m)
			requireIssue(t, errs, c.path, sim.CodeInvalidExpression)
		})
	}
}

func TestCompile_BehaviorParamsAcceptInputs(t *testing.T) {
	behavior := `
    cost: "double(inputs.amount) * 0.01"
    abandon: {p: "inputs.priority == 'low' ? 0.2 : 0.0"}
    output:
      band: {choice: {low: "inputs.risk", high: "1.0 - inputs.risk"}}
`
	m, errs := compileInline(t, paramsGraph, simWith("  b: {bernoulli: {p: 0.5}}\n", behavior))
	require.Nil(t, errs)
	vars := map[string]any{
		"world":  map[string]any{},
		"latent": map[string]any{},
		"inputs": map[string]any{"amount": 1000, "priority": "low", "risk": 0.25},
	}
	b := m.CompiledBehaviors["svc"]
	require.InDelta(t, 10.0, evalParam(t, *b.Cost, vars), 1e-9)
	require.InDelta(t, 0.2, evalParam(t, *b.AbandonP, vars), 1e-9)
	require.InDelta(t, 0.75, evalParam(t, b.Outputs["band"].Choice[0].Weight, vars), 1e-9)
}

func TestCompile_ErrorsSortedAndDeterministic(t *testing.T) {
	latents := "  z: {normal: {mean: \"latent.q\", sd: \"'s'\"}}\n  a: {bernoulli: {p: \"inputs.x\"}}\n"
	behavior := "    cost: \"nodes.x\"\n"
	_, first := compileInline(t, paramsGraph, simWith(latents, behavior))
	_, second := compileInline(t, paramsGraph, simWith(latents, behavior))
	require.Equal(t, first.Issues(), second.Issues())
	issues := first.Issues()
	require.GreaterOrEqual(t, len(issues), 4)
	for i := 1; i < len(issues); i++ {
		require.LessOrEqual(t, issues[i-1].Path, issues[i].Path)
	}
}

func TestCompile_GraphExtension(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.txt"), []byte(paramsGraph), 0o600))
	def, err := ParseSimModel([]byte(simWith("  b: {bernoulli: {p: 0.5}}\n", "    cost: 1\n")), engine.FormatYAML)
	require.NoError(t, err)
	def.Graph = "graph.txt"
	_, errs := Compile(def, dir, setupRegistry())
	requireIssue(t, errs, "$.graph", engine.CodeInvalidFormat)

	def.Graph = "missing.yaml"
	_, errs = Compile(def, dir, setupRegistry())
	requireIssue(t, errs, "$.graph", engine.CodeUnresolvedReference)
}

func TestCompilePolicy_RuleWhenMustBeBool(t *testing.T) {
	m, errs := compileInline(t, paramsGraph, simWith("  b: {bernoulli: {p: 0.5}}\n", "    cost: 1\n"))
	require.Nil(t, errs)
	p, err := ParsePolicy([]byte(`
apiVersion: policy/v1
id: p
version: v
decisions:
  pick:
    rules:
      - {when: "'yes'", choose: a}
      - {else: b}
`), engine.FormatYAML)
	require.NoError(t, err)
	_, pErrs := CompilePolicy(p, m)
	requireIssue(t, pErrs, "$.decisions.pick.rules[0].when", sim.CodeInvalidExpression)
}

func TestTablePolicy_NumericNormalization(t *testing.T) {
	m, errs := compileInline(t, paramsGraph, simWith("  b: {bernoulli: {p: 0.5}}\n", "    cost: 1\n"))
	require.Nil(t, errs)
	p, err := ParsePolicy([]byte(`
apiVersion: policy/v1
id: tbl
version: v2
decisions:
  pick:
    table:
      - observed: {svc.output.band: 1}
        choose: a
      - observed: {svc.output.band: 2.5}
        choose: a
      - observed: {svc.output.band: "1"}
        choose: b
    default: b
`), engine.FormatYAML)
	require.NoError(t, err)
	pol, pErrs := CompilePolicy(p, m)
	require.Nil(t, pErrs)
	require.Equal(t, "tbl@v2", pol.ID())

	cases := []struct {
		name string
		obs  any
		want string
	}{
		{"float64 1.0", float64(1), "a"},
		{"int 1", 1, "a"},
		{"int64 1", int64(1), "a"},
		{"int32 1", int32(1), "a"},
		{"uint8 1", uint8(1), "a"},
		{"float32 2.5", float32(2.5), "a"},
		{"json.Number 1", json.Number("1"), "a"},
		{"json.Number 1.0", json.Number("1.0"), "a"},
		{"json.Number 2.5", json.Number("2.5"), "a"},
		{"string 1 is not a number", "1", "b"},
		{"other number", 3, "b"},
		{"nil does not match a number", nil, "b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := pol.Choose(context.Background(), "pick", sim.Observation{"svc.output.band": c.obs}, nil)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}

	got, err := pol.Choose(context.Background(), "pick", sim.Observation{}, nil)
	require.NoError(t, err)
	require.Equal(t, "b", got, "missing observation is nil and falls to the default")
}

func TestTablePolicy_UnobservedKey(t *testing.T) {
	m, errs := compileInline(t, paramsGraph, simWith("  b: {bernoulli: {p: 0.5}}\n", "    cost: 1\n"))
	require.Nil(t, errs)
	p, err := ParsePolicy([]byte(`
apiVersion: policy/v1
id: tbl
version: v
decisions:
  pick:
    table:
      - observed: {svc.output.other: 1}
        choose: c
    default: a
`), engine.FormatYAML)
	require.NoError(t, err)
	_, pErrs := CompilePolicy(p, m)
	requireIssue(t, pErrs, "$.decisions.pick.table[0].observed['svc.output.other']", sim.CodePolicyUnobservedReference)
	requireIssue(t, pErrs, "$.decisions.pick.table[0].choose", sim.CodePolicyUnknownOption)
}

// Ensure the documented numeric output types are what the checker reports.
func TestCompile_ParamOutputTypes(t *testing.T) {
	env, err := newEnv("world")
	require.NoError(t, err)
	errs := &engine.ValidationErrors{}
	require.NotNil(t, compileCEL("1", "$.x", env, numericTypes, errs))
	require.NotNil(t, compileCEL("1.5", "$.x", env, numericTypes, errs))
	require.NotNil(t, compileCEL("world.a", "$.x", env, numericTypes, errs))
	require.Zero(t, errs.Len())
	require.Nil(t, compileCEL("'s'", "$.s", env, numericTypes, errs))
	require.Nil(t, compileCEL(" ", "$.e", env, numericTypes, errs))
	require.Nil(t, compileCEL("true", "$.b", env, []*cel.Type{cel.DoubleType}, errs))
	require.Equal(t, 3, errs.Len())
}

func TestLoan_DiscreteDurationValueSpec(t *testing.T) {
	m := loanModel(t)
	ok, reasons := m.Discrete()
	require.True(t, ok, "reasons: %v", reasons)
	require.Empty(t, reasons)

	m, errs := compileLoanVariant(t, nil, []string{"duration: 48h", "duration: {lognormal: {mu: 10, sigma: 0.5}}"})
	require.Nil(t, errs)
	ok, reasons = m.Discrete()
	require.False(t, ok)
	require.Equal(t, []string{"$.behaviors.human_review.duration.lognormal: lognormal distribution is continuous"}, reasons)
}
