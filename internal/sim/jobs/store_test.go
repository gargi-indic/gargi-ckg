package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// fakeStore is a minimal in-test sim.ExperimentStore (internal/sim may not
// import internal/store, ADR-0009). It keeps a snapshot of every stored
// experiment update so tests can inspect the write history.
type fakeStore struct {
	mu          sync.Mutex
	models      map[string][]sim.ModelBundle
	experiments map[string]sim.ExperimentRecord
	history     map[string][]sim.ExperimentRecord
}

var _ sim.ExperimentStore = (*fakeStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{
		models:      make(map[string][]sim.ModelBundle),
		experiments: make(map[string]sim.ExperimentRecord),
		history:     make(map[string][]sim.ExperimentRecord),
	}
}

func cloneRecord(r sim.ExperimentRecord) sim.ExperimentRecord {
	r.Definition = slices.Clone(r.Definition)
	r.Report = slices.Clone(r.Report)
	return r
}

func (s *fakeStore) SaveModel(_ context.Context, b sim.ModelBundle) (sim.ModelBundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b.Version = strconv.Itoa(len(s.models[b.ID]) + 1)
	b.CreatedAt = time.Now().UTC()
	b.Policies = maps.Clone(b.Policies)
	s.models[b.ID] = append(s.models[b.ID], b)
	return b, nil
}

func (s *fakeStore) GetModel(_ context.Context, id, version string) (sim.ModelBundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := s.models[id]
	if len(versions) == 0 {
		return sim.ModelBundle{}, &engine.StoreNotFoundError{Entity: "model " + id}
	}
	if version == "" {
		return versions[len(versions)-1], nil
	}
	n, err := strconv.Atoi(version)
	if err != nil || n < 1 || n > len(versions) {
		return sim.ModelBundle{}, &engine.StoreNotFoundError{Entity: "model " + id + " version " + version}
	}
	return versions[n-1], nil
}

func (s *fakeStore) ListModels(context.Context) ([]sim.ModelBundle, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeStore) CreateExperiment(_ context.Context, rec sim.ExperimentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.experiments[rec.ID]; ok {
		return fmt.Errorf("%w: %s", engine.ErrStoreConflict, rec.ID)
	}
	s.experiments[rec.ID] = cloneRecord(rec)
	s.history[rec.ID] = append(s.history[rec.ID], cloneRecord(rec))
	return nil
}

func (s *fakeStore) GetExperiment(_ context.Context, id string) (sim.ExperimentRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.experiments[id]
	if !ok {
		return sim.ExperimentRecord{}, &engine.StoreNotFoundError{Entity: "experiment " + id}
	}
	return cloneRecord(rec), nil
}

func (s *fakeStore) ListExperiments(context.Context, sim.ListExperimentsOptions) ([]sim.ExperimentRecord, string, error) {
	return nil, "", errors.New("not implemented")
}

func (s *fakeStore) UpdateExperiment(_ context.Context, id string, fn func(*sim.ExperimentRecord) error) (sim.ExperimentRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.experiments[id]
	if !ok {
		return sim.ExperimentRecord{}, &engine.StoreNotFoundError{Entity: "experiment " + id}
	}
	work := cloneRecord(rec)
	if err := fn(&work); err != nil {
		return sim.ExperimentRecord{}, err
	}
	work.ID = id
	s.experiments[id] = cloneRecord(work)
	s.history[id] = append(s.history[id], cloneRecord(work))
	return work, nil
}

// writes returns every stored state of experiment id, oldest first.
func (s *fakeStore) writes(id string) []sim.ExperimentRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.history[id])
}

// loanDir is the loan underwriting example.
var loanDir = filepath.Join("..", "..", "..", "examples", "simulation", "loan")

// loanBundle returns the loan example as a model bundle.
func loanBundle(t *testing.T) sim.ModelBundle {
	t.Helper()
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(loanDir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		return data
	}
	return sim.ModelBundle{
		ID:       "loan",
		Sim:      read("sim.yaml"),
		Graph:    read("graph.yaml"),
		Policies: map[string][]byte{"recession-v1.yaml": read(filepath.Join("policies", "recession-v1.yaml"))},
		Format:   "yaml",
	}
}

// loanExperiment returns an experiment/v1 document over the loan model.
func loanExperiment(evaluator string, runs int, policies string) []byte {
	return fmt.Appendf(nil, `apiVersion: experiment/v1
model: ./elsewhere/loan-sim.yaml
worlds: [base, recession]
policies: %s
evaluator: %s
runs: %d
seed: 42
top: 5
`, policies, evaluator, runs)
}

// Policy selections: every reduced deterministic policy, or the bundled
// recession-v1 policy only (cheap Monte Carlo chunks).
const (
	exhaustive = "{search: exhaustive}"
	onePolicy  = "{files: [./policies/recession-v1.yaml]}"
)
