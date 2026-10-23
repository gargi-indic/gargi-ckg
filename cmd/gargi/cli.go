package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/internal/sim/experiment"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/internal/sim/report"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// version is the gargi version, overridable with
// -ldflags "-X main.version=...".
var version = "dev"

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// errServerMode is printed by sim run without --local.
const errServerMode = "server mode arrives in P1; pass --local"

const usage = `usage:
  gargi model validate [--json] <sim.yaml>
  gargi sim run <experiment.yaml> --local [--workers N] [--json] [--out report.json] [--emit-best DIR] [--top K]
  gargi version
`

// run executes the gargi CLI with args (without the program name) and
// returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printf(stderr, "%s", usage)
		return exitUsage
	}
	switch args[0] {
	case "version":
		if len(args) > 1 {
			printf(stderr, "gargi version: unexpected arguments %q\n", args[1:])
			return exitUsage
		}
		printf(stdout, "gargi %s\n", version)
		return exitOK
	case "model":
		if len(args) < 2 || args[1] != "validate" {
			printf(stderr, "%s", usage)
			return exitUsage
		}
		return modelValidate(args[2:], stdout, stderr)
	case "sim":
		if len(args) < 2 || args[1] != "run" {
			printf(stderr, "%s", usage)
			return exitUsage
		}
		return simRun(args[2:], stdout, stderr)
	case "help", "-h", "-help", "--help":
		printf(stdout, "%s", usage)
		return exitOK
	default:
		printf(stderr, "gargi: unknown command %q\n", args[0])
		printf(stderr, "%s", usage)
		return exitUsage
	}
}

// parseInterspersed parses args with fs, allowing flags before and after
// the positional arguments, and returns the positional arguments. Arguments
// after "--" are positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		// fs.Parse consumes a "--" terminator; everything after it is positional.
		consumed := args[:len(args)-len(rest)]
		if len(consumed) > 0 && consumed[len(consumed)-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// parseExit maps a flag parse error to an exit code.
func parseExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	return exitUsage
}

// registry returns a node registry able to compile simulation models: the
// default reference nodes plus a decision node whose chooser the kernel
// replaces with the simulated policy.
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

// formatFor returns the document format of path from its extension.
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

// issuesOf returns err as sorted validation issues. An error that is not
// *engine.ValidationErrors becomes a single issue at the root path with
// fallback code.
func issuesOf(err error, fallback engine.ErrorCode) []engine.ValidationIssue {
	var ve *engine.ValidationErrors
	var issues []engine.ValidationIssue
	if errors.As(err, &ve) && ve != nil && ve.Len() > 0 {
		issues = ve.Issues()
	} else {
		issues = []engine.ValidationIssue{{Path: engine.PathRoot.String(), Code: fallback, Message: err.Error()}}
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Message < issues[j].Message
	})
	return issues
}

// printf writes to w. Write errors on the terminal streams are not
// actionable, so they are ignored; buffered writers report them on Flush.
func printf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

// printIssues writes one "path: code: message" line per issue.
func printIssues(w io.Writer, issues []engine.ValidationIssue) {
	for _, is := range issues {
		printf(w, "%s: %s: %s\n", is.Path, is.Code, is.Message)
	}
}

// writeJSON writes v as indented JSON followed by a newline.
func writeJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// validateModel parses and compiles the simulation model at path and
// returns its validation issues (none when the model is valid).
func validateModel(path string) ([]engine.ValidationIssue, error) {
	format, err := formatFor(path)
	if err != nil {
		return issuesOf(err, engine.CodeInvalidFormat), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	def, err := model.ParseSimModel(data, format)
	if err != nil {
		return issuesOf(err, engine.CodeInvalidFormat), nil
	}
	reg, err := registry()
	if err != nil {
		return nil, err
	}
	if _, errs := model.Compile(def, filepath.Dir(path), reg); errs.Len() > 0 {
		return issuesOf(errs, engine.CodeInvalidValue), nil
	}
	return nil, nil
}

func modelValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gargi model validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "Print the validation report as JSON")
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return parseExit(err)
	}
	if len(files) != 1 {
		printf(stderr, "%s\n", "usage: gargi model validate [--json] <sim.yaml>")
		return exitUsage
	}

	issues, err := validateModel(files[0])
	if err != nil {
		printf(stderr, "gargi: %v\n", err)
		return exitError
	}
	if *asJSON {
		report := struct {
			Errors []engine.ValidationIssue `json:"errors"`
		}{Errors: issues}
		if report.Errors == nil {
			report.Errors = []engine.ValidationIssue{}
		}
		if err := writeJSON(stdout, report); err != nil {
			printf(stderr, "gargi: %v\n", err)
			return exitError
		}
	} else if len(issues) == 0 {
		printf(stdout, "%s\n", "OK")
	} else {
		printIssues(stdout, issues)
	}
	if len(issues) > 0 {
		return exitError
	}
	return exitOK
}

// policyList is a PolicySource over an already enumerated policy list, so
// the CLI enumerates once and can look the best policies up afterwards.
type policyList []sim.Policy

func (l policyList) Policies(*model.CompiledModel) ([]sim.Policy, error) { return l, nil }

func simRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gargi sim run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	local := fs.Bool("local", false, "Run the experiment in-process")
	workers := fs.Int("workers", 0, "Number of workers (default: the experiment's workers, else GOMAXPROCS)")
	top := fs.Int("top", 0, "Number of top policies to report per world (default: the experiment's top, else 5)")
	asJSON := fs.Bool("json", false, "Print the report as JSON")
	out := fs.String("out", "", "Write the JSON report to this file")
	emitBest := fs.String("emit-best", "", "Write each world's best policy as policy/v1 YAML into this directory")
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return parseExit(err)
	}
	if !*local {
		printf(stderr, "%s\n", errServerMode)
		return exitUsage
	}
	if len(files) != 1 {
		printf(stderr, "%s\n", "usage: gargi sim run <experiment.yaml> --local [--workers N] [--json] [--out report.json] [--emit-best DIR] [--top K]")
		return exitUsage
	}
	if *workers < 0 || *top < 0 {
		printf(stderr, "%s\n", "gargi: --workers and --top must not be negative")
		return exitUsage
	}

	reg, err := registry()
	if err != nil {
		printf(stderr, "gargi: %v\n", err)
		return exitError
	}
	exp, err := experiment.LoadFile(files[0], reg)
	if err != nil {
		printIssues(stderr, issuesOf(err, engine.CodeInvalidValue))
		return exitError
	}
	ctx := context.Background()
	// Enumerate or solve once; the policies are kept for --emit-best. A
	// solution (policies: {search: exact}) is passed to Run as is, so the
	// report carries each world's solution.
	var src experiment.PolicySource
	if solver, ok := exp.PolicySource().(experiment.Solver); ok {
		sol, err := solver.Solve(ctx, exp.Model, exp.Worlds)
		if err != nil {
			printIssues(stderr, issuesOf(err, engine.CodeInvalidValue))
			return exitError
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
		printIssues(stderr, issuesOf(err, engine.CodeInvalidValue))
		return exitError
	}
	report, err := experiment.Run(ctx, exp, src, experiment.Options{Workers: *workers, Top: *top})
	if err != nil {
		printIssues(stderr, issuesOf(err, engine.CodeInvalidValue))
		return exitError
	}

	if *out != "" {
		if err := writeReportFile(*out, report); err != nil {
			printf(stderr, "gargi: %v\n", err)
			return exitError
		}
	}
	if *emitBest != "" {
		if err := emitBestPolicies(*emitBest, report, policies); err != nil {
			printf(stderr, "gargi: %v\n", err)
			return exitError
		}
	}
	if *asJSON {
		err = writeJSON(stdout, report)
	} else {
		err = printReport(stdout, report)
	}
	if err != nil {
		printf(stderr, "gargi: %v\n", err)
		return exitError
	}
	return exitOK
}

func writeReportFile(path string, report *experiment.Report) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := writeJSON(f, report); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// emitBestPolicies writes <world>.policy.yaml into dir for the best policy
// of every world of report: the solved optimum when the world has a
// solution, otherwise the top-ranked policy.
func emitBestPolicies(dir string, report *experiment.Report, policies []sim.Policy) error {
	byID := make(map[string]sim.Policy, len(policies))
	for _, p := range policies {
		byID[p.ID()] = p
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, w := range report.Worlds {
		if len(w.Policies) == 0 {
			continue
		}
		name := string(w.World)
		if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
			return fmt.Errorf("world id %q is not a valid file name", name)
		}
		bestID := w.Policies[0].PolicyID
		if w.Solution != nil {
			bestID = w.Solution.PolicyID
		}
		p, ok := byID[bestID]
		if !ok {
			return fmt.Errorf("world %q: best policy %q not found", name, bestID)
		}
		if _, ok := p.(*experiment.TablePolicy); !ok {
			return fmt.Errorf("world %q: --emit-best supports policies from policies.search: exhaustive or exact only; policy %q is not a table policy", name, bestID)
		}
		if err := writePolicyFile(filepath.Join(dir, name+".policy.yaml"), p); err != nil {
			return fmt.Errorf("world %q: %w", name, err)
		}
	}
	return nil
}

func writePolicyFile(path string, p sim.Policy) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := experiment.EmitPolicy(p, f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// printReport writes the human-readable report: per world a table of the
// top policies followed by the best policy's description.
func printReport(w io.Writer, report *experiment.Report) error {
	for i, wr := range report.Worlds {
		if i > 0 {
			printf(w, "\n")
		}
		header := fmt.Sprintf("World %s (model %s, evaluator %s", wr.World, report.Model, report.Evaluator)
		if report.Runs > 0 {
			header += fmt.Sprintf(", %d runs, seed %d", report.Runs, report.Seed)
		}
		printf(w, "%s\n", header+")")

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		printf(tw, "%s\n", "POLICY\tMEAN\tCI95\tP95 DURATION\tEXACT")
		for _, p := range wr.Policies {
			mean, ci, dur := p.Objective, p.Objective, p.Duration
			if p.MonteCarlo != nil {
				// evaluator both: the primary estimates are exact; show the
				// Monte Carlo statistics next to the exact value.
				mean, ci, dur = p.MonteCarlo.Objective, p.MonteCarlo.Objective, p.MonteCarlo.Duration
			}
			exact := "-"
			if p.Objective.Exact {
				exact = fmt.Sprintf("%.1f", p.Objective.Mean)
			}
			printf(tw, "%s\t%.1f\t[%.1f, %.1f]\t%s\t%s\n",
				p.PolicyID, mean.Mean, ci.CI95Low, ci.CI95High, formatSeconds(dur.P95), exact)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		if len(wr.Policies) > 0 {
			// Policies are ranked feasible first, so the first one is the
			// best feasible policy if there is any.
			best := wr.Policies[0]
			if best.IsFeasible() {
				printf(w, "Best policy: %s\n", best.Description)
				if best.Objective.Exact {
					printf(w, "Exact EV: %.1f\n", best.Objective.Mean)
				}
			} else {
				printf(w, "%s\n", "Best policy: none is feasible (every policy violates a requirement)")
			}
		}
		if wr.Solution != nil {
			if err := printSolution(w, wr.Solution); err != nil {
				return err
			}
		}
	}
	if report.Reports != nil {
		return printReports(w, report.Reports)
	}
	return nil
}

// printReports writes one compact table per requested report.
func printReports(w io.Writer, rs *experiment.ReportSet) error {
	if r := rs.Regret; r != nil {
		printf(w, "\n%s\n", "Regret (rows: policy tuned for a world; columns: value (regret) in each world)")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		header := []string{"TUNED FOR", "POLICY"}
		for _, world := range r.Worlds {
			header = append(header, strings.ToUpper(string(world)))
		}
		printf(tw, "%s\n", strings.Join(append(header, "MAX REGRET"), "\t"))
		for _, row := range r.Rows {
			cells := []string{string(row.TunedFor), row.PolicyID}
			for _, c := range row.Cells {
				cell := fmt.Sprintf("%.1f", c.Value)
				if c.Regret != nil {
					cell += fmt.Sprintf(" (%.1f)", *c.Regret)
				}
				if !c.Feasible {
					cell += " infeasible"
				}
				cells = append(cells, cell)
			}
			printf(tw, "%s\n", strings.Join(append(cells, fmt.Sprintf("%.1f", row.MaxRegret)), "\t"))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		for _, world := range r.NoFeasiblePolicy {
			printf(w, "World %s has no feasible policy.\n", world)
		}
	}
	if r := rs.Robust; r != nil {
		worlds := make([]string, 0, len(r.Prior))
		for world := range r.Prior {
			worlds = append(worlds, string(world))
		}
		sort.Strings(worlds)
		prior := make([]string, len(worlds))
		for i, world := range worlds {
			prior[i] = fmt.Sprintf("%s=%.2f", world, r.Prior[sim.WorldID(world)])
		}
		printf(w, "\nRobust (prior %s; CVaR alpha %g; %d eligible policies)\n", strings.Join(prior, " "), r.CVaRAlpha, r.Eligible)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		printf(tw, "%s\n", "CRITERION\tPOLICY\tSCORE\tEXPECTED VALUE\tWORST CASE\tCVAR")
		for _, c := range []struct {
			name string
			pick *report.RobustPick
		}{{"expected value", r.ExpectedValue}, {"maximin", r.Maximin}, {"cvar", r.CVaR}} {
			if c.pick == nil {
				printf(tw, "%s\t%s\n", c.name, "none (no policy is feasible in every world)")
				continue
			}
			s := c.pick.Scores
			printf(tw, "%s\t%s\t%.1f\t%.1f\t%.1f\t%.1f\n", c.name, c.pick.PolicyID, c.pick.Score, s.ExpectedValue, s.WorstCase, s.CVaR)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	for _, p := range rs.Pareto {
		for _, pw := range p.Worlds {
			printf(w, "\nPareto frontier, world %s (x: %s %s, y: %s %s)\n", pw.World, p.X.Metric, p.X.Goal, p.Y.Metric, p.Y.Goal)
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			printf(tw, "%s\n", "POLICY\tX\tY")
			for _, pt := range pw.Points {
				printf(tw, "%s\t%.4g\t%.4g\n", pt.PolicyID, pt.X, pt.Y)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
		}
	}
	return nil
}

// printSolution writes a world's exact solution: the optimal policy, its EV
// and the action values of every information set the optimal policy reaches
// (the JSON report lists every information set).
func printSolution(w io.Writer, s *experiment.WorldSolution) error {
	printf(w, "Optimal policy (exact solver, %d histories): %s\n", s.Histories, s.Description)
	printf(w, "Optimal EV: %.1f\n", s.EV)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	printf(tw, "%s\n", "INFORMATION SET\tREACH\tCHOICE\tACTION VALUES")
	for _, is := range s.InformationSets {
		if is.ReachProbability <= 0 {
			continue
		}
		values := make([]string, len(is.Actions))
		for i, a := range is.Actions {
			values[i] = fmt.Sprintf("%s=%.1f", a.Option, a.Value)
		}
		printf(tw, "%s\t%.4f\t%s\t%s\n", is.InfoSet, is.ReachProbability, is.Choice, strings.Join(values, " "))
	}
	return tw.Flush()
}

// formatSeconds formats a duration in seconds, rounded to the second.
func formatSeconds(s float64) string {
	return time.Duration(s * float64(time.Second)).Round(time.Second).String()
}
