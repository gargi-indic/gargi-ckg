package reactive_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/reactive"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

type mockSink struct {
	events []*engine.Event
}

func (s *mockSink) Emit(_ context.Context, ev *engine.Event) error {
	s.events = append(s.events, ev)
	return nil
}

func setupTestGraph(t *testing.T) (*engine.Graph, *engine.GraphState, *reactive.TransitionEmitter, *mockSink) {
	n1, _ := engine.NewNode("A", "test", nil)
	n2, _ := engine.NewNode("B", "test", nil)
	n3, _ := engine.NewNode("C", "test", nil)
	e1, _ := engine.NewEdge("A", "B", "", nil)
	e2, _ := engine.NewEdge("A", "C", "", nil)

	g, err := engine.NewGraph("test-graph", []*engine.Node{n1, n2, n3}, []*engine.Edge{e1, e2})
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}

	execID := engine.ExecutionID("exec-1")
	state, err := engine.NewGraphState(execID, g)
	if err != nil {
		t.Fatalf("failed to create state: %v", err)
	}

	sink := &mockSink{}
	emitter := reactive.NewTransitionEmitter(execID, nil, func() time.Time { return time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC) }, sink)

	return g, state, emitter, sink
}

func TestTerminalDetector_AssessState_Completed(t *testing.T) {
	g, state, emitter, sink := setupTestGraph(t)
	detector := reactive.NewTerminalDetector(g, nil, emitter)
	ctx := context.Background()

	// Initially running
	assessment, err := detector.AssessState(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if assessment.Phase != reactive.PhaseRunning {
		t.Errorf("expected PhaseRunning, got %s", assessment.Phase)
	}

	// Advance to Succeeded
	statusReady := engine.StatusReady
	statusRunning := engine.StatusRunning
	statusSucceeded := engine.StatusSucceeded
	statusSkipped := engine.StatusSkipped

	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusReady, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusSucceeded, nil, nil, nil))

	state, _ = state.Apply(engine.NewStateMutation("B", state.Version(), &statusReady, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("B", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("B", state.Version(), &statusSucceeded, nil, nil, nil))

	state, _ = state.Apply(engine.NewStateMutation("C", state.Version(), &statusSkipped, nil, nil, nil))

	assessment, err = detector.AssessState(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Phase != reactive.PhaseCompleted {
		t.Errorf("expected PhaseCompleted, got %s", assessment.Phase)
	}
	if assessment.FinalGraphState.Status() != engine.StatusSucceeded {
		t.Errorf("expected final graph status Succeeded, got %s", assessment.FinalGraphState.Status())
	}

	if len(sink.events) == 0 || sink.events[0].Type() != engine.EventTypeGraphCompleted {
		t.Errorf("expected EventTypeGraphCompleted event emitted")
	}
}

func TestTerminalDetector_AssessState_FailFast(t *testing.T) {
	g, state, emitter, sink := setupTestGraph(t)
	// Default is PolicyFailGraph
	detector := reactive.NewTerminalDetector(g, nil, emitter)
	ctx := context.Background()

	// Advance A to Succeeded, B to Failed
	statusReady := engine.StatusReady
	statusRunning := engine.StatusRunning
	statusSucceeded := engine.StatusSucceeded
	statusFailed := engine.StatusFailed
	errMsg := "some error"

	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusReady, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusSucceeded, nil, nil, nil))

	state, _ = state.Apply(engine.NewStateMutation("B", state.Version(), &statusReady, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("B", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("B", state.Version(), &statusFailed, nil, nil, &errMsg))

	assessment, err := detector.AssessState(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Phase != reactive.PhaseFailed {
		t.Errorf("expected PhaseFailed, got %s", assessment.Phase)
	}
	if assessment.FinalGraphState.Status() != engine.StatusFailed {
		t.Errorf("expected final graph status Failed, got %s", assessment.FinalGraphState.Status())
	}
	if len(sink.events) == 0 || sink.events[0].Type() != engine.EventTypeGraphFailed {
		t.Errorf("expected EventTypeGraphFailed event emitted")
	}
}

func TestTerminalDetector_AssessState_Continue(t *testing.T) {
	g, state, emitter, sink := setupTestGraph(t)
	fp := &engine.FailurePolicy{OnNodeFailure: reactive.PolicyContinue, OnCriticalFailure: reactive.PolicyCompensate}
	detector := reactive.NewTerminalDetector(g, fp, emitter)
	ctx := context.Background()

	// A succeeded, B failed, C is ready
	statusReady := engine.StatusReady
	statusRunning := engine.StatusRunning
	statusSucceeded := engine.StatusSucceeded
	statusFailed := engine.StatusFailed

	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusReady, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusSucceeded, nil, nil, nil))

	state, _ = state.Apply(engine.NewStateMutation("B", state.Version(), &statusReady, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("B", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("B", state.Version(), &statusFailed, nil, nil, nil))

	state, _ = state.Apply(engine.NewStateMutation("C", state.Version(), &statusReady, nil, nil, nil))

	assessment, err := detector.AssessState(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Phase != reactive.PhaseRunning {
		t.Errorf("expected PhaseRunning due to continue policy, got %s", assessment.Phase)
	}
	if assessment.FinalGraphState.Status() != engine.StatusPending {
		t.Errorf("expected final graph status Pending, got %s", assessment.FinalGraphState.Status())
	}

	// Now C finishes
	state, _ = state.Apply(engine.NewStateMutation("C", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("C", state.Version(), &statusSucceeded, nil, nil, nil))
	assessment, err = detector.AssessState(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Phase != reactive.PhaseFailed {
		t.Errorf("expected PhaseFailed after independent branch finished, got %s", assessment.Phase)
	}
	if assessment.FinalGraphState.Status() != engine.StatusFailed {
		t.Errorf("expected final graph status Failed, got %s", assessment.FinalGraphState.Status())
	}
	if len(sink.events) == 0 || sink.events[0].Type() != engine.EventTypeGraphFailed {
		t.Errorf("expected EventTypeGraphFailed event emitted")
	}
}

func TestTerminalDetector_AssessState_CriticalFailure_Compensate(t *testing.T) {
	g, state, emitter, _ := setupTestGraph(t)
	// Default policy for critical is Compensate
	detector := reactive.NewTerminalDetector(g, nil, emitter)
	ctx := context.Background()

	statusReady := engine.StatusReady
	statusRunning := engine.StatusRunning
	statusFailed := engine.StatusFailed
	errMsg := "critical error"

	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusReady, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusFailed, nil, nil, &errMsg))

	criticalErr := engine.NewCriticalError(errors.New("db down"))
	detector.RecordNodeFailure("A", criticalErr)

	assessment, err := detector.AssessState(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Phase != reactive.PhaseCompensating {
		t.Errorf("expected PhaseCompensating, got %s", assessment.Phase)
	}
	if assessment.FinalGraphState.Status() != engine.StatusCompensating {
		t.Errorf("expected final graph status Compensating, got %s", assessment.FinalGraphState.Status())
	}
}

func TestTerminalDetector_AssessState_CriticalFailure_FailGraph(t *testing.T) {
	g, state, emitter, _ := setupTestGraph(t)
	fp := &engine.FailurePolicy{OnNodeFailure: reactive.PolicyFailGraph, OnCriticalFailure: reactive.PolicyFailGraph}
	detector := reactive.NewTerminalDetector(g, fp, emitter)
	ctx := context.Background()

	statusReady := engine.StatusReady
	statusRunning := engine.StatusRunning
	statusFailed := engine.StatusFailed

	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusReady, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusFailed, nil, nil, nil))

	criticalErr := engine.NewCriticalError(errors.New("db down"))

	// Pass criticalErr directly in causes
	assessment, err := detector.AssessState(ctx, state, criticalErr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Phase != reactive.PhaseFailed {
		t.Errorf("expected PhaseFailed, got %s", assessment.Phase)
	}
	if assessment.FinalGraphState.Status() != engine.StatusFailed {
		t.Errorf("expected final graph status Failed, got %s", assessment.FinalGraphState.Status())
	}
}

func TestTerminalDetector_AssessState_PendingCanProgress(t *testing.T) {
	g, state, emitter, _ := setupTestGraph(t)
	fp := &engine.FailurePolicy{OnNodeFailure: reactive.PolicyContinue, OnCriticalFailure: reactive.PolicyCompensate}
	detector := reactive.NewTerminalDetector(g, fp, emitter)
	ctx := context.Background()

	// A failed, B is pending. Since B depends on A and A failed, B can't progress.
	// But C is pending and depends on A. Wait, both B and C depend on A.
	statusReady := engine.StatusReady
	statusRunning := engine.StatusRunning
	statusFailed := engine.StatusFailed

	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusReady, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusRunning, nil, nil, nil))
	state, _ = state.Apply(engine.NewStateMutation("A", state.Version(), &statusFailed, nil, nil, nil))

	assessment, err := detector.AssessState(ctx, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Since neither B nor C can progress because their upstream A failed (default joinmode is AllOf),
	// the graph should fail immediately despite PolicyContinue.
	if assessment.Phase != reactive.PhaseFailed {
		t.Errorf("expected PhaseFailed because no pending nodes can progress, got %s", assessment.Phase)
	}
}
