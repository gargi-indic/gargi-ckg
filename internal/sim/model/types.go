package model

// Sim represents a simulation definition.
type Sim struct {
	APIVersion string                    `json:"apiVersion"`
	ID         string                    `json:"id"`
	Graph      string                    `json:"graph"`
	Worlds     map[string]map[string]any `json:"worlds"`
	Latents    map[string]ValueSpec      `json:"latents,omitempty"`
	Behaviors  map[string]Behavior       `json:"behaviors,omitempty"`
	Resources  map[string]Resource       `json:"resources,omitempty"`
	Objective  Objective                 `json:"objective"`
}

// Resource describes the constraints and capacity of a system resource.
type Resource struct {
	Capacity int `json:"capacity"`
}

// Objective defines the goal to maximize or minimize and its constraints.
type Objective struct {
	Name        string            `json:"name"`
	Value       string            `json:"value"`
	Maximize    string            `json:"maximize"` // "mean"
	Constraints map[string]string `json:"constraints,omitempty"`
}

// Behavior specifies the cost, duration, failure rates, and outputs of a node.
type Behavior struct {
	Cost     any                  `json:"cost,omitempty"`     // number or string (CEL)
	Duration any                  `json:"duration,omitempty"` // string ("48h") or ValueSpec
	Abandon  *Abandon             `json:"abandon,omitempty"`
	Fail     *Fail                `json:"fail,omitempty"`
	Output   map[string]ValueSpec `json:"output,omitempty"`
}

// Abandon configures the probability of a task being abandoned.
type Abandon struct {
	P any `json:"p"` // number or string
}

// Fail configures the probability and classification of a failure.
type Fail struct {
	P     any    `json:"p"`     // number or string
	Class string `json:"class"` // "retryable", "non_retryable", "critical"
}

// ValueSpec exactly one of the fields should be set.
type ValueSpec struct {
	Fixed      any            `json:"fixed,omitempty"` // number or string
	Choice     map[string]any `json:"choice,omitempty"`
	Bernoulli  *Bernoulli     `json:"bernoulli,omitempty"`
	Normal     *Normal        `json:"normal,omitempty"`
	Lognormal  *Lognormal     `json:"lognormal,omitempty"`
	Uniform    *Uniform       `json:"uniform,omitempty"`
	Triangular *Triangular    `json:"triangular,omitempty"`
	Empirical  []float64      `json:"empirical,omitempty"`
	Expr       string         `json:"expr,omitempty"`
}

// Bernoulli represents a Bernoulli distribution.
type Bernoulli struct {
	P any `json:"p"`
}

// Normal represents a Normal (Gaussian) distribution.
type Normal struct {
	Mean any `json:"mean"`
	SD   any `json:"sd"`
}

// Lognormal represents a Lognormal distribution.
type Lognormal struct {
	Mu    any `json:"mu"`
	Sigma any `json:"sigma"`
}

// Uniform represents a continuous Uniform distribution.
type Uniform struct {
	Min any `json:"min"`
	Max any `json:"max"`
}

// Triangular represents a Triangular distribution.
type Triangular struct {
	Min  any `json:"min"`
	Mode any `json:"mode"`
	Max  any `json:"max"`
}

// Policy represents a policy definition.
type Policy struct {
	APIVersion string              `json:"apiVersion"`
	ID         string              `json:"id"`
	Version    string              `json:"version"`
	Decisions  map[string]Decision `json:"decisions"`
}

// Decision contains the rules or table used to make a policy choice.
type Decision struct {
	Rules   []Rule     `json:"rules,omitempty"`
	Table   []TableRow `json:"table,omitempty"`
	Default string     `json:"default,omitempty"`
}

// Rule represents a conditional choice in a policy decision.
type Rule struct {
	When   string `json:"when,omitempty"`
	Choose string `json:"choose,omitempty"`
	Else   string `json:"else,omitempty"`
}

// TableRow represents a single observation and its corresponding choice.
type TableRow struct {
	Observed map[string]any `json:"observed"`
	Choose   string         `json:"choose"`
}

// Experiment represents an experiment definition.
type Experiment struct {
	APIVersion string   `json:"apiVersion"`
	Model      string   `json:"model"`
	Worlds     []string `json:"worlds,omitempty"` // default all
	Policies   Policies `json:"policies"`
	Evaluator  string   `json:"evaluator,omitempty"` // "monte_carlo" | "exact" | "both", default "monte_carlo"
	Runs       int      `json:"runs"`
	Seed       uint64   `json:"seed"`
	Workers    int      `json:"workers,omitempty"`
	Top        int      `json:"top,omitempty"` // default 5
	// MaxPolicies caps the number of policies enumerated by search:
	// exhaustive; 0 selects the default (10,000).
	MaxPolicies int `json:"max_policies,omitempty"`
}

// Policies defines how policies are discovered or supplied to an experiment.
type Policies struct {
	Search string   `json:"search,omitempty"`
	Files  []string `json:"files,omitempty"`
}
