package kernel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

const (
	examplesDir = "../../../examples"
	loanDir     = "../../../examples/simulation/loan"
)

var epoch = time.Unix(0, 0).UTC()

// stubChooser satisfies the decision node registration used by model.Compile;
// the kernel registers its own policy-backed chooser.
type stubChooser struct{}

func (stubChooser) Choose(_ context.Context, _ engine.NodeID, _ map[string]any, options []string) (string, error) {
	return options[0], nil
}

func compileRegistry(t *testing.T) *engine.Registry {
	t.Helper()
	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))
	require.NoError(t, nodes.RegisterDecision(reg, stubChooser{}))
	return reg
}

// fixedPolicy chooses with fn, or the first option when fn is nil.
type fixedPolicy struct {
	fn func(decision engine.NodeID, options []string) string
}

func (fixedPolicy) ID() string       { return "fixed" }
func (fixedPolicy) Describe() string { return "fixed test policy" }
func (p fixedPolicy) Choose(_ context.Context, decision engine.NodeID, _ sim.Observation, options []string) (string, error) {
	if p.fn == nil {
		return options[0], nil
	}
	return p.fn(decision, options), nil
}

// simBody is the sim/v1 document for a test model; graph is filled in.
type simBody struct {
	objective   string
	constraints string // YAML lines indented under objective.constraints
	behaviors   string // YAML lines indented under behaviors
}

// compileInDir writes graphYAML into a temp dir and compiles a sim/v1 model
// for it. Nothing is written outside t.TempDir().
func compileInDir(t *testing.T, graphYAML string, body simBody) *model.CompiledModel {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.yaml"), []byte(graphYAML), 0o600))
	return compileSim(t, dir, "./graph.yaml", body)
}

// compileExample compiles a sim/v1 model over one of the examples/*.yaml graphs.
func compileExample(t *testing.T, file string, body simBody) *model.CompiledModel {
	t.Helper()
	return compileSim(t, examplesDir, "./"+file, body)
}

func compileSim(t *testing.T, baseDir, graph string, body simBody) *model.CompiledModel {
	t.Helper()
	if body.objective == "" {
		body.objective = "-run.cost"
	}
	doc := "apiVersion: sim/v1\nid: test\ngraph: " + graph + "\nworlds:\n  w: {}\n"
	if body.behaviors != "" {
		doc += "behaviors:\n" + body.behaviors
	}
	doc += "objective:\n  name: obj\n  maximize: mean\n  value: \"" + body.objective + "\"\n"
	if body.constraints != "" {
		doc += "  constraints:\n" + body.constraints
	}
	def, err := model.ParseSimModel([]byte(doc), engine.FormatYAML)
	require.NoError(t, err, doc)
	m, errs := model.Compile(def, baseDir, compileRegistry(t))
	if errs != nil && errs.Len() > 0 {
		t.Fatalf("compile: %v", errs.Error())
	}
	return m
}

// eventSink records every event.
type eventSink struct {
	events []*engine.Event
}

func (s *eventSink) Emit(_ context.Context, e *engine.Event) error {
	s.events = append(s.events, e)
	return nil
}

// nodeStatuses replays the event stream into the last status of each node.
func (s *eventSink) nodeStatuses() map[engine.NodeID]engine.Status {
	out := make(map[engine.NodeID]engine.Status)
	for _, e := range s.events {
		switch p := e.Payload().(type) {
		case engine.NodeStateChangedPayload:
			out[e.NodeID()] = p.NewStatus
		case engine.NodeCompletedPayload:
			out[e.NodeID()] = p.Status
		case engine.NodeFailedPayload:
			out[e.NodeID()] = p.Status
		}
	}
	return out
}

func (s *eventSink) json(t *testing.T) []string {
	t.Helper()
	out := make([]string, 0, len(s.events))
	for _, e := range s.events {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		out = append(out, string(b))
	}
	return out
}

func run(t *testing.T, m *model.CompiledModel, p sim.Policy, seed uint64) (sim.RunRecord, *eventSink) {
	t.Helper()
	sink := &eventSink{}
	world := sim.World{ID: "w"}
	if params, ok := m.Def.Worlds["w"]; ok {
		world.Params = params
	}
	rec, err := kernel.RunEpisode(context.Background(), m, world, p, sampler.NewMonteCarlo(seed), kernel.Options{Events: sink})
	require.NoError(t, err)
	return rec, sink
}

func TestSimKernel_Examples(t *testing.T) {
	for _, tc := range []struct {
		file     string
		expected map[engine.NodeID]engine.Status
		path     []engine.NodeID
	}{
		{
			file: "linear_pipeline.yaml",
			expected: map[engine.NodeID]engine.Status{
				"fetch_user":         engine.StatusSucceeded,
				"calculate_discount": engine.StatusSucceeded,
				"generate_invoice":   engine.StatusSucceeded,
			},
			path: []engine.NodeID{"fetch_user", "calculate_discount", "generate_invoice"},
		},
		{
			file: "fanout_fanin.yaml",
			expected: map[engine.NodeID]engine.Status{
				"ingest_batch":        engine.StatusSucceeded,
				"process_partition_a": engine.StatusSucceeded,
				"process_partition_b": engine.StatusSucceeded,
				"aggregate_results":   engine.StatusSucceeded,
			},
			path: []engine.NodeID{"ingest_batch", "process_partition_a", "process_partition_b", "aggregate_results"},
		},
		{
			file: "conditional_branch.yaml",
			expected: map[engine.NodeID]engine.Status{
				"evaluate_risk":       engine.StatusSucceeded,
				"manual_review_queue": engine.StatusSucceeded,
				"auto_approve":        engine.StatusSkipped,
				"notify_decision":     engine.StatusSucceeded,
			},
			path: []engine.NodeID{"evaluate_risk", "manual_review_queue", "notify_decision"},
		},
	} {
		t.Run(tc.file, func(t *testing.T) {
			m := compileExample(t, tc.file, simBody{})
			rec, sink := run(t, m, fixedPolicy{}, 1)
			require.Equal(t, engine.StatusSucceeded, rec.FinalStatus)
			require.Equal(t, tc.expected, sink.nodeStatuses())
			require.Equal(t, tc.path, rec.Path)
			require.Zero(t, rec.Duration, "real nodes take zero virtual time")
			require.NotNil(t, rec.Constraints)

			last := sink.events[len(sink.events)-1]
			require.Equal(t, engine.EventTypeGraphCompleted, last.Type())
			for _, e := range sink.events {
				require.Equal(t, epoch, e.Timestamp())
			}
		})
	}
}

func TestSimKernel_FanoutMakespan(t *testing.T) {
	m := compileExample(t, "fanout_fanin.yaml", simBody{
		behaviors: "  ingest_batch: {duration: 1h, cost: 1}\n" +
			"  process_partition_a: {duration: 3h, cost: 2}\n" +
			"  process_partition_b: {duration: 2h, cost: 4}\n",
	})
	rec, sink := run(t, m, fixedPolicy{}, 0)
	require.Equal(t, engine.StatusSucceeded, rec.FinalStatus)
	// 1h + max(3h, 2h); aggregate_results is a real node with zero duration.
	require.Equal(t, 4*time.Hour, rec.Duration)
	require.InDelta(t, 7.0, rec.Cost, 1e-9)
	require.InDelta(t, -7.0, rec.Objective, 1e-9)
	require.Equal(t, []engine.NodeID{"ingest_batch", "process_partition_b", "process_partition_a", "aggregate_results"}, rec.Path)

	// Both partitions start together at 1h.
	started := map[engine.NodeID]time.Time{}
	for _, e := range sink.events {
		if p, ok := e.Payload().(engine.NodeStateChangedPayload); ok && p.NewStatus == engine.StatusRunning {
			started[e.NodeID()] = e.Timestamp()
		}
	}
	require.Equal(t, epoch.Add(time.Hour), started["process_partition_a"])
	require.Equal(t, epoch.Add(time.Hour), started["process_partition_b"])
	require.Equal(t, epoch.Add(4*time.Hour), started["aggregate_results"])

	// With a behavior on the join, it adds to the makespan.
	m = compileExample(t, "fanout_fanin.yaml", simBody{
		behaviors: "  ingest_batch: {duration: 1h}\n" +
			"  process_partition_a: {duration: 3h}\n" +
			"  process_partition_b: {duration: 2h}\n" +
			"  aggregate_results: {duration: 30m}\n",
	})
	rec, _ = run(t, m, fixedPolicy{}, 0)
	require.Equal(t, 4*time.Hour+30*time.Minute, rec.Duration)
}

// TestSimKernel_SameInstantCompletionsBatch checks that every completion at
// the same virtual instant is applied, in node ID order, before the
// scheduler starts their successors.
func TestSimKernel_SameInstantCompletionsBatch(t *testing.T) {
	graph := `apiVersion: graph/v1
id: same-instant
nodes:
  - {id: Z, type: passthrough}
  - {id: A, type: passthrough}
  - {id: A2, type: passthrough}
  - id: join
    type: passthrough
edges:
  - {from: A, to: A2}
  - {from: A2, to: join}
  - {from: Z, to: join}
`
	m := compileInDir(t, graph, simBody{
		behaviors: "  A: {duration: 1h}\n  Z: {duration: 1h}\n",
	})
	rec, sink := run(t, m, fixedPolicy{}, 0)
	require.Equal(t, engine.StatusSucceeded, rec.FinalStatus)
	// A and Z both finish at 1h and pop in node ID order; A2 (zero duration)
	// only starts after both have been applied.
	require.Equal(t, []engine.NodeID{"A", "Z", "A2", "join"}, rec.Path)
	require.Equal(t, time.Hour, rec.Duration)

	var order []string
	for _, e := range sink.events {
		if p, ok := e.Payload().(engine.NodeStateChangedPayload); ok && p.NewStatus == engine.StatusRunning {
			order = append(order, fmt.Sprintf("%s@%s", e.NodeID(), e.Timestamp().Sub(epoch)))
		}
	}
	require.Equal(t, []string{"A@0s", "Z@0s", "A2@1h0m0s", "join@1h0m0s"}, order)
}

// TestSimKernel_SkippedChainTerminates checks the loop ends when only skipped
// nodes remain.
func TestSimKernel_SkippedChainTerminates(t *testing.T) {
	graph := `apiVersion: graph/v1
id: skipped-chain
nodes:
  - id: root
    type: transform
    config: {set: {go: false}}
  - {id: b, type: passthrough}
  - {id: c, type: passthrough}
  - {id: d, type: passthrough}
edges:
  - {from: root, to: b, condition: "upstream.output.go == true"}
  - {from: b, to: c}
  - {from: c, to: d}
`
	m := compileInDir(t, graph, simBody{objective: "double(size(run.path))"})
	rec, sink := run(t, m, fixedPolicy{}, 0)
	require.Equal(t, engine.StatusSucceeded, rec.FinalStatus)
	require.Equal(t, []engine.NodeID{"root"}, rec.Path)
	require.InDelta(t, 1.0, rec.Objective, 1e-9)
	require.Equal(t, map[engine.NodeID]engine.Status{
		"root": engine.StatusSucceeded,
		"b":    engine.StatusSkipped,
		"c":    engine.StatusSkipped,
		"d":    engine.StatusSkipped,
	}, sink.nodeStatuses())
}

// loanEpisode compiles the real loan model and the recession-v1 policy.
func loanEpisode(t *testing.T) (*model.CompiledModel, sim.Policy) {
	t.Helper()
	simYAML, err := os.ReadFile(filepath.Join(loanDir, "sim.yaml"))
	require.NoError(t, err)
	def, err := model.ParseSimModel(simYAML, engine.FormatYAML)
	require.NoError(t, err)
	m, errs := model.Compile(def, loanDir, compileRegistry(t))
	if errs != nil && errs.Len() > 0 {
		t.Fatalf("compile loan: %v", errs.Error())
	}

	polYAML, err := os.ReadFile(filepath.Join(loanDir, "policies", "recession-v1.yaml"))
	require.NoError(t, err)
	pdef, err := model.ParsePolicy(polYAML, engine.FormatYAML)
	require.NoError(t, err)
	p, perrs := model.CompilePolicy(pdef, m)
	if perrs != nil && perrs.Len() > 0 {
		t.Fatalf("compile policy: %v", perrs.Error())
	}
	return m, p
}

func loanWorld(m *model.CompiledModel, id string) sim.World {
	return sim.World{ID: sim.WorldID(id), Params: m.Def.Worlds[id]}
}

func TestSimKernel_LoanGolden(t *testing.T) {
	m, p := loanEpisode(t)
	require.Equal(t, "loan-underwriting@recession-v1", p.ID())

	for _, world := range []string{"base", "recession"} {
		t.Run(world, func(t *testing.T) {
			w := loanWorld(m, world)
			runOnce := func() (sim.RunRecord, *eventSink) {
				sink := &eventSink{}
				rec, err := kernel.RunEpisode(context.Background(), m, w, p, sampler.NewMonteCarlo(42), kernel.Options{Events: sink})
				require.NoError(t, err)
				return rec, sink
			}
			rec1, sink1 := runOnce()
			rec2, sink2 := runOnce()

			require.Equal(t, rec1, rec2)
			require.NotEmpty(t, sink1.events)
			require.Equal(t, sink1.json(t), sink2.json(t))
			for i := range sink1.events {
				require.Equal(t, sink1.events[i], sink2.events[i])
				require.Equal(t, kernel.DefaultExecutionID(p.ID(), w.ID), sink1.events[i].ExecutionID())
			}

			require.False(t, math.IsNaN(rec1.Objective) || math.IsInf(rec1.Objective, 0))
			require.NotZero(t, rec1.Objective)
			require.Equal(t, "score", rec1.Decisions["screen"])
			require.Equal(t, sim.WorldID(world), rec1.World)
			require.Equal(t, p.ID(), rec1.PolicyID)
			require.NotNil(t, rec1.Constraints)

			approved := false
			for _, id := range rec1.Path {
				if id == "approve" {
					approved = true
				}
			}
			// The objective is -cost plus +1000 or -loss when approved.
			if !approved {
				require.InDelta(t, -rec1.Cost, rec1.Objective, 1e-9)
			} else {
				loss := map[string]float64{"base": 4000, "recession": 6000}[world]
				good, bad := -rec1.Cost+1000, -rec1.Cost-loss
				require.True(t, rec1.Objective == good || rec1.Objective == bad, "objective %v", rec1.Objective)
			}
			t.Logf("world=%s objective=%v cost=%v duration=%v abandoned=%v status=%s path=%v decisions=%v",
				world, rec1.Objective, rec1.Cost, rec1.Duration, rec1.Abandoned, rec1.FinalStatus, rec1.Path, rec1.Decisions)
		})
	}
}

// TestSimKernel_LoanMonteCarloMean checks the kernel against the exact value
// of recession-v1 from the prototype (docs/simulation/prototypes/loan_sim.py):
// EV = 202.525 in recession. A coarse 4 SE bound keeps the test fast.
func TestSimKernel_LoanMonteCarloMean(t *testing.T) {
	if testing.Short() {
		t.Skip("Monte Carlo check")
	}
	m, p := loanEpisode(t)
	w := loanWorld(m, "recession")
	const n = 5000
	var sum, sumSq float64
	for r := range n {
		rec, err := kernel.RunEpisode(context.Background(), m, w, p, sampler.NewMonteCarlo(sim.Seed(42, w.ID, r)), kernel.Options{})
		require.NoError(t, err)
		sum += rec.Objective
		sumSq += rec.Objective * rec.Objective
	}
	mean := sum / n
	se := math.Sqrt((sumSq/n - mean*mean) / n)
	t.Logf("recession-v1 in recession: mean %.2f (SE %.2f, n=%d), exact 202.525", mean, se, n)
	require.InDelta(t, 202.525, mean, 4*se)
}

func TestSimKernel_Abandonment(t *testing.T) {
	graph := `apiVersion: graph/v1
id: abandon
nodes:
  - {id: first, type: passthrough}
  - {id: second, type: passthrough}
  - {id: side, type: passthrough}
  - {id: third, type: passthrough}
edges:
  - {from: first, to: second}
  - {from: first, to: side}
  - {from: second, to: third}
`
	m := compileInDir(t, graph, simBody{
		objective: "run.abandoned ? -run.cost - double(size(run.path)) * 100.0 : 1.0",
		constraints: "    second_failed: \"nodes.second.status == 'Failed'\"\n" +
			"    third_pending: \"nodes.third.status == 'Pending'\"\n" +
			"    first_out: \"nodes.first.output.x == 1\"\n",
		behaviors: "  first: {duration: 1h, cost: 10, output: {x: {fixed: 1}}}\n" +
			"  second: {duration: 5h, cost: 25, abandon: {p: 1.0}}\n" +
			"  side: {duration: 2h, cost: 7}\n",
	})
	rec, sink := run(t, m, fixedPolicy{}, 3)

	require.True(t, rec.Abandoned)
	require.Equal(t, engine.StatusFailed, rec.FinalStatus)
	require.Equal(t, time.Hour, rec.Duration, "the episode ends when second abandons at 1h")
	require.Equal(t, []engine.NodeID{"first"}, rec.Path)
	// first (10) + second (25, charged although abandoned). side becomes
	// ready at the same instant but sorts after second, so it never starts.
	require.InDelta(t, 35.0, rec.Cost, 1e-9)
	require.InDelta(t, -35.0-100.0, rec.Objective, 1e-9)
	require.Equal(t, map[string]bool{"second_failed": true, "third_pending": true, "first_out": true}, rec.Constraints)

	statuses := sink.nodeStatuses()
	require.Equal(t, engine.StatusFailed, statuses["second"])
	require.Equal(t, engine.StatusSkipped, statuses["side"], "ready but not started")
	_, thirdSeen := statuses["third"]
	require.False(t, thirdSeen, "third never becomes ready")
	last := sink.events[len(sink.events)-1]
	require.Equal(t, engine.EventTypeGraphFailed, last.Type())
	require.Equal(t, epoch.Add(time.Hour), last.Timestamp())
}

func TestSimKernel_AbandonmentFailsInFlightNodes(t *testing.T) {
	graph := `apiVersion: graph/v1
id: abandon-inflight
nodes:
  - {id: a_long, type: passthrough}
  - {id: root, type: passthrough}
  - {id: z_quit, type: passthrough}
edges:
  - {from: root, to: z_quit}
`
	m := compileInDir(t, graph, simBody{
		behaviors: "  a_long: {duration: 10h, cost: 3}\n" +
			"  root: {duration: 2h}\n" +
			"  z_quit: {abandon: {p: 1.0}, cost: 4}\n",
	})
	rec, sink := run(t, m, fixedPolicy{}, 0)
	require.True(t, rec.Abandoned)
	require.Equal(t, 2*time.Hour, rec.Duration)
	require.InDelta(t, 7.0, rec.Cost, 1e-9)
	require.InDelta(t, -7.0, rec.Objective, 1e-9)
	require.Equal(t, []engine.NodeID{"root"}, rec.Path)
	require.Equal(t, map[engine.NodeID]engine.Status{
		"a_long": engine.StatusFailed,
		"root":   engine.StatusSucceeded,
		"z_quit": engine.StatusFailed,
	}, sink.nodeStatuses())
}

func TestSimKernel_LoanAbandonment(t *testing.T) {
	m, p := loanEpisode(t)
	w := loanWorld(m, "base")
	for seed := range uint64(500) {
		rec, err := kernel.RunEpisode(context.Background(), m, w, p, sampler.NewMonteCarlo(seed), kernel.Options{})
		require.NoError(t, err)
		if !rec.Abandoned {
			continue
		}
		// Only good applicants leave during review: score (20) + review (150).
		require.Equal(t, engine.StatusFailed, rec.FinalStatus)
		require.InDelta(t, 170.0, rec.Cost, 1e-9)
		require.InDelta(t, -170.0, rec.Objective, 1e-9)
		require.NotNil(t, rec.Constraints)
		require.Equal(t, map[engine.NodeID]string{"screen": "score", "route": "review"}, rec.Decisions)
		require.Equal(t, []engine.NodeID{"screen", "ai_score", "route"}, rec.Path)
		return
	}
	t.Fatal("no abandoned episode in 500 seeds")
}

const failGraph = `apiVersion: graph/v1
id: failing
nodes:
  - {id: start, type: passthrough}
  - {id: bad, type: passthrough}
  - {id: good, type: passthrough}
  - {id: after_bad, type: passthrough}
  - {id: after_good, type: passthrough}
edges:
  - {from: start, to: bad}
  - {from: start, to: good}
  - {from: bad, to: after_bad}
  - {from: good, to: after_good}
`

func TestSimKernel_FailurePolicies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		policy   string
		class    string
		status   engine.Status
		statuses map[engine.NodeID]engine.Status
		duration time.Duration
	}{
		{
			name:   "critical under fail_graph",
			policy: "failurePolicy: {onCriticalFailure: fail_graph}\n",
			class:  "critical",
			status: engine.StatusFailed,
			statuses: map[engine.NodeID]engine.Status{
				"start": engine.StatusSucceeded, "bad": engine.StatusFailed, "good": engine.StatusRunning,
			},
			duration: 0,
		},
		{
			name:   "critical under default compensate",
			class:  "critical",
			status: engine.StatusCompensating,
			statuses: map[engine.NodeID]engine.Status{
				"start": engine.StatusSucceeded, "bad": engine.StatusFailed, "good": engine.StatusRunning,
			},
			duration: 0,
		},
		{
			name:   "non-retryable under default fail_graph",
			class:  "non_retryable",
			status: engine.StatusFailed,
			statuses: map[engine.NodeID]engine.Status{
				"start": engine.StatusSucceeded, "bad": engine.StatusFailed, "good": engine.StatusRunning,
			},
			duration: 0,
		},
		{
			name:   "retryable is not retried and continue runs the other branch",
			policy: "failurePolicy: {onNodeFailure: continue}\n",
			class:  "retryable",
			status: engine.StatusFailed,
			statuses: map[engine.NodeID]engine.Status{
				"start": engine.StatusSucceeded, "bad": engine.StatusFailed, "good": engine.StatusSucceeded,
				"after_bad": engine.StatusSkipped, "after_good": engine.StatusSucceeded,
			},
			duration: 3 * time.Hour,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := compileInDir(t, failGraph+tc.policy, simBody{
				behaviors: "  bad: {duration: 1h, fail: {p: 1.0, class: " + tc.class + "}}\n" +
					"  good: {duration: 3h}\n",
			})
			rec, sink := run(t, m, fixedPolicy{}, 0)
			require.False(t, rec.Abandoned)
			require.Equal(t, tc.status, rec.FinalStatus)
			require.Equal(t, tc.statuses, sink.nodeStatuses())
			require.Equal(t, tc.duration, rec.Duration)
			require.NotNil(t, rec.Constraints)

			var failed *engine.NodeFailedPayload
			for _, e := range sink.events {
				if p, ok := e.Payload().(engine.NodeFailedPayload); ok && e.NodeID() == "bad" {
					failed = &p
				}
			}
			require.NotNil(t, failed)
			want := engine.ClassNonRetryable
			if tc.class == "critical" {
				want = engine.ClassCritical
			}
			require.Equal(t, want, failed.ErrorClass)
		})
	}
}

func TestSimKernel_InvalidPolicyChoice(t *testing.T) {
	m, _ := loanEpisode(t)
	p := fixedPolicy{fn: func(engine.NodeID, []string) string { return "maybe" }}
	sink := &eventSink{}
	rec, err := kernel.RunEpisode(context.Background(), m, loanWorld(m, "base"), p, sampler.NewMonteCarlo(42), kernel.Options{Events: sink})
	require.NoError(t, err)
	require.Equal(t, engine.StatusFailed, rec.FinalStatus)
	require.Empty(t, rec.Decisions)
	require.Empty(t, rec.Path)
	require.Zero(t, rec.Cost)
	require.Equal(t, engine.StatusFailed, sink.nodeStatuses()["screen"])

	var msg string
	for _, e := range sink.events {
		if p, ok := e.Payload().(engine.NodeFailedPayload); ok {
			msg = p.ErrorMessage
		}
	}
	require.Contains(t, msg, `invalid option "maybe"`)
}

func TestSimKernel_ExecutionID(t *testing.T) {
	m := compileExample(t, "linear_pipeline.yaml", simBody{})
	sink := &eventSink{}
	_, err := kernel.RunEpisode(context.Background(), m, sim.World{ID: "w"}, fixedPolicy{}, sampler.NewMonteCarlo(0),
		kernel.Options{Events: sink, ExecutionID: "custom-id"})
	require.NoError(t, err)
	require.NotEmpty(t, sink.events)
	for _, e := range sink.events {
		require.Equal(t, engine.ExecutionID("custom-id"), e.ExecutionID())
	}
	require.Equal(t, engine.ExecutionID("sim-fixed-w"), kernel.DefaultExecutionID("fixed", "w"))
}
