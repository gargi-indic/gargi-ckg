package sim

import (
	"errors"
)

// ErrNotDiscrete is returned when a discrete sample is requested from a continuous distribution.
var ErrNotDiscrete = errors.New("not a discrete distribution")

// Sampler provides methods to generate random samples for the simulation.
type Sampler interface {
	// Choose selects an index from a set of discrete probabilities.
	// The weights must be non-negative and sum to more than 0.
	Choose(stream string, weights []float64) (int, error)

	// Float generates a continuous value sampled from the given Distribution.
	Float(stream string, d Distribution) (float64, error)
}
