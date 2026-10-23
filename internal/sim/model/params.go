package model

import (
	"fmt"
	"maps"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// paramsVar is the CEL variable rule when expressions read parameters from,
// as params.<name>.
const paramsVar = "params"

// paramName is the syntax of a policy parameter name.
var paramName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Parameterized is a compiled policy whose params can be rebound without
// recompiling it. Every policy returned by CompilePolicy implements it; a
// policy without params has empty ParamSpecs and Params and behaves exactly
// like a plain sim.Policy.
//
// Optimizers (grid and random search, and later ones) compile a policy/v1
// document once and derive one candidate per parameter assignment with
// WithParams. Candidates share the compiled CEL programs, are immutable and
// are safe for concurrent use.
type Parameterized interface {
	sim.Policy
	// ParamSpecs returns the declared params by name. The map is a copy;
	// the specs it holds are shared and must not be modified.
	ParamSpecs() map[string]ParamSpec
	// Params returns the bound value of every declared param by name: the
	// defaults until rebound by WithParams. Range params are float64;
	// values-list params are float64 or string. The map is a copy.
	Params() map[string]any
	// WithParams returns a copy of the policy with values bound on top of
	// the current values; params missing from values keep their current
	// value. An unknown name is reported as sim.CodePolicyUnknownParam and
	// an inadmissible value (outside [min, max] or not one of values) as
	// sim.CodeParamOutOfRange, both at $.params.<name>, in a
	// *engine.ValidationErrors listing every problem. The CEL programs are
	// shared with the receiver, not recompiled.
	//
	// The copy's ID is <id>@<version>[a=1,b=x] (params sorted by name,
	// numbers formatted with strconv.FormatFloat(v, 'g', -1, 64)) and its
	// Describe appends " params a=1,b=x".
	WithParams(values map[string]any) (Parameterized, error)
}

var _ Parameterized = (*runtimePolicy)(nil)

// IsRange reports whether the spec declares a range (any of Min, Max and
// Step set) rather than a values list.
func (s ParamSpec) IsRange() bool {
	return s.Min != nil || s.Max != nil || s.Step != nil
}

// DefaultValue returns the value bound to the param until it is rebound:
// Default when set, otherwise Min (range) or Values[0] (values list). Numbers
// are returned as float64. It assumes a valid spec.
func (s ParamSpec) DefaultValue() any {
	if s.Default != nil {
		v, _ := paramValue(s.Default)
		return v
	}
	if s.IsRange() {
		if s.Min == nil {
			return nil
		}
		return *s.Min
	}
	if len(s.Values) == 0 {
		return nil
	}
	v, _ := paramValue(s.Values[0])
	return v
}

// Admit checks that v is an admissible value of the param and returns it
// in its bound form (numbers as float64). For a range param v must be a
// number in [Min, Max]; for a values-list param it must equal one of Values
// (numbers compared as float64). Step is not enforced, so continuous
// optimizers may bind any value of the range.
func (s ParamSpec) Admit(v any) (any, error) {
	val, ok := paramValue(v)
	if s.IsRange() {
		f, isNum := val.(float64)
		if !ok || !isNum || s.Min == nil || s.Max == nil || f < *s.Min || f > *s.Max {
			return nil, fmt.Errorf("value %s is not a number in [%s, %s]", formatAny(v), formatOptional(s.Min), formatOptional(s.Max))
		}
		return f, nil
	}
	if ok {
		for _, allowed := range s.Values {
			if a, _ := paramValue(allowed); a == val {
				return val, nil
			}
		}
	}
	return nil, fmt.Errorf("value %s is not one of the param's values", formatAny(v))
}

// paramValue normalizes a param value: numbers become float64 (finite
// only), strings are kept. Other values are rejected.
func paramValue(v any) (any, bool) {
	switch n := normalizeNumber(v).(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, false
		}
		return n, true
	case string:
		return n, true
	}
	return nil, false
}

// FormatParamValue formats a bound param value as it appears in policy IDs
// and descriptions: numbers with strconv.FormatFloat(v, 'g', -1, 64),
// strings verbatim.
func FormatParamValue(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'g', -1, 64)
	case string:
		return n
	}
	return fmt.Sprint(v)
}

func formatAny(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return FormatParamValue(normalizeNumber(v))
}

func formatOptional(f *float64) string {
	if f == nil {
		return "?"
	}
	return FormatParamValue(*f)
}

// validateParams reports every problem of a policy's params declaration as
// engine.CodeInvalidValue at $.params.<name>[.field].
func validateParams(params map[string]ParamSpec, errs *engine.ValidationErrors) {
	base := engine.PathRoot.Field(paramsVar)
	for _, name := range sortedKeys(params) {
		spec := params[name]
		at := base.Field(name)
		if !paramName.MatchString(name) {
			errs.Add(at.String(), engine.CodeInvalidValue, fmt.Sprintf("param name %q must match %s", name, paramName))
		}
		switch isRange, isValues := spec.IsRange(), spec.Values != nil; {
		case isRange && isValues:
			errs.Add(at.String(), engine.CodeInvalidValue, "param must be either a range (min, max, step) or a values list, not both")
		case isRange:
			validateRange(spec, at, errs)
		case isValues:
			validateValues(spec, at, errs)
		default:
			errs.Add(at.String(), engine.CodeInvalidValue, "param must be a range (min, max, step) or a values list")
		}
	}
}

func validateRange(spec ParamSpec, at engine.Path, errs *engine.ValidationErrors) {
	finite := func(field string, f *float64) bool {
		if f == nil {
			return false
		}
		if math.IsNaN(*f) || math.IsInf(*f, 0) {
			errs.Add(at.Field(field).String(), engine.CodeInvalidValue, field+" must be finite")
			return false
		}
		return true
	}
	if spec.Min == nil {
		errs.Add(at.Field("min").String(), engine.CodeInvalidValue, "range param requires min")
	}
	if spec.Max == nil {
		errs.Add(at.Field("max").String(), engine.CodeInvalidValue, "range param requires max")
	}
	minOK, maxOK := finite("min", spec.Min), finite("max", spec.Max)
	if minOK && maxOK && *spec.Min > *spec.Max {
		errs.Add(at.Field("max").String(), engine.CodeInvalidValue,
			fmt.Sprintf("max %s must be >= min %s", FormatParamValue(*spec.Max), FormatParamValue(*spec.Min)))
	}
	if finite("step", spec.Step) && *spec.Step <= 0 {
		errs.Add(at.Field("step").String(), engine.CodeInvalidValue, "step must be > 0")
	}
	if spec.Default != nil && minOK && maxOK && *spec.Min <= *spec.Max {
		if _, err := spec.Admit(spec.Default); err != nil {
			errs.Add(at.Field("default").String(), engine.CodeInvalidValue, "default "+err.Error())
		}
	}
}

func validateValues(spec ParamSpec, at engine.Path, errs *engine.ValidationErrors) {
	if len(spec.Values) == 0 {
		errs.Add(at.Field("values").String(), engine.CodeInvalidValue, "values must not be empty")
		return
	}
	seen := make(map[any]int, len(spec.Values))
	for i, v := range spec.Values {
		val, ok := paramValue(v)
		if !ok {
			errs.Add(at.Field("values").Index(i).String(), engine.CodeInvalidValue, "value must be a finite number or a string")
			continue
		}
		if j, dup := seen[val]; dup {
			errs.Add(at.Field("values").Index(i).String(), engine.CodeInvalidValue,
				fmt.Sprintf("duplicate value %s (also values[%d])", formatAny(v), j))
			continue
		}
		seen[val] = i
	}
	if spec.Default != nil {
		if _, err := spec.Admit(spec.Default); err != nil {
			errs.Add(at.Field("default").String(), engine.CodeInvalidValue, "default "+err.Error())
		}
	}
}

// ParamSpecs returns a copy of the declared params.
func (p *runtimePolicy) ParamSpecs() map[string]ParamSpec {
	return maps.Clone(p.specs)
}

// Params returns a copy of the bound param values.
func (p *runtimePolicy) Params() map[string]any {
	return maps.Clone(p.values)
}

// WithParams returns a copy of p with values bound; see Parameterized.
func (p *runtimePolicy) WithParams(values map[string]any) (Parameterized, error) {
	errs := &engine.ValidationErrors{}
	bound := maps.Clone(p.values)
	if bound == nil {
		bound = map[string]any{}
	}
	base := engine.PathRoot.Field(paramsVar)
	for _, name := range sortedKeys(values) {
		at := base.Field(name).String()
		spec, ok := p.specs[name]
		if !ok {
			errs.Add(at, sim.CodePolicyUnknownParam, fmt.Sprintf("unknown param %q", name))
			continue
		}
		v, err := spec.Admit(values[name])
		if err != nil {
			errs.Add(at, sim.CodeParamOutOfRange, err.Error())
			continue
		}
		bound[name] = v
	}
	if errs.Len() > 0 {
		return nil, sortedIssues(errs)
	}
	return p.bind(bound), nil
}

// bind returns a copy of p with values as its bound params. The compiled
// decisions are shared.
func (p *runtimePolicy) bind(values map[string]any) *runtimePolicy {
	cp := *p
	cp.values = values
	cp.id, cp.description = p.baseID, p.baseDescription
	if len(p.specs) > 0 {
		names := sortedKeys(p.specs)
		pairs := make([]string, len(names))
		for i, n := range names {
			pairs[i] = n + "=" + FormatParamValue(values[n])
		}
		joined := strings.Join(pairs, ",")
		cp.id += "[" + joined + "]"
		cp.description += " params " + joined
	}
	return &cp
}
