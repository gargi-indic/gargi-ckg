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
}
