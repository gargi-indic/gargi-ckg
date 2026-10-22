// Package sampler provides Monte Carlo and enumeration samplers.
package sampler

import (
	"errors"
	"fmt"

	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// Enumerator evaluates exact enumeration of discrete choices.
// It iterates depth-first over the tree of all choices encountered during a run.
type Enumerator struct {
	paths       []choicePath
	depth       int
	done        bool
	weight      float64
	currentPath map[string]int
}

type choicePath struct {
	stream  string
	options []float64
	choice  int
}

// NewEnumerator returns a new Enumerator.
func NewEnumerator() *Enumerator {
	return &Enumerator{
		paths:       []choicePath{},
		currentPath: make(map[string]int),
	}
}

// Next advances to the next combination of choices.
// It returns true if there is a next combination, and false when enumeration is complete.
func (e *Enumerator) Next() bool {
	if e.done {
		return false
	}

	if len(e.paths) == 0 {
		e.weight = 1.0
		// First run: just initialize and let it run to record paths.
		return true
	}

	// Advance the odometer (last choice first).
	for i := len(e.paths) - 1; i >= 0; i-- {
		p := &e.paths[i]

		// Find next choice with >0 prob
		nextChoice := p.choice + 1
		for nextChoice < len(p.options) && p.options[nextChoice] == 0 {
			nextChoice++
		}

		if nextChoice < len(p.options) {
			p.choice = nextChoice
			e.paths = e.paths[:i+1] // Truncate paths after this point as they will be re-discovered
			break
		}

		// Rollover
		p.choice = 0
		// Find first valid choice
		for p.choice < len(p.options) && p.options[p.choice] == 0 {
			p.choice++
		}
		if i == 0 {
			e.done = true
			return false
		}
	}

	e.weight = 1.0
	for _, p := range e.paths {
		e.weight *= p.options[p.choice]
	}

	// Reset runtime state for the new run
	e.depth = 0
	e.currentPath = make(map[string]int)

	return true
}

// Weight returns the product of the probabilities of all choices made in the current branch.
func (e *Enumerator) Weight() float64 {
	return e.weight
}

// Sampler returns a sim.Sampler that follows the current enumeration branch.
func (e *Enumerator) Sampler() sim.Sampler {
	return e
}

// Choose implements sim.Sampler to record a choice and select an option on the current branch.
func (e *Enumerator) Choose(stream string, weights []float64) (int, error) {
	if len(weights) == 0 {
		return 0, errors.New("empty weights")
	}

	sum := 0.0
	for _, w := range weights {
		if w < 0 {
			return 0, errors.New("negative weight")
		}
		sum += w
	}
	if sum <= 0 {
		return 0, errors.New("zero sum of weights")
	}

	normalized := make([]float64, len(weights))
	for i, w := range weights {
		normalized[i] = w / sum
	}

	if e.depth < len(e.paths) {
		// Replay choice
		if e.paths[e.depth].stream != fmt.Sprintf("%s-%d", stream, e.currentPath[stream]) {
			return 0, fmt.Errorf("divergent stream: expected %s, got %s", e.paths[e.depth].stream, stream)
		}
		choice := e.paths[e.depth].choice
		e.depth++
		e.currentPath[stream]++
		return choice, nil
	}

	// Discover new choice point
	firstChoice := 0
	for firstChoice < len(normalized) && normalized[firstChoice] == 0 {
		firstChoice++
	}

	if firstChoice >= len(normalized) {
		return 0, errors.New("all weights zero")
	}

	// First time we see this choice point on this branch, we take the first valid option
	streamName := fmt.Sprintf("%s-%d", stream, e.currentPath[stream])
	e.paths = append(e.paths, choicePath{
		stream:  streamName,
		options: normalized,
		choice:  firstChoice,
	})

	e.weight *= normalized[firstChoice]
	e.depth++
	e.currentPath[stream]++

	return firstChoice, nil
}

// Float returns ErrNotDiscrete as continuous distributions cannot be enumerated.
func (e *Enumerator) Float(_ string, _ sim.Distribution) (float64, error) {
	return 0, sim.ErrNotDiscrete
}
