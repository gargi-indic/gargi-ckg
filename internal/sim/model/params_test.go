package model

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

// paramsPolicy returns a policy/v1 document over paramsGraph (decision pick
// observing svc.output.band, options a and b) with the given params block
// (indented by two spaces) and the when of its first rule.
func paramsPolicy(params, when string) string {
	doc := "apiVersion: policy/v1\nid: p\nversion: v\n"
	if params != "" {
		doc += "params:\n" + params
	}
	return doc + "decisions:\n  pick:\n    rules:\n      - when: " + when + "\n        choose: a\n      - else: b\n"
}

func paramsModel(t *testing.T) *CompiledModel {
	t.Helper()
	m, errs := compileInline(t, paramsGraph, simWith("  b: {bernoulli: {p: 0.5}}\n", "    cost: 1\n"))
	require.Nil(t, errs)
	return m
}

// compileParams parses and compiles doc against paramsGraph.
func compileParams(t *testing.T, doc string) (Parameterized, *engine.ValidationErrors) {
	t.Helper()
	p, err := ParsePolicy([]byte(doc), engine.FormatYAML)
	require.NoError(t, err)
	pol, errs := CompilePolicy(p, paramsModel(t))
	if errs.Len() > 0 {
		return nil, errs
	}
	pp, ok := pol.(Parameterized)
	require.True(t, ok)
	return pp, nil
}

func ptr(f float64) *float64 { return &f }

func TestParsePolicy_Params(t *testing.T) {
	p, err := ParsePolicy([]byte(paramsPolicy(
		"  theta: {min: 0, max: 1, step: 0.1, default: 0.5}\n  mode: {values: [low, 2, high]}\n  open: {min: -1, max: 1}\n",
		`"svc.output.band >= params.theta"`)), engine.FormatYAML)
	require.NoError(t, err)
	require.Equal(t, map[string]ParamSpec{
		"theta": {Min: ptr(0), Max: ptr(1), Step: ptr(0.1), Default: 0.5},
		"mode":  {Values: []any{"low", 2.0, "high"}},
		"open":  {Min: ptr(-1), Max: ptr(1)},
	}, p.Params)
	require.Equal(t, 0.5, p.Params["theta"].DefaultValue())
	require.Equal(t, "low", p.Params["mode"].DefaultValue())
	require.Equal(t, -1.0, p.Params["open"].DefaultValue())

	// Unknown fields and wrong types are still rejected by the binder.
	_, err = ParsePolicy([]byte(paramsPolicy("  theta: {min: 0, max: 1, stp: 0.1}\n  mode: {values: [true]}\n  x: {min: a, max: 1}\n", `"true"`)), engine.FormatYAML)
	var ve *engine.ValidationErrors
	require.ErrorAs(t, err, &ve)
	require.Equal(t, []engine.ValidationIssue{
		{Path: "$.params.mode.values", Code: engine.CodeInvalidValue, Message: "values must not be empty"},
		{Path: "$.params.mode.values[0]", Code: engine.CodeInvalidType, Message: "expected number or string"},
		{Path: "$.params.theta.stp", Code: engine.CodeUnknownField, Message: "unknown field 'stp'"},
		{Path: "$.params.x.min", Code: engine.CodeInvalidType, Message: "expected number"},
		{Path: "$.params.x.min", Code: engine.CodeInvalidValue, Message: "range param requires min"},
	}, ve.Issues())
}

func TestParsePolicy_ParamSpecErrors(t *testing.T) {
	tests := []struct {
		name   string
		params string
		want   []engine.ValidationIssue
	}{
		{"min > max", "  theta: {min: 2, max: 1}\n", []engine.ValidationIssue{
			{Path: "$.params.theta.max", Code: engine.CodeInvalidValue, Message: "max 1 must be >= min 2"},
		}},
		{"step zero", "  theta: {min: 0, max: 1, step: 0}\n", []engine.ValidationIssue{
			{Path: "$.params.theta.step", Code: engine.CodeInvalidValue, Message: "step must be > 0"},
		}},
		{"step negative", "  theta: {min: 0, max: 1, step: -0.1}\n", []engine.ValidationIssue{
			{Path: "$.params.theta.step", Code: engine.CodeInvalidValue, Message: "step must be > 0"},
		}},
		{"range and values", "  theta: {min: 0, max: 1, values: [0, 1]}\n", []engine.ValidationIssue{
			{Path: "$.params.theta", Code: engine.CodeInvalidValue, Message: "param must be either a range (min, max, step) or a values list, not both"},
		}},
		{"neither", "  theta: {}\n", []engine.ValidationIssue{
			{Path: "$.params.theta", Code: engine.CodeInvalidValue, Message: "param must be a range (min, max, step) or a values list"},
		}},
		{"missing max", "  theta: {min: 0, step: 0.1}\n", []engine.ValidationIssue{
			{Path: "$.params.theta.max", Code: engine.CodeInvalidValue, Message: "range param requires max"},
		}},
		{"empty values", "  theta: {values: []}\n", []engine.ValidationIssue{
			{Path: "$.params.theta.values", Code: engine.CodeInvalidValue, Message: "values must not be empty"},
		}},
		{"duplicate values", "  theta: {values: [1, x, 1.0, x]}\n", []engine.ValidationIssue{
			{Path: "$.params.theta.values[2]", Code: engine.CodeInvalidValue, Message: "duplicate value 1 (also values[0])"},
			{Path: "$.params.theta.values[3]", Code: engine.CodeInvalidValue, Message: `duplicate value "x" (also values[1])`},
		}},
		{"default out of range", "  theta: {min: 0, max: 1, default: 1.5}\n", []engine.ValidationIssue{
			{Path: "$.params.theta.default", Code: engine.CodeInvalidValue, Message: "default value 1.5 is not a number in [0, 1]"},
		}},
		{"default not a number", "  theta: {min: 0, max: 1, default: high}\n", []engine.ValidationIssue{
			{Path: "$.params.theta.default", Code: engine.CodeInvalidValue, Message: `default value "high" is not a number in [0, 1]`},
		}},
		{"default not in values", "  theta: {values: [low, high], default: mid}\n", []engine.ValidationIssue{
			{Path: "$.params.theta.default", Code: engine.CodeInvalidValue, Message: `default value "mid" is not one of the param's values`},
		}},
		{"bad names", "  Theta: {min: 0, max: 1}\n  1x: {values: [a]}\n  ok_1: {values: [a]}\n", []engine.ValidationIssue{
			{Path: "$.params.Theta", Code: engine.CodeInvalidValue, Message: `param name "Theta" must match ^[a-z_][a-z0-9_]*$`},
			{Path: "$.params['1x']", Code: engine.CodeInvalidValue, Message: `param name "1x" must match ^[a-z_][a-z0-9_]*$`},
		}},
		{"all reported, sorted", "  b: {min: 1, max: 0, step: 0}\n  a: {values: []}\n", []engine.ValidationIssue{
			{Path: "$.params.a.values", Code: engine.CodeInvalidValue, Message: "values must not be empty"},
			{Path: "$.params.b.max", Code: engine.CodeInvalidValue, Message: "max 0 must be >= min 1"},
			{Path: "$.params.b.step", Code: engine.CodeInvalidValue, Message: "step must be > 0"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePolicy([]byte(paramsPolicy(tt.params, `"true"`)), engine.FormatYAML)
			var ve *engine.ValidationErrors
			require.ErrorAs(t, err, &ve)
			require.Equal(t, tt.want, ve.Issues())
		})
	}

	// CompilePolicy validates params too, for documents built in Go.
	m := paramsModel(t)
	_, errs := CompilePolicy(&Policy{
		ID: "p", Version: "v",
		Params:    map[string]ParamSpec{"theta": {Min: ptr(1), Max: ptr(0)}},
		Decisions: map[string]Decision{"pick": {Rules: []Rule{{Else: "a"}}}},
	}, m)
	require.Equal(t, []engine.ValidationIssue{
		{Path: "$.params.theta.max", Code: engine.CodeInvalidValue, Message: "max 0 must be >= min 1"},
	}, errs.Issues())
}

func TestCompilePolicy_ParamReferences(t *testing.T) {
	const theta = "  theta: {min: 0, max: 1, default: 0.5}\n"
	whenPath := "$.decisions.pick.rules[0].when"
	tests := []struct {
		name string
		when string
		code engine.ErrorCode
	}{
		{"unknown param", `"svc.output.band >= params.unknown"`, sim.CodePolicyUnknownParam},
		{"bare params", `"size(params) > 0"`, sim.CodePolicyUnobservedReference},
		{"index", `"svc.output.band >= params['theta']"`, sim.CodePolicyUnobservedReference},
		{"select into param", `"params.theta.x == 1"`, sim.CodePolicyUnobservedReference},
		{"has", `"has(params.theta)"`, sim.CodePolicyUnobservedReference},
		{"params never widen the information set", `"params.theta > 0.0 && svc.output.secret == 1"`, sim.CodePolicyUnobservedReference},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := compileParams(t, paramsPolicy(theta, tt.when))
			require.Equal(t, []engine.ValidationIssue{{Path: whenPath, Code: tt.code, Message: errs.Issues()[0].Message}}, errs.Issues())
		})
	}

	// A policy without params still gets the unknown-param code.
	_, errs := compileParams(t, paramsPolicy("", `"svc.output.band >= params.theta"`))
	require.Equal(t, []engine.ValidationIssue{
		{Path: whenPath, Code: sim.CodePolicyUnknownParam, Message: `unknown param "theta"`},
	}, errs.Issues())
}

// TestCompilePolicy_ParamsShadowedByNode checks that a decision observing a
// node named params cannot be combined with declared params.
func TestCompilePolicy_ParamsShadowedByNode(t *testing.T) {
	m, errs := compileInline(t, strings.ReplaceAll(paramsGraph, "svc", "params"),
		strings.Replace(simWith("  b: {bernoulli: {p: 0.5}}\n", "    cost: 1\n"), "  svc:\n", "  params:\n", 1))
	require.Nil(t, errs)
	doc := strings.ReplaceAll(paramsPolicy("", `"params.output.band == 1"`), "svc", "params")
	p, err := ParsePolicy([]byte(doc), engine.FormatYAML)
	require.NoError(t, err)
	pol, errs := CompilePolicy(p, m)
	require.Nil(t, errs)
	choice, err := pol.Choose(context.Background(), "pick", sim.Observation{"params.output.band": 1.0}, nil)
	require.NoError(t, err)
	require.Equal(t, "a", choice)

	p.Params = map[string]ParamSpec{"theta": {Values: []any{1.0}}}
	_, errs = CompilePolicy(p, m)
	require.Equal(t, []engine.ValidationIssue{
		{Path: "$.decisions.pick", Code: engine.CodeInvalidValue, Message: `decision observes node "params", which shadows the policy params`},
	}, errs.Issues())
}

// TestParams_ChooseCrossTypeNumbers checks params.theta (a CEL double)
// against observed doubles and ints.
func TestParams_ChooseCrossTypeNumbers(t *testing.T) {
	pol, errs := compileParams(t, paramsPolicy("  theta: {min: 0, max: 5, default: 1}\n", `"svc.output.band >= params.theta"`))
	require.Nil(t, errs)
	ctx := context.Background()
	for _, tt := range []struct {
		band any
		want string
	}{
		{1.5, "a"},
		{0.5, "b"},
		{1.0, "a"},
		{1, "a"},
		{0, "b"},
		{int64(2), "a"},
		{int64(0), "b"},
	} {
		got, err := pol.Choose(ctx, "pick", sim.Observation{"svc.output.band": tt.band}, nil)
		require.NoError(t, err, "band %v (%T)", tt.band, tt.band)
		require.Equal(t, tt.want, got, "band %v (%T)", tt.band, tt.band)
	}

	// Rebinding changes the decision without recompiling.
	high, err := pol.WithParams(map[string]any{"theta": 3})
	require.NoError(t, err)
	got, err := high.Choose(ctx, "pick", sim.Observation{"svc.output.band": 2}, nil)
	require.NoError(t, err)
	require.Equal(t, "b", got)

	// Values-list params keep their type: strings compare as strings.
	pol, errs = compileParams(t, paramsPolicy("  band: {values: [low, high]}\n", `"svc.output.band == params.band"`))
	require.Nil(t, errs)
	got, err = pol.Choose(ctx, "pick", sim.Observation{"svc.output.band": "low"}, nil)
	require.NoError(t, err)
	require.Equal(t, "a", got)
	high, err = pol.WithParams(map[string]any{"band": "high"})
	require.NoError(t, err)
	got, err = high.Choose(ctx, "pick", sim.Observation{"svc.output.band": "low"}, nil)
	require.NoError(t, err)
	require.Equal(t, "b", got)
}

func TestParams_WithParams(t *testing.T) {
	pol, errs := compileParams(t, paramsPolicy(
		"  b: {values: [x, 2, 0.25]}\n  a: {min: 0, max: 1, step: 0.5, default: 1}\n",
		`"svc.output.band >= params.a && params.b != 'y'"`))
	require.Nil(t, errs)

	require.Equal(t, "p@v[a=1,b=x]", pol.ID())
	require.Equal(t, "policy p version v params a=1,b=x", pol.Describe())
	require.Equal(t, map[string]any{"a": 1.0, "b": "x"}, pol.Params())
	require.Len(t, pol.ParamSpecs(), 2)

	// Missing names keep their current value; numbers are bound as float64.
	c, err := pol.WithParams(map[string]any{"b": 2})
	require.NoError(t, err)
	require.Equal(t, "p@v[a=1,b=2]", c.ID())
	require.Equal(t, map[string]any{"a": 1.0, "b": 2.0}, c.Params())
	c, err = c.WithParams(map[string]any{"a": 0.3, "b": 0.25})
	require.NoError(t, err)
	require.Equal(t, "p@v[a=0.3,b=0.25]", c.ID())
	require.Equal(t, "policy p version v params a=0.3,b=0.25", c.Describe())
	tenth := 0.1
	c, err = c.WithParams(map[string]any{"a": tenth + 0.2})
	require.NoError(t, err)
	require.Equal(t, "p@v[a=0.30000000000000004,b=0.25]", c.ID(), "IDs use the shortest round-trip format")

	// The receiver is unchanged.
	require.Equal(t, "p@v[a=1,b=x]", pol.ID())
	require.Equal(t, map[string]any{"a": 1.0, "b": "x"}, pol.Params())

	// Params and ParamSpecs return copies.
	pol.Params()["a"] = 0.0
	require.Equal(t, 1.0, pol.Params()["a"])

	// CEL programs are shared, not recompiled.
	orig, copied := pol.(*runtimePolicy), c.(*runtimePolicy)
	require.Equal(t, reflect.ValueOf(orig.decisions).Pointer(), reflect.ValueOf(copied.decisions).Pointer())
	require.Same(t, orig.decisions["pick"], copied.decisions["pick"])

	// Every problem is reported, sorted.
	_, err = pol.WithParams(map[string]any{"nope": 1, "a": 1.5, "b": "y", "c": 0})
	var ve *engine.ValidationErrors
	require.ErrorAs(t, err, &ve)
	require.Equal(t, []engine.ValidationIssue{
		{Path: "$.params.a", Code: sim.CodeParamOutOfRange, Message: "value 1.5 is not a number in [0, 1]"},
		{Path: "$.params.b", Code: sim.CodeParamOutOfRange, Message: `value "y" is not one of the param's values`},
		{Path: "$.params.c", Code: sim.CodePolicyUnknownParam, Message: `unknown param "c"`},
		{Path: "$.params.nope", Code: sim.CodePolicyUnknownParam, Message: `unknown param "nope"`},
	}, ve.Issues())
	_, err = pol.WithParams(map[string]any{"a": "0.5", "b": 3})
	require.ErrorAs(t, err, &ve)
	require.Equal(t, []engine.ValidationIssue{
		{Path: "$.params.a", Code: sim.CodeParamOutOfRange, Message: `value "0.5" is not a number in [0, 1]`},
		{Path: "$.params.b", Code: sim.CodeParamOutOfRange, Message: "value 3 is not one of the param's values"},
	}, ve.Issues())
}

// TestParams_NoParamsUnchanged checks that a policy without params keeps its
// ID and description and still implements Parameterized.
func TestParams_NoParamsUnchanged(t *testing.T) {
	pol, errs := compileParams(t, paramsPolicy("", `"svc.output.band == 'low'"`))
	require.Nil(t, errs)
	require.Equal(t, "p@v", pol.ID())
	require.Equal(t, "policy p version v", pol.Describe())
	require.Empty(t, pol.Params())
	require.Empty(t, pol.ParamSpecs())
	c, err := pol.WithParams(nil)
	require.NoError(t, err)
	require.Equal(t, "p@v", c.ID())
	_, err = pol.WithParams(map[string]any{"theta": 1})
	require.Error(t, err)
}
