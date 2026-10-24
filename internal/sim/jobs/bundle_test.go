package jobs_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/experiment"
	"github.com/gargi-indic/gargi-flow/internal/sim/jobs"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

func newRegistry(t *testing.T) *engine.Registry {
	t.Helper()
	reg, err := jobs.NewRegistry()
	require.NoError(t, err)
	return reg
}

// requireSorted asserts issues are sorted by path, code and message.
func requireSorted(t *testing.T, issues []engine.ValidationIssue) {
	t.Helper()
	require.True(t, slices.IsSortedFunc(issues, func(a, b engine.ValidationIssue) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := strings.Compare(string(a.Code), string(b.Code)); c != 0 {
			return c
		}
		return strings.Compare(a.Message, b.Message)
	}), "issues not sorted: %+v", issues)
}

func paths(issues []engine.ValidationIssue) []string {
	out := make([]string, len(issues))
	for i, is := range issues {
		out[i] = is.Path
	}
	return out
}

func TestBundleFS(t *testing.T) {
	b := loanBundle(t)
	fsys := jobs.NewBundleFS(b, engine.FormatYAML)

	data, err := fs.ReadFile(fsys, "sim.yaml")
	require.NoError(t, err)
	require.Equal(t, b.Sim, data)
	// The graph and policies resolve by base name, whatever the directory.
	for name, want := range map[string][]byte{
		"graph.yaml":                   b.Graph,
		"some/dir/graph.yaml":          b.Graph,
		"policies/recession-v1.yaml":   b.Policies["recession-v1.yaml"],
		"elsewhere/recession-v1.yaml":  b.Policies["recession-v1.yaml"],
		"deeper/a/b/recession-v1.yaml": b.Policies["recession-v1.yaml"],
	} {
		got, err := fs.ReadFile(fsys, name)
		require.NoError(t, err, name)
		require.Equal(t, want, got, name)
	}

	f, err := fsys.Open("policies/recession-v1.yaml")
	require.NoError(t, err)
	info, err := f.Stat()
	require.NoError(t, err)
	require.Equal(t, "recession-v1.yaml", info.Name())
	require.EqualValues(t, len(b.Policies["recession-v1.yaml"]), info.Size())
	require.False(t, info.IsDir())
	all, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, b.Policies["recession-v1.yaml"], all)
	require.NoError(t, f.Close())

	_, err = fsys.Open("missing.yaml")
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = fs.ReadFile(fsys, "sim.json")
	require.ErrorIs(t, err, fs.ErrNotExist, "the sim document is served under its format's name only")
	_, err = fsys.Open("../graph.yaml")
	require.ErrorIs(t, err, fs.ErrInvalid)

	// ReadFile returns a copy.
	data[0] = 'X'
	again, err := fs.ReadFile(fsys, "sim.yaml")
	require.NoError(t, err)
	require.NotEqual(t, byte('X'), again[0])
}

func TestValidateModel_Loan(t *testing.T) {
	m, errs := jobs.ValidateModel(loanBundle(t), newRegistry(t))
	require.Zero(t, errs.Len(), "%v", errs)
	require.Equal(t, "loan-underwriting", m.Def.ID)
}

func TestValidateModel_ReportsEverySortedIssue(t *testing.T) {
	reg := newRegistry(t)
	b := loanBundle(t)
	b.Policies = map[string][]byte{
		"bad.txt":         []byte("x"),
		"broken.yaml":     []byte("apiVersion: policy/v1\nid: loan-underwriting\nversion: v\ndecisions:\n  nope:\n    rules:\n      - else: score\n"),
		"recession.yaml":  b.Policies["recession-v1.yaml"],
		"unparsable.json": []byte("{"),
	}
	_, errs := jobs.ValidateModel(b, reg)
	require.NotNil(t, errs)
	issues := errs.Issues()
	requireSorted(t, issues)
	got := paths(issues)
	require.Contains(t, got, "$.policies['bad.txt']")
	require.Contains(t, got, "$.policies['unparsable.json']:$")
	broken := false
	for _, p := range got {
		if strings.HasPrefix(p, "$.policies['broken.yaml']:") {
			broken = true
		}
		require.NotContains(t, p, "recession.yaml", "the valid policy has no issue")
	}
	require.True(t, broken, "issues inside a policy document are prefixed: %v", got)

	// A broken graph is reported at the graph paths `gargi model validate`
	// uses.
	b = loanBundle(t)
	b.Graph = []byte("apiVersion: graph/v1\nnodes: [")
	_, errs = jobs.ValidateModel(b, reg)
	require.NotNil(t, errs)
	require.True(t, strings.HasPrefix(errs.Issues()[0].Path, "$.graph"), "%v", errs)

	// An unknown sim format and an unparsable sim document.
	b = loanBundle(t)
	b.Format = "toml"
	_, errs = jobs.ValidateModel(b, reg)
	require.Equal(t, []string{"$"}, paths(errs.Issues()))
	require.Equal(t, engine.CodeInvalidFormat, errs.Issues()[0].Code)
	b = loanBundle(t)
	b.Sim = []byte("apiVersion: sim/v1\nid: [")
	_, errs = jobs.ValidateModel(b, reg)
	require.Positive(t, errs.Len())
}

func TestCompileExperiment_ResolvesFromBundle(t *testing.T) {
	reg := newRegistry(t)
	b := loanBundle(t)
	// The experiment's model path is ignored and policies.files resolves to
	// the bundle by base name.
	exp, err := jobs.CompileExperiment(b, loanExperiment("exact", 1, "{files: [./policies/recession-v1.yaml]}"), "yaml", reg)
	require.NoError(t, err)
	require.Equal(t, "sim.yaml", exp.Def.Model)
	require.NotNil(t, exp.FS)
	policies, err := exp.PolicySource().Policies(exp.Model)
	require.NoError(t, err)
	require.Len(t, policies, 1)

	// A document without a model field is fine too, also in JSON.
	js := []byte(`{"apiVersion":"experiment/v1","worlds":["base"],"policies":{"search":"exhaustive"},"evaluator":"exact","runs":1,"seed":1,"top":1}`)
	exp, err = jobs.CompileExperiment(b, js, "json", reg)
	require.NoError(t, err)
	require.Len(t, exp.Worlds, 1)

	// The compiled experiment runs entirely from memory.
	report, err := experiment.Run(context.Background(), exp, exp.PolicySource(), experiment.Options{Workers: 2})
	require.NoError(t, err)
	require.InDelta(t, 460.0, report.Worlds[0].Policies[0].Objective.Mean, 1e-9)
}

func TestCompileExperiment_ValidationErrors(t *testing.T) {
	reg := newRegistry(t)
	b := loanBundle(t)

	_, err := jobs.CompileExperiment(b, []byte("apiVersion: experiment/v1\nworlds: [base]\npolicies: {search: exhaustive}\nevaluator: nope\nruns: 0\n"), "yaml", reg)
	var ve *engine.ValidationErrors
	require.ErrorAs(t, err, &ve)
	requireSorted(t, ve.Issues())
	require.Equal(t, []string{"$.evaluator", "$.runs"}, paths(ve.Issues()))

	_, err = jobs.CompileExperiment(b, loanExperiment("exact", 1, exhaustive), "toml", reg)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, engine.CodeInvalidFormat, ve.Issues()[0].Code)

	// Unknown worlds are reported by the compiler.
	_, err = jobs.CompileExperiment(b, []byte("apiVersion: experiment/v1\nworlds: [mars]\npolicies: {search: exhaustive}\nevaluator: exact\nruns: 1\ntop: 1\n"), "yaml", reg)
	require.ErrorAs(t, err, &ve)
	require.Contains(t, paths(ve.Issues()), "$.worlds[0]")

	// policies.policy resolves against the bundle as well.
	_, err = jobs.CompileExperiment(b, []byte("apiVersion: experiment/v1\nworlds: [base]\npolicies: {search: grid, policy: ./policies/absent.yaml}\nevaluator: exact\nruns: 1\ntop: 1\n"), "yaml", reg)
	require.ErrorAs(t, err, &ve)
	require.Equal(t, "$.policies.policy", ve.Issues()[0].Path)
	require.Equal(t, engine.CodeUnresolvedReference, ve.Issues()[0].Code)
}

func TestReplay(t *testing.T) {
	reg := newRegistry(t)
	b := loanBundle(t)
	exp, err := jobs.CompileExperiment(b, loanExperiment("both", 10, exhaustive), "yaml", reg)
	require.NoError(t, err)
	report, err := experiment.Run(context.Background(), exp, exp.PolicySource(), experiment.Options{Workers: 2})
	require.NoError(t, err)

	ctx := context.Background()
	got, err := jobs.Replay(ctx, exp, jobs.ReplayRequest{Index: 3, Report: report, ExecutionID: "replay-1"})
	require.NoError(t, err)
	require.Equal(t, sim.WorldID("base"), got.World)
	require.Equal(t, report.Worlds[0].Policies[0].PolicyID, got.PolicyID, "defaults to the best policy of the first world")
	require.Equal(t, sim.Seed(42, "base", 3), got.Seed)
	require.Equal(t, 3, got.Record.Replication)
	require.NotEmpty(t, got.Events)
	require.NotEmpty(t, got.Record.Path)
	for i, ev := range got.Events {
		require.Equal(t, engine.ExecutionID("replay-1"), ev.ExecutionID())
		if i > 0 {
			require.Greater(t, ev.Sequence(), got.Events[i-1].Sequence())
		}
	}

	// Replays are deterministic.
	again, err := jobs.Replay(ctx, exp, jobs.ReplayRequest{Index: 3, Report: report, ExecutionID: "replay-1"})
	require.NoError(t, err)
	require.Equal(t, got.Record, again.Record)
	require.Len(t, again.Events, len(got.Events))

	// An explicit world and policy.
	rec := report.Worlds[1].Policies[1].PolicyID
	other, err := jobs.Replay(ctx, exp, jobs.ReplayRequest{World: "recession", Policy: rec, Index: 0})
	require.NoError(t, err)
	require.Equal(t, sim.WorldID("recession"), other.World)
	require.Equal(t, rec, other.PolicyID)

	for name, tc := range map[string]struct {
		req  jobs.ReplayRequest
		want error
	}{
		"negative index":   {jobs.ReplayRequest{Index: -1, Report: report}, jobs.ErrRunIndex},
		"index past runs":  {jobs.ReplayRequest{Index: 10, Report: report}, jobs.ErrRunIndex},
		"unknown world":    {jobs.ReplayRequest{World: "mars", Report: report}, jobs.ErrUnknownWorld},
		"unknown policy":   {jobs.ReplayRequest{Policy: "nope"}, jobs.ErrUnknownPolicy},
		"no report":        {jobs.ReplayRequest{}, jobs.ErrNoReport},
		"world not ranked": {jobs.ReplayRequest{Report: &experiment.Report{}}, jobs.ErrNoReport},
	} {
		_, err := jobs.Replay(ctx, exp, tc.req)
		require.True(t, errors.Is(err, tc.want), "%s: error %v, want %v", name, err, tc.want)
	}
	_, err = jobs.Replay(ctx, nil, jobs.ReplayRequest{})
	require.Error(t, err)
}
