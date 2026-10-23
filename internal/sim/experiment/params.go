package experiment

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"

	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// randomStream is the PCG stream of random search ("gridsear" in ASCII).
const randomStream = 0x6772696473656172

const (
	// gridEpsilon absorbs float noise in the number of grid steps of a
	// range.
	gridEpsilon = 1e-9
	// maxGridPoints caps the grid points random search draws from, so the
	// index fits an int64.
	maxGridPoints = 1 << 62
)

// GridSource is a PolicySource (policies: {search: grid, policy: ...}) that
// evaluates every combination of the params of a parameterized policy.
//
// Params are taken in name order, the last name varying fastest. A range
// param contributes min + i*step for i = 0..floor((max-min)/step + 1e-9),
// each rounded to 12 significant digits (so 0.1*3 is 0.3) and never above
// max; a values-list param contributes its values in document order. A
// range param without step is an error. Every candidate is
// Policy.WithParams of one combination, so all candidates share the
// template's compiled CEL programs. A policy without params yields one
// candidate, the template itself.
type GridSource struct {
	// Policy is the template, compiled against the model passed to
	// Policies.
	Policy model.Parameterized
	// MaxPolicies caps the number of candidates; more is an error. Zero
	// selects DefaultMaxPolicies.
	MaxPolicies int
}

var _ PolicySource = GridSource{}

// RandomSource is a PolicySource (policies: {search: random, policy: ...,
// samples: N, seed: S}) that draws Samples random assignments of the params
// of a parameterized policy.
//
// Draws come from rand.New(rand.NewPCG(Seed, 0x6772696473656172)). For each
// candidate the params are drawn in name order: a range param with step
// picks one of its grid points (as GridSource) uniformly, a range param
// without step draws uniformly from [min, max] (rounded like the grid), and
// a values-list param picks one of its values uniformly. A candidate whose
// ID equals an earlier one is dropped, so fewer than Samples candidates can
// be returned. The same Seed always yields the same candidates in the same
// order.
type RandomSource struct {
	// Policy is the template, compiled against the model passed to
	// Policies.
	Policy model.Parameterized
	// Samples is the number of draws (>= 1).
	Samples int
	// Seed seeds the draws.
	Seed uint64
}

var _ PolicySource = RandomSource{}

// GridSource returns a GridSource over the experiment's policies.policy with
// the experiment's max_policies.
func (e *CompiledExperiment) GridSource() GridSource {
	return GridSource{Policy: e.Policy, MaxPolicies: e.Def.MaxPolicies}
}

// RandomSource returns a RandomSource over the experiment's policies.policy
// with policies.samples draws, seeded with policies.seed or, when unset,
// the experiment seed.
func (e *CompiledExperiment) RandomSource() RandomSource {
	seed := e.Def.Seed
	if e.Def.Policies.Seed != nil {
		seed = *e.Def.Policies.Seed
	}
	return RandomSource{Policy: e.Policy, Samples: e.Def.Policies.Samples, Seed: seed}
}

// policyParamPath is the location of a param of the policies.policy
// document, in the "$.policies.policy:$.params.<name>" convention.
func policyParamPath(name string) engine.Path {
	return engine.Path(engine.PathRoot.Field("policies").Field("policy").String() + ":" +
		engine.PathRoot.Field("params").Field(name).String())
}

// Policies returns every grid candidate in grid order. A range param
// without step is reported as engine.CodeInvalidValue at
// "$.policies.policy:$.params.<name>.step"; more than MaxPolicies
// candidates as sim.CodeTooManyPolicies. Both are *engine.ValidationErrors.
func (s GridSource) Policies(_ *model.CompiledModel) ([]sim.Policy, error) {
	if s.Policy == nil {
		return nil, errors.New("experiment: grid search has no policy")
	}
	specs := s.Policy.ParamSpecs()
	names := sortedKeys(specs)
	errs := &engine.ValidationErrors{}
	total := 1.0
	for _, name := range names {
		spec := specs[name]
		if spec.IsRange() && spec.Step == nil {
			errs.Add(policyParamPath(name).Field("step").String(), engine.CodeInvalidValue, "grid search requires step")
			continue
		}
		total *= axisLen(spec)
	}
	if errs.Len() > 0 {
		return nil, sortIssues(errs)
	}
	limit := s.MaxPolicies
	if limit <= 0 {
		limit = DefaultMaxPolicies
	}
	if total > float64(limit) {
		errs.Add(engine.PathRoot.Field("policies").Field("search").String(), sim.CodeTooManyPolicies,
			fmt.Sprintf("grid search has %s candidates, more than max_policies (%d)", strconv.FormatFloat(total, 'g', -1, 64), limit))
		return nil, errs
	}

	axes := make([][]any, len(names))
	for i, name := range names {
		axes[i] = axisValues(specs[name])
	}
	c := newCandidates(s.Policy, int(total))
	idx := make([]int, len(names))
	for {
		values := make(map[string]any, len(names))
		for i, name := range names {
			values[name] = axes[i][idx[i]]
		}
		if err := c.add(values); err != nil {
			return nil, err
		}
		// Advance the odometer, last name fastest.
		i := len(idx) - 1
		for ; i >= 0; i-- {
			idx[i]++
			if idx[i] < len(axes[i]) {
				break
			}
			idx[i] = 0
		}
		if i < 0 {
			return c.policies, nil
		}
	}
}

// Policies returns the distinct random candidates in draw order.
func (s RandomSource) Policies(_ *model.CompiledModel) ([]sim.Policy, error) {
	if s.Policy == nil {
		return nil, errors.New("experiment: random search has no policy")
	}
	if s.Samples < 1 {
		return nil, fmt.Errorf("experiment: random search requires samples >= 1, got %d", s.Samples)
	}
	specs := s.Policy.ParamSpecs()
	names := sortedKeys(specs)
	rng := rand.New(rand.NewPCG(s.Seed, randomStream))
	c := newCandidates(s.Policy, s.Samples)
	for range s.Samples {
		values := make(map[string]any, len(names))
		for _, name := range names {
			spec := specs[name]
			switch {
			case !spec.IsRange():
				values[name] = spec.Values[rng.IntN(len(spec.Values))]
			case spec.Step != nil:
				n := min(axisLen(spec), maxGridPoints)
				values[name] = gridPoint(spec, rng.Int64N(int64(n)))
			default:
				values[name] = min(roundParam(*spec.Min+rng.Float64()*(*spec.Max-*spec.Min)), *spec.Max)
			}
		}
		if err := c.add(values); err != nil {
			return nil, err
		}
	}
	return c.policies, nil
}

// candidates collects WithParams copies of a template, dropping repeated IDs.
type candidates struct {
	template model.Parameterized
	policies []sim.Policy
	seen     map[string]bool
}

func newCandidates(template model.Parameterized, capacity int) *candidates {
	return &candidates{template: template, policies: make([]sim.Policy, 0, capacity), seen: make(map[string]bool, capacity)}
}

func (c *candidates) add(values map[string]any) error {
	p, err := c.template.WithParams(values)
	if err != nil {
		return err
	}
	if !c.seen[p.ID()] {
		c.seen[p.ID()] = true
		c.policies = append(c.policies, p)
	}
	return nil
}

// axisLen returns the number of grid values of a param: its values, or the
// grid points of a range with step.
func axisLen(spec model.ParamSpec) float64 {
	if !spec.IsRange() {
		return float64(len(spec.Values))
	}
	return math.Floor((*spec.Max-*spec.Min) / *spec.Step + gridEpsilon) + 1
}

// axisValues returns the grid values of a param in grid order.
func axisValues(spec model.ParamSpec) []any {
	if !spec.IsRange() {
		return append([]any(nil), spec.Values...)
	}
	n := int64(axisLen(spec))
	out := make([]any, n)
	for i := range n {
		out[i] = gridPoint(spec, i)
	}
	return out
}

// gridPoint returns min + i*step of a range param, rounded and capped at
// max.
func gridPoint(spec model.ParamSpec, i int64) float64 {
	return min(roundParam(*spec.Min+float64(i)**spec.Step), *spec.Max)
}

// roundParam rounds v to 12 significant digits to drop float noise such as
// 0.30000000000000004.
func roundParam(v float64) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 12, 64), 64)
	if err != nil {
		return v
	}
	return r
}
