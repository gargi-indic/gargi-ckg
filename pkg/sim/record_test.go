package sim_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

func TestRunRecord_JSON(t *testing.T) {
	record := sim.RunRecord{
		World:       "world-1",
		PolicyID:    "policy-a",
		Replication: 42,
		Weight:      0.75,
		Objective:   100.5,
		Cost:        10.25,
		Duration:    15 * time.Second,
		Abandoned:   false,
		Path:        []engine.NodeID{"node1", "node2"},
		Decisions:   map[engine.NodeID]string{"node1": "optA"},
		Constraints: map[string]bool{"max_cost": true},
		FinalStatus: engine.StatusSucceeded,
	}

	expectedJSON := `{"world":"world-1","policyId":"policy-a","replication":42,"weight":0.75,"objective":100.5,"cost":10.25,"duration":15000000000,"abandoned":false,"path":["node1","node2"],"decisions":{"node1":"optA"},"constraints":{"max_cost":true},"finalStatus":"Succeeded"}`

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Failed to marshal RunRecord: %v", err)
	}

	if string(data) != expectedJSON {
		t.Errorf("JSON marshal mismatch.\nExpected: %s\nGot:      %s", expectedJSON, string(data))
	}

	var unmarshaled sim.RunRecord
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal RunRecord: %v", err)
	}

	if !reflect.DeepEqual(record, unmarshaled) {
		t.Errorf("RunRecord round-trip mismatch.\nExpected: %+v\nGot:      %+v", record, unmarshaled)
	}
}
