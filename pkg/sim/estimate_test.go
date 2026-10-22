package sim_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

func TestEstimate_JSON(t *testing.T) {
	estimate := sim.Estimate{
		N:              100,
		TotalWeight:    100.0,
		Mean:           50.5,
		StdDev:         5.0,
		CI95Low:        45.5,
		CI95High:       55.5,
		P50:            50.0,
		P95:            58.0,
		Min:            40.0,
		Max:            60.0,
		ConstraintProb: map[string]float64{"max_cost": 0.95},
		Exact:          false,
	}

	expectedJSON := `{"n":100,"totalWeight":100,"mean":50.5,"stdDev":5,"ci95Low":45.5,"ci95High":55.5,"p50":50,"p95":58,"min":40,"max":60,"constraintProb":{"max_cost":0.95},"exact":false}`

	data, err := json.Marshal(estimate)
	if err != nil {
		t.Fatalf("Failed to marshal Estimate: %v", err)
	}

	if string(data) != expectedJSON {
		t.Errorf("JSON marshal mismatch.\nExpected: %s\nGot:      %s", expectedJSON, string(data))
	}

	var unmarshaled sim.Estimate
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal Estimate: %v", err)
	}

	if !reflect.DeepEqual(estimate, unmarshaled) {
		t.Errorf("Estimate round-trip mismatch.\nExpected: %+v\nGot:      %+v", estimate, unmarshaled)
	}
}
