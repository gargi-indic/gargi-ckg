package report

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Kind names a report of experiment/v1 reports.
type Kind string

// Report kinds.
const (
	// KindRegret is the cross-world regret matrix.
	KindRegret Kind = "regret"
	// KindRobust is the expected-value, maximin and CVaR picks.
	KindRobust Kind = "robust"
	// KindPareto is the per-world Pareto frontier of two metrics.
	KindPareto Kind = "pareto"
)

// Goal is the direction in which a Pareto axis improves.
type Goal string

// Pareto goals.
const (
	// GoalMax means larger values are better.
	GoalMax Goal = "max"
	// GoalMin means smaller values are better.
	GoalMin Goal = "min"
)

// better reports whether a is strictly better than b under g.
func (g Goal) better(a, b float64) bool {
	if g == GoalMin {
		return a < b
	}
	return a > b
}

// Axis is one metric of a Pareto report and its goal.
type Axis struct {
	// Metric is a built-in (objective, cost, duration) or declared metric.
	Metric string `json:"metric"`
	// Goal is max or min.
	Goal Goal `json:"goal"`
}

// Spec is one parsed entry of experiment/v1 reports.
type Spec struct {
	// Kind is the report kind.
	Kind Kind
	// X and Y are the axes of a Pareto report; zero for other kinds.
	X, Y Axis
}

// metricName is the syntax of a metric name.
var metricName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// ParseSpec parses one entry of experiment/v1 reports:
//
//	regret
//	robust
//	pareto:<metricA>[:min|max],<metricB>[:min|max]
//
// Pareto goals default to max; the two metrics must differ. Whether the
// metrics exist is checked by the caller against the model. New kinds (for
// example voi:<node> or flip_point:<param>) are added to the switch below.
func ParseSpec(s string) (Spec, error) {
	kind, args, hasArgs := strings.Cut(s, ":")
	switch Kind(kind) {
	case KindRegret, KindRobust:
		if hasArgs {
			return Spec{}, fmt.Errorf("report %q takes no arguments", kind)
		}
		return Spec{Kind: Kind(kind)}, nil
	case KindPareto:
		if !hasArgs {
			return Spec{}, errors.New("pareto needs two metrics: pareto:<metricA>[:min|max],<metricB>[:min|max]")
		}
		parts := strings.Split(args, ",")
		if len(parts) != 2 {
			return Spec{}, fmt.Errorf("pareto needs exactly two metrics, got %d", len(parts))
		}
		x, err := parseAxis(parts[0])
		if err != nil {
			return Spec{}, err
		}
		y, err := parseAxis(parts[1])
		if err != nil {
			return Spec{}, err
		}
		if x.Metric == y.Metric {
			return Spec{}, fmt.Errorf("pareto needs two different metrics, got %q twice", x.Metric)
		}
		return Spec{Kind: KindPareto, X: x, Y: y}, nil
	default:
		return Spec{}, fmt.Errorf("unknown report %q: want regret, robust or pareto:<metricA>[:min|max],<metricB>[:min|max]", s)
	}
}

// parseAxis parses <metric>[:min|max].
func parseAxis(s string) (Axis, error) {
	name, goal, hasGoal := strings.Cut(strings.TrimSpace(s), ":")
	name, goal = strings.TrimSpace(name), strings.TrimSpace(goal)
	if !metricName.MatchString(name) {
		return Axis{}, fmt.Errorf("pareto metric %q must match %s", name, metricName)
	}
	a := Axis{Metric: name, Goal: GoalMax}
	if hasGoal {
		switch Goal(goal) {
		case GoalMax, GoalMin:
			a.Goal = Goal(goal)
		default:
			return Axis{}, fmt.Errorf("pareto goal %q of metric %q must be min or max", goal, name)
		}
	}
	return a, nil
}

// MinWorlds returns the number of experiment worlds the report needs: 2 for
// regret and robust (they compare worlds), 1 otherwise.
func (s Spec) MinWorlds() int {
	if s.Kind == KindRegret || s.Kind == KindRobust {
		return 2
	}
	return 1
}

// Metrics returns the metrics the report reads, in axis order.
func (s Spec) Metrics() []string {
	if s.Kind == KindPareto {
		return []string{s.X.Metric, s.Y.Metric}
	}
	return nil
}
