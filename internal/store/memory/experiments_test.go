package memory_test

import (
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/store/memory"
	"github.com/gargi-indic/gargi-flow/internal/store/storetest"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

func TestExperimentStoreConformance(t *testing.T) {
	storetest.RunExperimentStore(t, func(_ *testing.T) sim.ExperimentStore {
		return memory.NewExperimentStore()
	})
}
