package model

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

const loanDir = "../../../examples/simulation/loan"

func readLoan(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(loanDir, name))
	require.NoError(t, err)
	return string(data)
}

// compileLoanVariant writes the loan graph and sim (after applying the given
// replacements) to a temp dir and compiles them.
func compileLoanVariant(t *testing.T, graphRepl, simRepl []string) (*CompiledModel, *engine.ValidationErrors) {
	t.Helper()
	g, s := readLoan(t, "graph.yaml"), readLoan(t, "sim.yaml")
	for i := 0; i+1 < len(graphRepl); i += 2 {
		require.Contains(t, g, graphRepl[i])
		g = strings.Replace(g, graphRepl[i], graphRepl[i+1], 1)
	}
	for i := 0; i+1 < len(simRepl); i += 2 {
		require.Contains(t, s, simRepl[i])
		s = strings.Replace(s, simRepl[i], simRepl[i+1], 1)
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(g), 0o600))
	def, err := ParseSimModel([]byte(s), engine.FormatYAML)
	require.NoError(t, err)
	return Compile(def, dir, setupRegistry())
}

func loanModel(t *testing.T) *CompiledModel {
	t.Helper()
	m, errs := compileLoanVariant(t, nil, nil)
	if errs != nil && errs.Len() > 0 {
		t.Fatalf("loan model must compile: %v", errs.Error())
	}
	return m
}

func requireCode(t *testing.T, errs *engine.ValidationErrors, code engine.ErrorCode) {
	t.Helper()
	require.NotNil(t, errs, "expected error code %s", code)
	for _, is := range errs.Issues() {
		if is.Code == code {
			return
		}
	}
	t.Fatalf("expected error code %s, got: %v", code, errs.Error())
}

func loanPolicy(t *testing.T, m *CompiledModel, routeWhen string) (sim.Policy, *engine.ValidationErrors) {
	t.Helper()
	y := "apiVersion: policy/v1\nid: loan-underwriting\nversion: probe\ndecisions:\n" +
		"  screen: {rules: [{else: score}]}\n" +
		"  route:\n    rules:\n      - when: " + `"` + strings.ReplaceAll(routeWhen, `"`, `\"`) + `"` + "\n        choose: reject\n      - else: review\n" +
		"  final: {rules: [{else: reject}]}\n"
	p, err := ParsePolicy([]byte(y), engine.FormatYAML)
	require.NoError(t, err)
	return CompilePolicy(p, m)
}

func TestLoan_CompilesWithSupportAndDiscrete(t *testing.T) {
	m := loanModel(t)

	ok, reasons := m.Discrete()
	require.True(t, ok, "reasons: %v", reasons)

	cases := map[string][]any{
		"screen.output.choice":       {"score", "skip"},
		"ai_score.output.band":       {"high", "low"},
		"human_review.output.result": {"fail", "pass"},
		"route.status":               {"Skipped", "Succeeded"},
	}
	for path, want := range cases {
		got, ok := m.ObservedSupport(path)
		require.True(t, ok, path)
		require.Equal(t, want, got, path)
	}
	_, ok = m.ObservedSupport("nope.output.x")
	require.False(t, ok)
}

func TestLoan_RecessionPolicyRuntime(t *testing.T) {
	m := loanModel(t)
	p, err := ParsePolicy([]byte(readLoan(t, "policies/recession-v1.yaml")), engine.FormatYAML)
	require.NoError(t, err)
	pol, errs := CompilePolicy(p, m)
	if errs != nil && errs.Len() > 0 {
		t.Fatalf("recession-v1 must compile: %v", errs.Error())
	}
	require.Equal(t, "loan-underwriting@recession-v1", pol.ID())

	ctx := context.Background()
	cases := []struct {
		decision string
		obs      sim.Observation
		want     string
	}{
		{"screen", sim.Observation{}, "score"},
		{"route", sim.Observation{"screen.output.choice": "score", "ai_score.output.band": "high"}, "reject"},
		{"route", sim.Observation{"screen.output.choice": "score", "ai_score.output.band": "low"}, "review"},
		{"route", sim.Observation{"screen.output.choice": "skip", "ai_score.output.band": nil}, "review"},
		{"final", sim.Observation{"screen.output.choice": "score", "ai_score.output.band": "low", "human_review.output.result": "pass"}, "approve"},
		{"final", sim.Observation{"screen.output.choice": "score", "ai_score.output.band": "low", "human_review.output.result": "fail"}, "reject"},
	}
	for _, c := range cases {
		got, err := pol.Choose(ctx, engine.NodeID(c.decision), c.obs, nil)
		require.NoError(t, err, c.decision)
		require.Equal(t, c.want, got, "%s %v", c.decision, c.obs)
	}
}

func TestLoan_InformationSetEnforcement(t *testing.T) {
	m := loanModel(t)

	_, errs := loanPolicy(t, m, "ai_score.output.band == 'high'")
	require.True(t, errs == nil || errs.Len() == 0, "observed reference must be accepted: %v", errs)

	rejected := map[string]engine.ErrorCode{
		"latent.bad == true":                   sim.CodeInvalidExpression,
		"world.p_bad > 0.1":                    sim.CodeInvalidExpression,
		"nodes.ai_score.status == 'Succeeded'": sim.CodeInvalidExpression,
		"ai_score.output == {}":                sim.CodePolicyUnobservedReference,
		"ai_score['output']['band'] == 'x'":    sim.CodePolicyUnobservedReference,
		"has(ai_score.output.other)":           sim.CodePolicyUnobservedReference,
		"ai_score.output.bandit == 'x'":        sim.CodePolicyUnobservedReference,
		"human_review.output.result == 'pass'": sim.CodeInvalidExpression,
	}
	for expr, code := range rejected {
		_, errs := loanPolicy(t, m, expr)
		requireCode(t, errs, code)
	}
}

func TestLoan_CompileErrorCodes(t *testing.T) {
	cases := []struct {
		name      string
		graphRepl []string
		simRepl   []string
		code      engine.ErrorCode
	}{
		{"nested observe path", []string{"observes: [screen.output.choice, ai_score.output.band]", "observes: [screen.output.choice, ai_score.output.band.x]"}, nil, sim.CodeInvalidObservePath},
		{"empty node id", []string{"observes: [screen.output.choice, ai_score.output.band]", "observes: [screen.output.choice, .output.band]"}, nil, sim.CodeInvalidObservePath},
		{"bad segment", []string{"observes: [screen.output.choice, ai_score.output.band]", "observes: [screen.output.choice, ai_score.foo]"}, nil, sim.CodeInvalidObservePath},
		{"unknown observed node", []string{"observes: [screen.output.choice, ai_score.output.band]", "observes: [screen.output.choice, ghost.output.band]"}, nil, engine.CodeUnresolvedReference},
		{"non-ancestor", []string{"observes: [screen.output.choice, ai_score.output.band]", "observes: [screen.output.choice, final.output.choice]"}, nil, sim.CodeObservesNonAncestor},
		{"missing behavior", nil, []string{"  ai_score:\n", "  ai_scorex:\n"}, sim.CodeBehaviorMissing},
		{"unknown behavior node", nil, []string{"  ai_score:\n", "  ai_scorex:\n"}, sim.CodeBehaviorUnknownNode},
		{"objective returns string", nil, []string{`value: "-run.cost + (`, `value: "'x' + string(-run.cost + (`, `: 0.0)"`, `: 0.0))"`}, sim.CodeInvalidExpression},
		{"latent references latent", nil, []string{`p: "world.p_bad"`, `p: "latent.bad"`}, sim.CodeInvalidExpression},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errs := compileLoanVariant(t, c.graphRepl, c.simRepl)
			requireCode(t, errs, c.code)
		})
	}
}

func TestLoan_PolicyCoverageCodes(t *testing.T) {
	m := loanModel(t)
	compile := func(y string) *engine.ValidationErrors {
		p, err := ParsePolicy([]byte(y), engine.FormatYAML)
		require.NoError(t, err)
		_, errs := CompilePolicy(p, m)
		return errs
	}
	head := "apiVersion: policy/v1\nid: loan-underwriting\nversion: t\ndecisions:\n"
	requireCode(t, compile(head+"  screen: {rules: [{else: score}]}\n  route: {rules: [{else: review}]}\n"), sim.CodePolicyMissingDecision)
	requireCode(t, compile(head+"  screen: {rules: [{else: score}]}\n  route: {rules: [{else: review}]}\n  final: {rules: [{else: reject}]}\n  approve: {rules: [{else: x}]}\n"), sim.CodePolicyUnknownDecision)
	requireCode(t, compile(head+"  screen: {rules: [{else: maybe}]}\n  route: {rules: [{else: review}]}\n  final: {rules: [{else: reject}]}\n"), sim.CodePolicyUnknownOption)
}

func TestLoan_CompileDeterministic(t *testing.T) {
	repl := []string{"observes: [screen.output.choice, ai_score.output.band]", "observes: [ghost.output.x, final.output.choice, ai_score.foo]"}
	_, a := compileLoanVariant(t, repl, nil)
	_, b := compileLoanVariant(t, repl, nil)
	require.NotNil(t, a)
	require.Equal(t, a.Error(), b.Error())
	require.GreaterOrEqual(t, a.Len(), 3)
}
