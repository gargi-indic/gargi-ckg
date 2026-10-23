package model

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/google/cel-go/cel"
)

// Built-in metric names. Every run has these metrics; models cannot declare
// metrics with these names.
const (
	// MetricObjective is the run's objective value.
	MetricObjective = "objective"
	// MetricCost is the run's total cost.
	MetricCost = "cost"
	// MetricDuration is the run's simulated duration in seconds.
	MetricDuration = "duration"
)

// metricName is the syntax of a declared metric name.
var metricName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// IsBuiltinMetric reports whether name is a built-in metric (objective, cost
// or duration).
func IsBuiltinMetric(name string) bool {
	return name == MetricObjective || name == MetricCost || name == MetricDuration
}

// HasMetric reports whether name is a built-in metric or a metric declared by
// the model.
func (m *CompiledModel) HasMetric(name string) bool {
	if IsBuiltinMetric(name) {
		return true
	}
	_, ok := m.CompiledMetrics[name]
	return ok
}

// Aggregate is the aggregation of an aggregate requirement.
type Aggregate string

// Aggregations of a requirement.
const (
	// AggMean is the mean of a metric.
	AggMean Aggregate = "mean"
	// AggP50 is the median (nearest-rank P50) of a metric.
	AggP50 Aggregate = "p50"
	// AggP95 is the nearest-rank P95 of a metric.
	AggP95 Aggregate = "p95"
	// AggProb is the probability that a per-run boolean condition holds.
	AggProb Aggregate = "prob"
)

// Op is the comparison operator of an aggregate requirement.
type Op string

// Comparison operators of a requirement.
const (
	OpLE Op = "<="
	OpLT Op = "<"
	OpGE Op = ">="
	OpGT Op = ">"
	OpEQ Op = "=="
)

// Holds reports whether value op bound is true.
func (op Op) Holds(value, bound float64) bool {
	switch op {
	case OpLE:
		return value <= bound
	case OpLT:
		return value < bound
	case OpGE:
		return value >= bound
	case OpGT:
		return value > bound
	case OpEQ:
		return value == bound
	}
	return false
}

// Requirement is a parsed aggregate requirement <agg>(<arg>) <op> <rhs>.
type Requirement struct {
	// Agg is the aggregation.
	Agg Aggregate
	// Arg is the metric name (mean, p50, p95) or the boolean CEL expression
	// (prob), trimmed.
	Arg string
	// Op is the comparison operator.
	Op Op
	// Bound is the right-hand side; a duration('...') bound is in seconds.
	Bound float64
}

// ParseRequirement parses the grammar of an aggregate requirement:
//
//	<agg>(<arg>) <op> <rhs>
//
// where agg is mean, p50, p95 or prob; arg is a metric name for mean, p50
// and p95 (whether the metric exists is checked by Compile) and a CEL
// expression for prob (its parentheses must balance; quoted strings may
// contain any character); op is <=, <, >=, > or ==; and rhs is a finite
// number or duration('<Go duration>') (single or double quotes), converted
// to seconds. Whitespace around the tokens is ignored.
func ParseRequirement(s string) (Requirement, error) {
	var r Requirement
	rest := strings.TrimSpace(s)
	if rest == "" {
		return r, errors.New("requirement must not be empty")
	}

	end := 0
	for end < len(rest) && (rest[end] == '_' || rest[end] >= 'a' && rest[end] <= 'z' || rest[end] >= '0' && rest[end] <= '9') {
		end++
	}
	agg := Aggregate(rest[:end])
	switch agg {
	case AggMean, AggP50, AggP95, AggProb:
	case "":
		return r, errors.New("requirement must start with mean, p50, p95 or prob")
	default:
		return r, fmt.Errorf("unknown aggregate %q: want mean, p50, p95 or prob", agg)
	}
	r.Agg = agg
	rest = strings.TrimLeft(rest[end:], " \t")
	if !strings.HasPrefix(rest, "(") {
		return r, fmt.Errorf("expected ( after %s", agg)
	}
	closing, err := matchParen(rest)
	if err != nil {
		return r, err
	}
	r.Arg = strings.TrimSpace(rest[1:closing])
	if r.Arg == "" {
		return r, fmt.Errorf("%s() needs an argument", agg)
	}
	if agg != AggProb && !metricName.MatchString(r.Arg) {
		return r, fmt.Errorf("%s(%s): argument must be a metric name", agg, r.Arg)
	}

	rest = strings.TrimSpace(rest[closing+1:])
	if rest == "" {
		return r, errors.New("missing comparison operator: want <=, <, >=, > or ==")
	}
	switch {
	case strings.HasPrefix(rest, "<="), strings.HasPrefix(rest, ">="), strings.HasPrefix(rest, "=="):
		r.Op = Op(rest[:2])
	case strings.HasPrefix(rest, "<"), strings.HasPrefix(rest, ">"):
		r.Op = Op(rest[:1])
	default:
		return r, fmt.Errorf("unknown comparison operator at %q: want <=, <, >=, > or ==", rest)
	}
	rest = strings.TrimSpace(rest[len(r.Op):])
	if rest == "" {
		return r, errors.New("missing right-hand side: want a number or duration('...')")
	}
	if r.Bound, err = parseBound(rest); err != nil {
		return r, err
	}
	return r, nil
}

// matchParen returns the index of the parenthesis closing s[0] == '(',
// skipping quoted strings.
func matchParen(s string) (int, error) {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"':
			j := i + 1
			for ; j < len(s) && s[j] != c; j++ {
				if s[j] == '\\' {
					j++
				}
			}
			if j >= len(s) {
				return 0, errors.New("unterminated string literal")
			}
			i = j
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, errors.New("unbalanced parentheses: missing )")
}

// parseBound parses the right-hand side of a requirement: a finite number or
// duration('<Go duration>') in seconds.
func parseBound(s string) (float64, error) {
	if inner, ok := strings.CutPrefix(s, "duration("); ok {
		lit, ok := strings.CutSuffix(strings.TrimSpace(inner), ")")
		lit = strings.TrimSpace(lit)
		if !ok || len(lit) < 2 || (lit[0] != '\'' && lit[0] != '"') || lit[len(lit)-1] != lit[0] {
			return 0, fmt.Errorf("invalid bound %q: want duration('<Go duration>')", s)
		}
		d, err := time.ParseDuration(lit[1 : len(lit)-1])
		if err != nil {
			return 0, fmt.Errorf("invalid bound %q: %w", s, err)
		}
		return d.Seconds(), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("invalid bound %q: want a finite number or duration('...')", s)
	}
	return f, nil
}

// CompiledRequirement is an aggregate requirement of the model, parsed and,
// for prob, with its per-run condition compiled.
type CompiledRequirement struct {
	// Name is the requirement's key in objective.requirements.
	Name string
	// Source is the requirement as written.
	Source string
	Requirement
	// Event is the compiled boolean condition of a prob requirement, nil
	// otherwise. The kernel evaluates it once per run into
	// sim.RunRecord.Events[Name].
	Event cel.Program
}

// compileMetrics compiles the declared metrics and the aggregate
// requirements of def into m.
func compileMetrics(def *Sim, m *CompiledModel, env *cel.Env, errs *engine.ValidationErrors) {
	metricsPath := engine.PathRoot.Field("metrics")
	m.CompiledMetrics = make(map[string]cel.Program, len(def.Metrics))
	for _, name := range sortedKeys(def.Metrics) {
		path := metricsPath.Field(name).String()
		switch {
		case IsBuiltinMetric(name):
			errs.Add(path, engine.CodeInvalidValue, fmt.Sprintf("metric name %q is reserved for a built-in metric", name))
		case !metricName.MatchString(name):
			errs.Add(path, engine.CodeInvalidValue, fmt.Sprintf("metric name %q must match %s", name, metricName))
		default:
			if prog := compileCEL(def.Metrics[name], path, env, numericTypes, errs); prog != nil {
				m.CompiledMetrics[name] = prog
				m.MetricOrder = append(m.MetricOrder, name)
			}
		}
	}

	reqPath := engine.PathRoot.Field("objective").Field("requirements")
	for _, name := range sortedKeys(def.Objective.Requirements) {
		src := def.Objective.Requirements[name]
		path := reqPath.Field(name).String()
		r, err := ParseRequirement(src)
		if err != nil {
			errs.Add(path, sim.CodeInvalidExpression, err.Error())
			continue
		}
		cr := CompiledRequirement{Name: name, Source: src, Requirement: r}
		if r.Agg == AggProb {
			if cr.Event = compileCEL(r.Arg, path, env, boolTypes, errs); cr.Event == nil {
				continue
			}
		} else if !IsBuiltinMetric(r.Arg) {
			if _, declared := def.Metrics[r.Arg]; !declared {
				errs.Add(path, engine.CodeUnresolvedReference, fmt.Sprintf("unknown metric %q: want objective, cost, duration or a declared metric", r.Arg))
				continue
			}
		}
		m.Requirements = append(m.Requirements, cr)
	}
}
