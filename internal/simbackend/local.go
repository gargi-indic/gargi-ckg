package simbackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/internal/sim/experiment"
	"github.com/gargi-indic/gargi-flow/internal/sim/explain"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// Local is a Backend that runs in-process.
type Local struct{}

var _ Backend = (*Local)(nil)

// NewLocal creates a new Local backend.
func NewLocal() *Local {
	return &Local{}
}

func registry() (*engine.Registry, error) {
	reg := engine.NewRegistry()
	if err := nodes.RegisterDefaults(reg, nodes.Dependencies{}); err != nil {
		return nil, err
	}
	if err := nodes.RegisterDecision(reg, firstOption{}); err != nil {
		return nil, err
	}
	return reg, nil
}

// firstOption is a placeholder decision chooser; simulations never call it.
type firstOption struct{}

func (firstOption) Choose(_ context.Context, _ engine.NodeID, _ map[string]any, options []string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("decision has no options")
	}
	return options[0], nil
}

func formatFor(path string) (engine.Format, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return engine.FormatYAML, nil
	case ".json":
		return engine.FormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported file extension %q", filepath.Ext(path))
	}
}

// IssuesOf returns err as sorted validation issues. An error that is not
// *engine.ValidationErrors becomes a single issue at the root path with
// fallback code.
func IssuesOf(err error, fallback engine.ErrorCode) []engine.ValidationIssue {
	var ve *engine.ValidationErrors
	var issues []engine.ValidationIssue
	if errors.As(err, &ve) && ve != nil && ve.Len() > 0 {
		issues = ve.Issues()
	} else {
		issues = []engine.ValidationIssue{{Path: engine.PathRoot.String(), Code: fallback, Message: err.Error()}}
	}
	return issues
}

type policyList []sim.Policy

func (l policyList) Policies(*model.CompiledModel) ([]sim.Policy, error) { return l, nil }

// ValidateModel validates the sim/v1 model at path.
func (l *Local) ValidateModel(_ context.Context, path string) ([]engine.ValidationIssue, error) {
	format, err := formatFor(path)
	if err != nil {
		return IssuesOf(err, engine.CodeInvalidFormat), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	def, err := model.ParseSimModel(data, format)
	if err != nil {
		return IssuesOf(err, engine.CodeInvalidFormat), nil
	}
	reg, err := registry()
	if err != nil {
		return nil, err
	}
	if _, errs := model.Compile(def, filepath.Dir(path), reg); errs.Len() > 0 {
		return IssuesOf(errs, engine.CodeInvalidValue), nil
	}
	return nil, nil
}

// RunExperiment runs the experiment/v1 document at path.
func (l *Local) RunExperiment(ctx context.Context, path string, opts RunOptions) (*RunResult, error) {
	reg, err := registry()
	if err != nil {
		return nil, err
	}
	exp, err := experiment.LoadFile(path, reg)
	if err != nil {
		return nil, err
	}

	var src experiment.PolicySource
	if solver, ok := exp.PolicySource().(experiment.Solver); ok {
		sol, err := solver.Solve(ctx, exp.Model, exp.Worlds)
		if err != nil {
			return nil, err
		}
		src = sol
	}

	var policies []sim.Policy
	if src != nil {
		policies, err = src.Policies(exp.Model)
	} else {
		policies, err = exp.PolicySource().Policies(exp.Model)
		src = policyList(policies)
	}
	if err != nil {
		return nil, err
	}

	runOpts := experiment.Options{Workers: opts.Workers, Top: opts.Top}
	if opts.Progress != nil {
		runOpts.Progress = func(done, total int) { opts.Progress(sim.Progress{Done: done, Total: total}) }
	}

	report, err := experiment.Run(ctx, exp, src, runOpts)
	if err != nil {
		return nil, err
	}

	return &RunResult{
		ExperimentID: "",
		Report:       report,
		Policies:     policies,
	}, nil
}

// Report returns report kind of a stored experiment as JSON.
func (l *Local) Report(_ context.Context, _, _ string) (json.RawMessage, error) {
	return nil, fmt.Errorf("%w: Report", ErrUnsupported)
}

// ExplainPolicy renders a policy in plain language.
func (l *Local) ExplainPolicy(_ context.Context, ref string) (string, error) {
	format := engine.FormatYAML
	if strings.EqualFold(filepath.Ext(ref), ".json") {
		format = engine.FormatJSON
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return "", err
	}
	p, err := model.ParsePolicy(data, format)
	if err != nil {
		return "", err
	}
	return explain.Explain(p), nil
}
