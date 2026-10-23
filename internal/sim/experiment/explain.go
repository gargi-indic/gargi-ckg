package experiment

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gargi-indic/gargi-flow/internal/sim/kernel"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/internal/sim/report"
	"github.com/gargi-indic/gargi-flow/internal/sim/sampler"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// This file computes the reports that explain why a policy wins: voi,
// flip_point, path_heatmap and policy_tree. They need the exact solver or
// the kernel, so they live here; their JSON types are in internal/sim/report.

const (
	// FlipSweepPoints is the number of evenly spaced points, from lo to hi
	// inclusive, at which a flip_point report solves the base world.
	FlipSweepPoints = 21
	// FlipTolerance is the relative width at which a flip_point bisection
	// stops: an interval of at most FlipTolerance·(hi − lo).
	FlipTolerance = 1e-6
	// FlipMaxIterations caps the bisection steps of one flip.
	FlipMaxIterations = 60
)

// validateExplainReport checks a voi, flip_point, path_heatmap or
// policy_tree entry of reports against the model and the worlds, adding
// issues at at. voi, flip_point and policy_tree need a discrete model; its
// reasons are reported once per kind (reported). A flip_point without an
// explicit range gets the minimum and maximum of its param across the
// experiment's worlds in spec.Lo and spec.Hi. It reports whether the entry
// is valid.
func validateExplainReport(exp *CompiledExperiment, spec *report.Spec, at string, errs *engine.ValidationErrors, reported map[report.Kind]bool) bool {
	ok := true
	if spec.NeedsSolver() {
		if err := requireDiscrete(exp.Model, string(spec.Kind)+" report"); err != nil {
			ok = false
			var ve *engine.ValidationErrors
			if !reported[spec.Kind] && errors.As(err, &ve) {
				reported[spec.Kind] = true
				for _, is := range ve.Issues() {
					errs.Add(is.Path, is.Code, is.Message)
				}
			}
		}
	}
	switch spec.Kind {
	case report.KindVOI:
		exists, observed := false, false
		for _, n := range exp.Model.Graph.Nodes {
			exists = exists || string(n.ID) == spec.Node
			if n.Type == "decision" {
				for _, path := range observesOf(n) {
					observed = observed || observesNode(path, spec.Node)
				}
			}
		}
		switch {
		case !exists:
			errs.Add(at, engine.CodeUnresolvedReference, fmt.Sprintf("voi: unknown node %q", spec.Node))
			ok = false
		case !observed:
			errs.Add(at, engine.CodeInvalidValue, fmt.Sprintf("voi: node %q is not observed by any decision (no observes path starts with %q)", spec.Node, spec.Node+"."))
			ok = false
		}
	case report.KindFlipPoint:
		lo, hi := math.Inf(1), math.Inf(-1)
		numeric := true
		for _, world := range exp.Worlds {
			raw, found := world.Params[spec.Param]
			v, isNum := numericParam(raw)
			if !found || !isNum {
				what := "is missing"
				if found {
					what = fmt.Sprintf("is %v, not a number", raw)
				}
				errs.Add(at, engine.CodeInvalidValue, fmt.Sprintf("flip_point: param %q of world %q %s; the swept param must be a number in every experiment world", spec.Param, world.ID, what))
				numeric = false
				continue
			}
			lo, hi = min(lo, v), max(hi, v)
		}
		if !numeric {
			return false
		}
		if len(exp.Worlds) == 0 {
			return ok // the missing worlds are reported by Compile
		}
		if !spec.HasRange {
			if !(lo < hi) {
				errs.Add(at, engine.CodeInvalidValue, fmt.Sprintf("flip_point: the sweep range needs lo < hi, but every experiment world has %s = %g; give a range: flip_point:%s:<lo>,<hi>", spec.Param, lo, spec.Param))
				return false
			}
			spec.Lo, spec.Hi = lo, hi
		}
	}
	return ok
}

// observesOf returns the observes paths of a decision node.
func observesOf(n engine.NodeDefinition) []string {
	raw, _ := n.Config["observes"].([]any)
	out := make([]string, 0, len(raw))
	for _, o := range raw {
		if s, ok := o.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// observesNode reports whether the observes path reads node (its first
// segment is node).
func observesNode(path, node string) bool {
	first, _, _ := strings.Cut(path, ".")
	return first == node
}

// numericParam returns a world param as a float64 when it is a number.
func numericParam(v any) (float64, bool) {
	n, err := normalizeObserved(v)
	if err != nil {
		return 0, false
	}
	f, ok := n.(float64)
	return f, ok && !math.IsNaN(f) && !math.IsInf(f, 0)
}

// hasExplainReports reports whether exp requests a voi, flip_point,
// path_heatmap or policy_tree report.
func hasExplainReports(exp *CompiledExperiment) bool {
	for _, r := range exp.Reports {
		switch r.Kind {
		case report.KindVOI, report.KindFlipPoint, report.KindPathHeatmap, report.KindPolicyTree:
			return true
		}
	}
	return false
}

// explainer computes the explain reports of one Run.
type explainer struct {
	exp      *CompiledExperiment
	worlds   []WorldReport
	policies []sim.Policy
	// solution is the exact solution of the experiment's worlds: the one
	// Run solved (policies: {search: exact}) or, lazily, a new one.
	solution *Solution
	useExact bool
	workers  int
}

// buildExplainReports adds the requested voi, flip_point, path_heatmap and
// policy_tree reports to set, in request order. worlds are the ranked world
// reports, policies the evaluated policies and solution the solution Run
// computed (nil unless the policy source is a Solver).
func buildExplainReports(ctx context.Context, set *ReportSet, x *explainer) error {
	for _, r := range x.exp.Reports {
		var err error
		switch r.Kind {
		case report.KindVOI:
			var v []report.VOI
			if v, err = x.valueOfInformation(ctx, r.Node); err == nil {
				set.VOI = append(set.VOI, v...)
			}
		case report.KindFlipPoint:
			var f *report.FlipPoint
			if f, err = x.flipPoint(ctx, r); err == nil {
				set.FlipPoints = append(set.FlipPoints, *f)
			}
		case report.KindPathHeatmap:
			set.PathHeatmap, err = x.pathHeatmap(ctx)
		case report.KindPolicyTree:
			set.PolicyTree, err = x.policyTree(ctx)
		default:
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("experiment: %s report: %w", r.Kind, err)
		}
	}
	return nil
}

// solved returns the exact solution of the experiment's worlds.
func (x *explainer) solved(ctx context.Context) (*Solution, error) {
	if x.solution == nil {
		sol, err := ExactSource{}.Solve(ctx, x.exp.Model, x.exp.Worlds)
		if err != nil {
			return nil, err
		}
		x.solution = sol
	}
	return x.solution, nil
}

// valueOfInformation solves every world with and without observing node.
func (x *explainer) valueOfInformation(ctx context.Context, node string) ([]report.VOI, error) {
	with, err := x.solved(ctx)
	if err != nil {
		return nil, err
	}
	m, err := WithoutObservation(x.exp.Model, node)
	if err != nil {
		return nil, err
	}
	without, err := ExactSource{}.Solve(ctx, m, x.exp.Worlds)
	if err != nil {
		return nil, err
	}
	out := make([]report.VOI, len(x.exp.Worlds))
	for w := range x.exp.Worlds {
		a, b := with.Worlds[w], without.Worlds[w]
		out[w] = report.VOI{
			World:              a.World,
			Node:               node,
			WithObservation:    report.PolicyValue{PolicyID: a.PolicyID, EV: a.EV},
			WithoutObservation: report.PolicyValue{PolicyID: b.PolicyID, EV: b.EV},
			VOI:                a.EV - b.EV,
		}
	}
	return out, nil
}

// WithoutObservation returns a new compiled model equal to m except that
// no decision observes node and running node is free: every observes path
// whose first segment is node is removed from every decision, and node's
// behavior cost is removed (its outputs, duration, failure and abandonment
// are kept). The model is recompiled from copies of m's definition and
// graph; m is not modified.
func WithoutObservation(m *model.CompiledModel, node string) (*model.CompiledModel, error) {
	if m == nil || m.Graph == nil || m.Def == nil {
		return nil, errors.New("experiment: compiled model is nil")
	}
	g := *m.Graph
	g.Nodes = slices.Clone(m.Graph.Nodes)
	for i, n := range g.Nodes {
		raw, ok := n.Config["observes"].([]any)
		if n.Type != "decision" || !ok {
			continue
		}
		kept := make([]any, 0, len(raw))
		for _, o := range raw {
			if s, ok := o.(string); ok && observesNode(s, node) {
				continue
			}
			kept = append(kept, o)
		}
		cfg := maps.Clone(n.Config)
		cfg["observes"] = kept
		g.Nodes[i].Config = cfg
	}
	def := *m.Def
	def.Behaviors = maps.Clone(m.Def.Behaviors)
	if b, ok := def.Behaviors[node]; ok {
		b.Cost = nil
		def.Behaviors[node] = b
	}
	out, errs := model.CompileGraph(&def, &g, m.Registry)
	if errs.Len() > 0 {
		return nil, fmt.Errorf("experiment: compile the model without observing %q: %w", node, errs)
	}
	return out, nil
}

// flipPoint sweeps spec.Param of the base world (the experiment's first
// world) over [spec.Lo, spec.Hi] and bisects every change of the optimal
// policy.
func (x *explainer) flipPoint(ctx context.Context, spec report.Spec) (*report.FlipPoint, error) {
	base := x.exp.Worlds[0]
	lo, hi := spec.Lo, spec.Hi
	describe := map[string]string{}
	solve := func(values ...float64) ([]report.FlipSample, error) {
		worlds := make([]sim.World, len(values))
		for i, v := range values {
			params := maps.Clone(base.Params)
			if params == nil {
				params = map[string]any{}
			}
			params[spec.Param] = v
			id := fmt.Sprintf("%s@%s=%s", base.ID, spec.Param, strconv.FormatFloat(v, 'g', -1, 64))
			worlds[i] = sim.World{ID: sim.WorldID(id), Params: params}
		}
		sol, err := ExactSource{}.Solve(ctx, x.exp.Model, worlds)
		if err != nil {
			return nil, err
		}
		out := make([]report.FlipSample, len(values))
		for i, ws := range sol.Worlds {
			describe[ws.PolicyID] = ws.Description
			out[i] = report.FlipSample{Value: values[i], PolicyID: ws.PolicyID, EV: ws.EV}
		}
		return out, nil
	}

	values := make([]float64, FlipSweepPoints)
	for i := range values {
		values[i] = lo + (hi-lo)*float64(i)/float64(FlipSweepPoints-1)
	}
	values[len(values)-1] = hi
	sweep, err := solve(values...)
	if err != nil {
		return nil, err
	}

	tol := FlipTolerance * (hi - lo)
	// bisect narrows [l, r] (different optimal policies at its ends) to
	// width tol. A third policy found at a midpoint splits the interval in
	// two, each bisected on its own.
	var bisect func(l, r report.FlipSample, iter int) ([]report.Flip, error)
	bisect = func(l, r report.FlipSample, iter int) ([]report.Flip, error) {
		for ; iter < FlipMaxIterations && r.Value-l.Value > tol; iter++ {
			mid := l.Value + (r.Value-l.Value)/2
			if mid <= l.Value || mid >= r.Value {
				break
			}
			s, err := solve(mid)
			if err != nil {
				return nil, err
			}
			switch m := s[0]; m.PolicyID {
			case l.PolicyID:
				l = m
			case r.PolicyID:
				r = m
			default:
				left, err := bisect(l, m, iter+1)
				if err != nil {
					return nil, err
				}
				right, err := bisect(m, r, iter+1)
				if err != nil {
					return nil, err
				}
				return append(left, right...), nil
			}
		}
		return []report.Flip{{From: describe[l.PolicyID], To: describe[r.PolicyID], Left: l, Right: r}}, nil
	}

	fp := &report.FlipPoint{Param: spec.Param, BaseWorld: base.ID, Lo: lo, Hi: hi, Sweep: sweep, Flips: []report.Flip{}}
	for i := 0; i+1 < len(sweep); i++ {
		if sweep[i].PolicyID == sweep[i+1].PolicyID {
			continue
		}
		flips, err := bisect(sweep[i], sweep[i+1], 0)
		if err != nil {
			return nil, err
		}
		fp.Flips = append(fp.Flips, flips...)
	}
	return fp, nil
}

// policyTree renders every world's exact solution as a tree.
func (x *explainer) policyTree(ctx context.Context) ([]report.PolicyTree, error) {
	sol, err := x.solved(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]report.PolicyTree, len(sol.Worlds))
	for w, ws := range sol.Worlds {
		sets := make([]report.TreeSet, len(ws.InformationSets))
		for i, is := range ws.InformationSets {
			s := report.TreeSet{
				InfoSet:          is.InfoSet,
				Decision:         is.Decision,
				Observed:         is.Observed,
				ReachProbability: is.ReachProbability,
				Choice:           is.Choice,
				Options:          make([]string, len(is.Actions)),
				Values:           make([]float64, len(is.Actions)),
			}
			if is.Parent != nil {
				s.HasParent, s.ParentInfoSet, s.ParentOption = true, is.Parent.InfoSet, is.Parent.Option
			}
			for a, act := range is.Actions {
				s.Options[a], s.Values[a] = act.Option, act.Value
			}
			sets[i] = s
		}
		tree, err := report.NewPolicyTree(ws.World, ws.PolicyID, ws.EV, sets)
		if err != nil {
			return nil, fmt.Errorf("world %q: %w", ws.World, err)
		}
		out[w] = *tree
	}
	return out, nil
}

// pathHeatmap traces each world's best policy: with the exact evaluator
// (and both) every chance branch weighted by its probability, with Monte
// Carlo the experiment's replications (same seeds) counted.
func (x *explainer) pathHeatmap(ctx context.Context) ([]report.PathHeatmap, error) {
	byID := make(map[string]sim.Policy, len(x.policies))
	for _, p := range x.policies {
		byID[p.ID()] = p
	}
	g := x.exp.Model.Graph
	out := make([]report.PathHeatmap, len(x.exp.Worlds))
	for w, world := range x.exp.Worlds {
		if len(x.worlds[w].Policies) == 0 {
			return nil, fmt.Errorf("world %q has no ranked policy", world.ID)
		}
		id := x.worlds[w].Policies[0].PolicyID
		p, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("world %q: best policy %q not found", world.ID, id)
		}
		var nodes, edges []float64
		var err error
		if x.useExact {
			nodes, edges, err = exactTraffic(ctx, x.exp.Model, world, p)
		} else {
			nodes, edges, err = monteCarloTraffic(ctx, x.exp, world, p, x.workers)
		}
		if err != nil {
			return nil, err
		}
		hm := report.PathHeatmap{
			World:    world.ID,
			PolicyID: id,
			Nodes:    make([]report.NodeMass, len(g.Nodes)),
			Edges:    make([]report.EdgeMass, len(g.Edges)),
		}
		for i, n := range g.Nodes {
			hm.Nodes[i] = report.NodeMass{ID: string(n.ID), Mass: nodes[i]}
		}
		for i, ed := range g.Edges {
			hm.Edges[i] = report.EdgeMass{From: string(ed.From), To: string(ed.To), Index: i, Mass: edges[i]}
		}
		out[w] = hm
	}
	return out, nil
}

// exactTraffic enumerates every chance branch of p in world and returns the
// probability that each graph node starts and each graph edge is traversed.
func exactTraffic(ctx context.Context, m *model.CompiledModel, world sim.World, p sim.Policy) ([]float64, []float64, error) {
	nodes, edges := make([]float64, len(m.Graph.Nodes)), make([]float64, len(m.Graph.Edges))
	total := 0.0
	enum := sampler.NewEnumerator()
	for branch := 0; enum.Next(); branch++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		rec, err := kernel.RunEpisode(ctx, m, world, p, enum.Sampler(), kernel.Options{Trace: true})
		if err != nil {
			return nil, nil, fmt.Errorf("world %q policy %q branch %d: %w", world.ID, p.ID(), branch, err)
		}
		wt := enum.Weight()
		total += wt
		for _, i := range rec.Started {
			nodes[i] += wt
		}
		for _, i := range rec.Edges {
			edges[i] += wt
		}
	}
	if math.Abs(total-1) > weightTolerance {
		return nil, nil, fmt.Errorf("world %q policy %q: branch weights sum to %.17g, want 1", world.ID, p.ID(), total)
	}
	for i := range nodes {
		nodes[i] /= total
	}
	for i := range edges {
		edges[i] /= total
	}
	return nodes, edges, nil
}

// monteCarloTraffic runs the experiment's Monte Carlo replications of p in
// world (replication r uses sim.Seed(seed, world, r), as the evaluator
// does) on a pool of workers and returns the fraction of runs in which each
// node starts and each edge is traversed. Counts are integers merged per
// chunk, so the result does not depend on the worker count.
func monteCarloTraffic(parent context.Context, exp *CompiledExperiment, world sim.World, p sim.Policy, workers int) ([]float64, []float64, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	m := exp.Model
	runs := exp.Def.Runs
	nn, ne := len(m.Graph.Nodes), len(m.Graph.Edges)
	chunks := (runs + ChunkSize - 1) / ChunkSize

	var (
		mu       sync.Mutex
		nodes    = make([]int64, nn)
		edges    = make([]int64, ne)
		next     atomic.Int64
		wg       sync.WaitGroup
		firstErr error
	)
	for range min(workers, chunks) {
		wg.Go(func() {
			for ctx.Err() == nil {
				c := int(next.Add(1) - 1)
				if c >= chunks {
					return
				}
				cn, ce := make([]int64, nn), make([]int64, ne)
				var err error
				for r := c * ChunkSize; r < min((c+1)*ChunkSize, runs) && err == nil; r++ {
					var rec sim.RunRecord
					rec, err = kernel.RunEpisode(ctx, m, world, p, sampler.NewMonteCarlo(sim.Seed(exp.Def.Seed, world.ID, r)), kernel.Options{Trace: true})
					if err != nil {
						err = fmt.Errorf("world %q policy %q replication %d: %w", world.ID, p.ID(), r, err)
						break
					}
					for _, i := range rec.Started {
						cn[i]++
					}
					for _, i := range rec.Edges {
						ce[i]++
					}
				}
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
						cancel()
					}
				} else {
					for i := range cn {
						nodes[i] += cn[i]
					}
					for i := range ce {
						edges[i] += ce[i]
					}
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	if firstErr != nil {
		return nil, nil, firstErr
	}
	fn, fe := make([]float64, nn), make([]float64, ne)
	for i, c := range nodes {
		fn[i] = float64(c) / float64(runs)
	}
	for i, c := range edges {
		fe[i] = float64(c) / float64(runs)
	}
	return fn, fe, nil
}
