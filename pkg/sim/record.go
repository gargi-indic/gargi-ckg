package sim

import (
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

// RunRecord stores the outcome of a single simulation run.
type RunRecord struct {
	// World is the identifier of the simulated world.
	World WorldID `json:"world"`
	// PolicyID is the identifier of the policy used for decisions.
	PolicyID string `json:"policyId"`
	// Replication is the index of this simulation run within an experiment.
	Replication int `json:"replication"`
	// Weight is 1 for Monte Carlo, or the branch probability for exact enumeration.
	Weight float64 `json:"weight"`
	// Objective is the overall evaluated objective value of this run.
	Objective float64 `json:"objective"`
	// Cost is the total accumulated cost across all nodes.
	Cost float64 `json:"cost"`
	// Duration is the total simulated time for this run.
	Duration time.Duration `json:"duration"`
	// Abandoned indicates if the run was abandoned early.
	Abandoned bool `json:"abandoned"`
	// Path is the sequence of nodes executed during the run.
	Path []engine.NodeID `json:"path"`
	// Decisions maps decision nodes to the option chosen by the policy.
	Decisions map[engine.NodeID]string `json:"decisions"`
	// Constraints maps constraint names to whether they were satisfied.
	Constraints map[string]bool `json:"constraints"`
	// FinalStatus is the overall execution status at the end of the run.
	FinalStatus engine.Status `json:"finalStatus"`
	// Metrics maps every metric declared by the model (sim/v1 metrics) to
	// its value in this run. It is nil when the model declares no metrics.
	// The built-in metrics (objective, cost, duration) are the fields above.
	Metrics map[string]float64 `json:"metrics,omitempty"`
	// Events maps the name of every prob(...) aggregate requirement of the
	// model (sim/v1 objective.requirements) to whether its per-run condition
	// held in this run. It is derived data for the requirement's probability
	// and is not serialized; it is nil when the model has no such
	// requirement.
	Events map[string]bool `json:"-"`
}
