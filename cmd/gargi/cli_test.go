package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/experiment"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/require"
)

const loanDir = "../../examples/simulation/loan"

// gargi runs the CLI in-process and returns its exit code and output.
func gargi(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, data, 0o600))
}

// loanCopy copies the loan model into a temporary directory with an
// experiment that keeps the exhaustive search and both evaluators but runs
// few Monte Carlo replications, so the test stays fast under -race. The
// exact values do not depend on the number of runs.
func loanCopy(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	copyFile(t, filepath.Join(loanDir, "sim.yaml"), filepath.Join(dir, "sim.yaml"))
	copyFile(t, filepath.Join(loanDir, "graph.yaml"), filepath.Join(dir, "graph.yaml"))
	exp := "apiVersion: experiment/v1\nmodel: ./sim.yaml\nworlds: [base, recession]\n" +
		"policies: {search: exhaustive}\nevaluator: both\nruns: 20\nseed: 42\ntop: 5\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "experiment.yaml"), []byte(exp), 0o600))
	return filepath.Join(dir, "experiment.yaml")
}

func TestVersion(t *testing.T) {
	code, out, _ := gargi(t, "version")
	require.Equal(t, 0, code)
	require.Equal(t, "gargi dev\n", out)
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"bogus"},
		{"model"},
		{"model", "check"},
		{"sim"},
		{"model", "validate"},
		{"sim", "run", "--local"},
		{"model", "validate", "--nope", "x.yaml"},
	} {
		code, _, errOut := gargi(t, args...)
		require.Equal(t, 2, code, "args %q", args)
		require.NotEmpty(t, errOut, "args %q", args)
	}
}

func TestModelValidate_Valid(t *testing.T) {
	code, out, errOut := gargi(t, "model", "validate", filepath.Join(loanDir, "sim.yaml"))
	require.Equal(t, 0, code, errOut)
	require.Equal(t, "OK\n", out)

	code, out, _ = gargi(t, "model", "validate", "--json", filepath.Join(loanDir, "sim.yaml"))
	require.Equal(t, 0, code)
	require.JSONEq(t, `{"errors":[]}`, out)
}

func TestModelValidate_Invalid(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join(loanDir, "graph.yaml"), filepath.Join(dir, "graph.yaml"))
	data, err := os.ReadFile(filepath.Join(loanDir, "sim.yaml"))
	require.NoError(t, err)
	invalid := strings.Replace(string(data), "behaviors:\n", "behaviors:\n  ghost_node:\n    cost: 1\n", 1)
	require.NotEqual(t, string(data), invalid)
	path := filepath.Join(dir, "sim.yaml")
	require.NoError(t, os.WriteFile(path, []byte(invalid), 0o600))

	code, out, _ := gargi(t, "model", "validate", path)
	require.Equal(t, 1, code)
	require.Contains(t, out, "ghost_node")
	line := regexp.MustCompile(`^[^:\s]+: [a-z_]+: .+$`)
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		require.Regexp(t, line, l)
	}

	// Flags after the positional file work too.
	code, out, _ = gargi(t, "model", "validate", path, "--json")
	require.Equal(t, 1, code)
	var report struct {
		Errors []engine.ValidationIssue `json:"errors"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &report))
	require.NotEmpty(t, report.Errors)
	for _, is := range report.Errors {
		require.NotEmpty(t, is.Path)
		require.NotEmpty(t, is.Code)
		require.NotEmpty(t, is.Message)
	}
}

func TestModelValidate_MissingFile(t *testing.T) {
	code, _, errOut := gargi(t, "model", "validate", filepath.Join(t.TempDir(), "missing.yaml"))
	require.Equal(t, 1, code)
	require.Contains(t, errOut, "missing.yaml")
}

func TestModelValidate_ParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sim.yaml")
	require.NoError(t, os.WriteFile(path, []byte("apiVersion: [unterminated\n"), 0o600))
	code, out, _ := gargi(t, "model", "validate", path)
	require.Equal(t, 1, code)
	require.NotEmpty(t, out)
}

func TestSimRun_RequiresLocal(t *testing.T) {
	code, out, errOut := gargi(t, "sim", "run", filepath.Join(loanDir, "experiment.yaml"))
	require.Equal(t, 2, code)
	require.Empty(t, out)
	require.Equal(t, "server mode arrives in P1; pass --local\n", errOut)
}

func TestSimRun_Loan(t *testing.T) {
	exp := loanCopy(t)
	for _, args := range [][]string{
		{"sim", "run", exp, "--local"},
		{"sim", "run", "--local", "--workers", "2", exp},
	} {
		code, out, errOut := gargi(t, args...)
		require.Equal(t, 0, code, errOut)
		require.Contains(t, out, "World base")
		require.Contains(t, out, "World recession")
		require.Contains(t, out, "POLICY")
		require.Contains(t, out, "P95 DURATION")
		require.Equal(t, 2, strings.Count(out, "Best policy: "), out)
		require.Contains(t, out, "Exact EV: 460.0\n")
		require.Contains(t, out, "Exact EV: 202.5\n")
		base, recession, ok := strings.Cut(out, "World recession")
		require.True(t, ok)
		require.Contains(t, base, "460.0")
		require.Contains(t, recession, "202.5")
	}
}

func TestSimRun_TopFlag(t *testing.T) {
	code, out, errOut := gargi(t, "sim", "run", "--local", "--top", "2", "--json", loanCopy(t))
	require.Equal(t, 0, code, errOut)
	var report experiment.Report
	require.NoError(t, json.Unmarshal([]byte(out), &report))
	for _, w := range report.Worlds {
		require.Len(t, w.Policies, 2)
	}
}

func TestSimRun_JSONOutAndEmitBest(t *testing.T) {
	exp := loanCopy(t)
	dir := t.TempDir()
	outFile := filepath.Join(dir, "report.json")
	bestDir := filepath.Join(dir, "best")

	code, out, errOut := gargi(t, "sim", "run", exp, "--local", "--json", "--out", outFile, "--emit-best", bestDir)
	require.Equal(t, 0, code, errOut)

	// stdout is exactly the Report JSON.
	var report experiment.Report
	dec := json.NewDecoder(strings.NewReader(out))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(&report))
	require.False(t, dec.More())
	require.True(t, strings.HasPrefix(out, "{"))
	require.True(t, strings.HasSuffix(out, "}\n"))
	require.Equal(t, "both", report.Evaluator)
	require.Len(t, report.Worlds, 2)
	want := map[string]float64{"base": 460.0, "recession": 202.525}
	for _, w := range report.Worlds {
		require.NotEmpty(t, w.Policies)
		require.True(t, w.Policies[0].Objective.Exact)
		require.InDelta(t, want[string(w.World)], w.Policies[0].Objective.Mean, 1e-9)
	}

	// --out holds the same JSON.
	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	require.Equal(t, out, string(data))

	// --emit-best writes a parseable policy per world.
	for _, w := range report.Worlds {
		data, err := os.ReadFile(filepath.Join(bestDir, string(w.World)+".policy.yaml"))
		require.NoError(t, err)
		require.Contains(t, string(data), w.Policies[0].Description)
		p, err := model.ParsePolicy(data, engine.FormatYAML)
		require.NoError(t, err)
		require.Equal(t, w.Policies[0].PolicyID, p.ID+"@"+p.Version)
	}
}

func TestSimRun_EmitBestRequiresTablePolicy(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join(loanDir, "sim.yaml"), filepath.Join(dir, "sim.yaml"))
	copyFile(t, filepath.Join(loanDir, "graph.yaml"), filepath.Join(dir, "graph.yaml"))
	copyFile(t, filepath.Join(loanDir, "policies", "recession-v1.yaml"), filepath.Join(dir, "policies", "recession-v1.yaml"))
	exp := "apiVersion: experiment/v1\nmodel: ./sim.yaml\nworlds: [base]\n" +
		"policies: {files: [policies/recession-v1.yaml]}\nevaluator: exact\nruns: 1\nseed: 42\n"
	path := filepath.Join(dir, "experiment.yaml")
	require.NoError(t, os.WriteFile(path, []byte(exp), 0o600))

	code, out, errOut := gargi(t, "sim", "run", path, "--local", "--emit-best", filepath.Join(dir, "best"))
	require.Equal(t, 1, code)
	require.Empty(t, out)
	require.Contains(t, errOut, "--emit-best")
}

func TestSimRun_InvalidExperiment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "experiment.yaml")
	require.NoError(t, os.WriteFile(path, []byte("apiVersion: experiment/v1\nmodel: ./missing.yaml\npolicies: {search: exhaustive}\nruns: 10\n"), 0o600))
	code, _, errOut := gargi(t, "sim", "run", "--local", path)
	require.Equal(t, 1, code)
	require.Contains(t, errOut, "model")
}
