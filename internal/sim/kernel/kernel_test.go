package kernel_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/internal/sim/kernel"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/internal/sim/sampler"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
)

type dummyPolicy struct {
	id     string
	choose func(ctx context.Context, decision engine.NodeID, obs sim.Observation, options []string) (string, error)
}

func (p dummyPolicy) ID() string       { return p.id }
func (p dummyPolicy) Describe() string { return p.id }
func (p dummyPolicy) Choose(ctx context.Context, decision engine.NodeID, obs sim.Observation, options []string) (string, error) {
	return p.choose(ctx, decision, obs, options)
}

type dummyChooser struct{}

func (d dummyChooser) Choose(_ context.Context, _ engine.NodeID, _ map[string]any, options []string) (string, error) {
	return options[0], nil
}

func readExamplesGraph(t *testing.T, filename string) *model.Sim {
	t.Helper()
	_, err := os.ReadFile(filepath.Join("../../../examples", filename))
	require.NoError(t, err)

	var def model.Sim
	def.Graph = filepath.Join("../../../examples", filename)
	def.APIVersion = "sim/v1"
	def.Objective.Value = "1.0"
	return &def
}

func TestSimKernel_Examples(t *testing.T) {
	for _, tc := range []struct {
		file     string
		expected map[engine.NodeID]engine.Status
	}{
		{
			file: "linear_pipeline.yaml",
			expected: map[engine.NodeID]engine.Status{
				"A": engine.StatusSucceeded,
				"B": engine.StatusSucceeded,
				"C": engine.StatusSucceeded,
			},
		},
		{
			file: "fanout_fanin.yaml",
			expected: map[engine.NodeID]engine.Status{
				"ingest_batch":        engine.StatusSucceeded,
				"process_partition_a": engine.StatusSucceeded,
				"process_partition_b": engine.StatusSucceeded,
				"aggregate_results":   engine.StatusSucceeded,
			},
		},
		{
			file: "conditional_branch.yaml",
			expected: map[engine.NodeID]engine.Status{
				"check_amount":      engine.StatusSucceeded,
				"auto_approve":      engine.StatusSucceeded,
				"manual_review":     engine.StatusSkipped,
				"notify_customer":   engine.StatusSucceeded,
				"escalate_director": engine.StatusSkipped,
			},
		},
	} {
		t.Run(tc.file, func(t *testing.T) {
			def := readExamplesGraph(t, tc.file)

			reg := engine.NewRegistry()
			require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))
			require.NoError(t, nodes.RegisterDecision(reg, dummyChooser{}))

			m, errs := model.Compile(def, ".", reg)
			require.Zero(t, errs.Len(), errs.Error())

			p := dummyPolicy{
				id: "dummy",
				choose: func(_ context.Context, _ engine.NodeID, _ sim.Observation, options []string) (string, error) {
					return options[0], nil
				},
			}

			rec, err := kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(0), kernel.Options{})
			require.NoError(t, err)
			require.Equal(t, engine.StatusSucceeded, rec.FinalStatus)

			expectedPaths := len(tc.expected)
			for _, st := range tc.expected {
				if st == engine.StatusSkipped {
					expectedPaths--
				}
			}
			require.Len(t, rec.Path, expectedPaths)
		})
	}
}

func TestSimKernel_FanoutMakespan(t *testing.T) {
	def := readExamplesGraph(t, "fanout_fanin.yaml")

	def.Behaviors = map[string]model.Behavior{
		"ingest_batch": {
			Duration: "1h",
			Output:   map[string]model.ValueSpec{},
		},
		"process_partition_a": {
			Duration: "3h",
			Output:   map[string]model.ValueSpec{},
		},
		"process_partition_b": {
			Duration: "2h",
			Output:   map[string]model.ValueSpec{},
		},
		"aggregate_results": {
			Duration: "0h",
			Output:   map[string]model.ValueSpec{},
		},
	}

	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))
	require.NoError(t, nodes.RegisterDecision(reg, dummyChooser{}))

	m, errs := model.Compile(def, ".", reg)
	require.Zero(t, errs.Len(), errs.Error())

	p := dummyPolicy{
		id: "dummy",
		choose: func(_ context.Context, _ engine.NodeID, _ sim.Observation, options []string) (string, error) {
			return options[0], nil
		},
	}

	rec, err := kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(0), kernel.Options{})
	require.NoError(t, err)
	require.Equal(t, engine.StatusSucceeded, rec.FinalStatus)

	require.Equal(t, 4*time.Hour, rec.Duration)
}

func TestSimKernel_LoanGolden(t *testing.T) {
	_, err := os.ReadFile("../../../examples/simulation/loan/graph.yaml")
	require.NoError(t, err)

	def := &model.Sim{
		APIVersion: "sim/v1",
		Graph:      "../../../examples/simulation/loan/graph.yaml",
		Objective: model.Objective{
			Value: "1.0",
		},
		Behaviors: map[string]model.Behavior{
			"ai_score":     {Duration: "2h", Output: map[string]model.ValueSpec{}},
			"human_review": {Duration: "48h", Output: map[string]model.ValueSpec{}},
		},
	}

	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))
	require.NoError(t, nodes.RegisterDecision(reg, dummyChooser{}))

	m, errs := model.Compile(def, ".", reg)
	require.Zero(t, errs.Len(), errs.Error())

	p := dummyPolicy{
		id: "recession-v1",
		choose: func(_ context.Context, _ engine.NodeID, _ sim.Observation, options []string) (string, error) {
			return options[0], nil
		},
	}

	rec1, err := kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(42), kernel.Options{})
	require.NoError(t, err)

	rec2, err := kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(42), kernel.Options{})
	require.NoError(t, err)

	require.Equal(t, rec1, rec2)
}

func TestSimKernel_Abandonment(t *testing.T) {
	def := readExamplesGraph(t, "linear_pipeline.yaml")

	def.Behaviors = map[string]model.Behavior{
		"fetch_user": {
			Duration: "1h",
			Abandon:  &model.Abandon{P: 1.0},
			Output:   map[string]model.ValueSpec{},
		},
		"calculate_discount": {Duration: "1h", Output: map[string]model.ValueSpec{}},
		"generate_invoice":   {Duration: "1h", Output: map[string]model.ValueSpec{}},
	}

	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))
	require.NoError(t, nodes.RegisterDecision(reg, dummyChooser{}))

	m, errs := model.Compile(def, ".", reg)
	require.Zero(t, errs.Len(), errs.Error())

	p := dummyPolicy{
		id: "dummy",
		choose: func(_ context.Context, _ engine.NodeID, _ sim.Observation, options []string) (string, error) {
			return options[0], nil
		},
	}

	rec, err := kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(0), kernel.Options{})
	require.NoError(t, err)
	require.True(t, rec.Abandoned)
	require.True(t, rec.FinalStatus == engine.StatusFailed || rec.FinalStatus == engine.StatusCompensating)
}

func TestSimKernel_CriticalFailure(t *testing.T) {
	def := readExamplesGraph(t, "linear_pipeline.yaml")

	def.Graph = "../../../examples/linear_pipeline.yaml"

	def.Behaviors = map[string]model.Behavior{
		"fetch_user": {
			Duration: "1h",
			Fail:     &model.Fail{P: 1.0, Class: "critical"},
			Output:   map[string]model.ValueSpec{},
		},
		"calculate_discount": {Duration: "1h", Output: map[string]model.ValueSpec{}},
		"generate_invoice":   {Duration: "1h", Output: map[string]model.ValueSpec{}},
	}

	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))
	require.NoError(t, nodes.RegisterDecision(reg, dummyChooser{}))

	m, errs := model.Compile(def, ".", reg)
	require.Zero(t, errs.Len(), errs.Error())

	p := dummyPolicy{
		id: "dummy",
		choose: func(_ context.Context, _ engine.NodeID, _ sim.Observation, options []string) (string, error) {
			return options[0], nil
		},
	}

	rec, err := kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(0), kernel.Options{})
	require.NoError(t, err)
	require.False(t, rec.Abandoned)
	require.True(t, rec.FinalStatus == engine.StatusFailed || rec.FinalStatus == engine.StatusCompensating)
}

func TestSimKernel_InvalidPolicyChoice(t *testing.T) {
	_, err := os.ReadFile("../../../examples/simulation/loan/graph.yaml")
	require.NoError(t, err)

	def := &model.Sim{
		APIVersion: "sim/v1",
		Graph:      "../../../examples/simulation/loan/graph.yaml",
		Objective: model.Objective{
			Value: "1.0",
		},
		Behaviors: map[string]model.Behavior{
			"ai_score":     {Duration: "2h", Output: map[string]model.ValueSpec{}},
			"human_review": {Duration: "48h", Output: map[string]model.ValueSpec{}},
		},
	}

	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))
	require.NoError(t, nodes.RegisterDecision(reg, dummyChooser{}))

	m, errs := model.Compile(def, ".", reg)
	require.Zero(t, errs.Len(), errs.Error())

	p := dummyPolicy{
		id: "recession-v1",
		choose: func(_ context.Context, _ engine.NodeID, _ sim.Observation, _ []string) (string, error) {
return "invalid_choice", nil
		},
	}

	rec, err := kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(42), kernel.Options{})
	require.NoError(t, err)
	require.True(t, rec.FinalStatus == engine.StatusFailed || rec.FinalStatus == engine.StatusCompensating)
}

func TestSimKernel_SameInstantHeapOrder(t *testing.T) {
	// Write a test where two independent branches finish at the EXACT same virtual time (1h).
	// One is node A, the other is node Z. The heap should pop A first.
	// Since kernel.RunEpisode processes the heap internally and puts successful completions in rec.Path,
	// rec.Path should end up with [A, Z].

	def := &model.Sim{
		APIVersion: "sim/v1",
		Graph:      "../../../examples/same_instant_test.yaml",
		Objective: model.Objective{
			Value: "1.0",
		},
		Behaviors: map[string]model.Behavior{
			"A": {Duration: "1h", Output: map[string]model.ValueSpec{}},
			"Z": {Duration: "1h", Output: map[string]model.ValueSpec{}},
		},
	}

	graphYAML := `
apiVersion: graph/v1
id: same-instant-test
nodes:
  - id: A
    type: transform
    config: {set: {done: true}}
  - id: Z
    type: transform
    config: {set: {done: true}}
edges: []
`
	err := os.WriteFile("../../../examples/same_instant_test.yaml", []byte(graphYAML), 0644)
	require.NoError(t, err)
	defer os.Remove("../../../examples/same_instant_test.yaml")

	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))
	require.NoError(t, nodes.RegisterDecision(reg, dummyChooser{}))

	m, errs := model.Compile(def, ".", reg)
	require.Zero(t, errs.Len(), errs.Error())

	p := dummyPolicy{
		id: "dummy",
		choose: func(_ context.Context, _ engine.NodeID, _ sim.Observation, options []string) (string, error) {
			return options[0], nil
		},
	}

	rec, err := kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(0), kernel.Options{})
	require.NoError(t, err)

	require.Equal(t, []engine.NodeID{"A", "Z"}, rec.Path)
}

// dummySink for verifying events.
type dummySink struct {
	events []*engine.Event
}

func (s *dummySink) Emit(_ context.Context, e *engine.Event) error {
	s.events = append(s.events, e)
	return nil
}

func TestSimKernel_IdenticalEventsCheck(t *testing.T) {
	_, err := os.ReadFile("../../../examples/simulation/loan/graph.yaml")
	require.NoError(t, err)

	def := &model.Sim{
		APIVersion: "sim/v1",
		Graph:      "../../../examples/simulation/loan/graph.yaml",
		Objective: model.Objective{
			Value: "1.0",
		},
		Behaviors: map[string]model.Behavior{
			"ai_score":     {Duration: "2h", Output: map[string]model.ValueSpec{}},
			"human_review": {Duration: "48h", Output: map[string]model.ValueSpec{}},
		},
	}

	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))
	require.NoError(t, nodes.RegisterDecision(reg, dummyChooser{}))

	m, errs := model.Compile(def, ".", reg)
	require.Zero(t, errs.Len(), errs.Error())

	p := dummyPolicy{
		id: "recession-v1",
		choose: func(_ context.Context, _ engine.NodeID, _ sim.Observation, options []string) (string, error) {
			return options[0], nil
		},
	}

	sink1 := &dummySink{}
	opts1 := kernel.Options{Events: sink1}
	_, err = kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(42), opts1)
	require.NoError(t, err)

	sink2 := &dummySink{}
	opts2 := kernel.Options{Events: sink2}
	_, err = kernel.RunEpisode(context.Background(), m, sim.World{}, p, sampler.NewMonteCarlo(42), opts2)
	require.NoError(t, err)

	require.Equal(t, len(sink1.events), len(sink2.events))
	for i := range sink1.events {
		e1 := sink1.events[i]
		e2 := sink2.events[i]

		require.Equal(t, e1.Type(), e2.Type())
		require.Equal(t, e1.NodeID(), e2.NodeID())
		require.Equal(t, e1.Sequence(), e2.Sequence())
		require.Equal(t, e1.Timestamp(), e2.Timestamp())
	}
}
