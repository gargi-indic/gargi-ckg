package sim_test

import (
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

func TestSeed(t *testing.T) {
	// Golden value tests
	tests := []struct {
		name           string
		experimentSeed uint64
		world          sim.WorldID
		replication    int
		want           uint64
	}{
		{"zeros", 0, "", 0, 9354609568656401157},
		{"test1", 12345, "world-A", 1, 1686254468500217118},
		{"test2", 67890, "world-B", 2, 1963971211689695067},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sim.Seed(tt.experimentSeed, tt.world, tt.replication)
			if got != tt.want {
				t.Errorf("Seed() = %v, want %v", got, tt.want)
			}

			// Prove identical inputs give same value
			got2 := sim.Seed(tt.experimentSeed, tt.world, tt.replication)
			if got2 != got {
				t.Errorf("Seed() is not deterministic: got %v, then %v", got, got2)
			}
		})
	}

	// Prove changing only world changes value
	baseSeed := sim.Seed(12345, "world-A", 1)
	worldSeed := sim.Seed(12345, "world-B", 1)
	if baseSeed == worldSeed {
		t.Errorf("Seed() did not change when only world changed")
	}

	// Prove changing only replication changes value
	repSeed := sim.Seed(12345, "world-A", 2)
	if baseSeed == repSeed {
		t.Errorf("Seed() did not change when only replication changed")
	}
}
