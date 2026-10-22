package sampler

import (
	"math"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

func TestMonteCarlo_Determinism(t *testing.T) {
	s1 := NewMonteCarlo(42)
	s2 := NewMonteCarlo(42)

	d := sim.Distribution{Kind: sim.DistKindNormal, Params: map[string]float64{"mean": 0, "sd": 1}}

	v1_1, _ := s1.Float("s1", d)
	v1_2, _ := s1.Float("s2", d)

	v2_1, _ := s2.Float("s1", d)
	v2_2, _ := s2.Float("s2", d)

	if v1_1 != v2_1 {
		t.Errorf("expected %f, got %f", v1_1, v2_1)
	}
	if v1_2 != v2_2 {
		t.Errorf("expected %f, got %f", v1_2, v2_2)
	}

	// adding a draw on a new stream leaves existing streams unchanged
	s3 := NewMonteCarlo(42)
	_, _ = s3.Float("new_stream", d)
	v3_1, _ := s3.Float("s1", d)
	if v1_1 != v3_1 {
		t.Errorf("stream s1 changed after adding new stream: expected %f, got %f", v1_1, v3_1)
	}
}

func TestMonteCarlo_Moments(t *testing.T) {
	s := NewMonteCarlo(1)
	n := 100000

	d := sim.Distribution{Kind: sim.DistKindNormal, Params: map[string]float64{"mean": 5, "sd": 2}}

	vals := make([]float64, n)
	for i := 0; i < n; i++ {
		vals[i], _ = s.Float("stream", d)
	}

	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	mean := sum / float64(len(vals))

	varianceSum := 0.0
	for _, v := range vals {
		varianceSum += (v - mean) * (v - mean)
	}
	sd := math.Sqrt(varianceSum / float64(len(vals)))

	if math.Abs(mean-5) > 0.05 {
		t.Errorf("expected mean ~5, got %f", mean)
	}
	if math.Abs(sd-2) > 0.05 {
		t.Errorf("expected sd ~2, got %f", sd)
	}
}

func TestEnumerator_LoanBehaviors(t *testing.T) {
	// A simple mock for "loan behaviors" branch weights summing to 1
	e := NewEnumerator()

	totalWeight := 0.0
	iters := 0
	for e.Next() {
		iters++
		s := e.Sampler()

		// stream 1
		_, _ = s.Choose("s1", []float64{0.1, 0.9})

		// stream 2
		_, _ = s.Choose("s2", []float64{0.5, 0.5})

		totalWeight += e.Weight()
	}

	if iters != 4 {
		t.Errorf("expected 4 iterations, got %d", iters)
	}

	if math.Abs(totalWeight-1.0) > 1e-12 {
		t.Errorf("expected sum of weights 1.0, got %f", totalWeight)
	}
}

func TestEnumerator_EdgeCases(t *testing.T) {
	e := NewEnumerator()

	d := sim.Distribution{Kind: sim.DistKindNormal, Params: map[string]float64{"mean": 0, "sd": 1}}
	if _, err := e.Float("s1", d); err != sim.ErrNotDiscrete {
		t.Errorf("expected ErrNotDiscrete, got %v", err)
	}

	e.Next()
	s := e.Sampler()
	if _, err := s.Choose("stream", []float64{0, 0}); err == nil {
		t.Errorf("expected error for all zero weights")
	}
}

func TestEnumerator_NoChoices(t *testing.T) {
	e := NewEnumerator()

	iters := 0
	for e.Next() {
		iters++
	}

	if iters != 1 {
		t.Errorf("expected 1 iteration for an episode making no choices, got %d", iters)
	}
}
