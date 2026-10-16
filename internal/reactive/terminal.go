package reactive

import (
	"context"
	"fmt"
	"sort"

	"github.com/gargi-indic/gargi-ckg/internal/graph"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

// ExecutionPhase represents the current lifecycle phase of a graph execution.
type ExecutionPhase string

const (
	// PhaseRunning indicates the graph execution is still actively progressing.
	PhaseRunning ExecutionPhase = "running"
	// PhaseCompleted indicates all reachable leaves have terminated successfully.
	PhaseCompleted ExecutionPhase = "completed"
	// PhaseFailed indicates the graph execution has terminated in a failed state.
	PhaseFailed ExecutionPhase = "failed"
	// PhaseCompensating indicates a critical failure triggered Saga compensation.
	PhaseCompensating ExecutionPhase = "compensating"
)

// Failure policy constants matching schemas/graph/v1/schema.json.
const (
	PolicyFailGraph  = "fail_graph"
	PolicyContinue   = "continue"
	PolicyCompensate = "compensate"
)

// TerminalAssessment captures the outcome of evaluating graph termination and failure policy.
type TerminalAssessment struct {
	Phase           ExecutionPhase
	FinalGraphState *engine.GraphState
	TerminalEvents  []*engine.Event
	FailureCause    error
}

// TerminalDetector evaluates a GraphState against its Graph topology and FailurePolicy.
type TerminalDetector struct {
	graph         *engine.Graph
	failurePolicy *engine.FailurePolicy
	emitter       *TransitionEmitter
	nodeErrors    map[engine.NodeID]error
}

// NewTerminalDetector creates a new TerminalDetector.
// If failurePolicy is nil, defaults are used (OnNodeFailure: "fail_graph", OnCriticalFailure: "compensate").
func NewTerminalDetector(
	g *engine.Graph,
	failurePolicy *engine.FailurePolicy,
	emitter *TransitionEmitter,
) *TerminalDetector {
	return &TerminalDetector{
		graph:         g,
		failurePolicy: failurePolicy,
		emitter:       emitter,
		nodeErrors:    make(map[engine.NodeID]error),
	}
}

// RecordNodeFailure associates an error cause (carrying engine.ErrorClass) with a failed node ID.
func (d *TerminalDetector) RecordNodeFailure(nodeID engine.NodeID, cause error) {
	if cause != nil {
		d.nodeErrors[nodeID] = cause
	}
}

func transitionGraphTo(state *engine.GraphState, target engine.Status) (*engine.GraphState, error) {
	if state.Status() == target {
		return state, nil
	}

	var sequence []engine.Status

	// Helper to determine path
	switch target {
	case engine.StatusFailed:
		switch state.Status() {
		case engine.StatusPending:
			sequence = []engine.Status{engine.StatusReady, engine.StatusRunning, engine.StatusFailed}
		case engine.StatusReady:
			sequence = []engine.Status{engine.StatusRunning, engine.StatusFailed}
		case engine.StatusRunning:
			sequence = []engine.Status{engine.StatusFailed}
		}
	case engine.StatusSucceeded:
		switch state.Status() {
		case engine.StatusPending:
			sequence = []engine.Status{engine.StatusReady, engine.StatusRunning, engine.StatusSucceeded}
		case engine.StatusReady:
			sequence = []engine.Status{engine.StatusRunning, engine.StatusSucceeded}
		case engine.StatusRunning:
			sequence = []engine.Status{engine.StatusSucceeded}
		}
	case engine.StatusCompensating:
		// Succeeded/Failed -> Compensating
		switch state.Status() {
		case engine.StatusPending:
			sequence = []engine.Status{engine.StatusReady, engine.StatusRunning, engine.StatusFailed, engine.StatusCompensating}
		case engine.StatusReady:
			sequence = []engine.Status{engine.StatusRunning, engine.StatusFailed, engine.StatusCompensating}
		case engine.StatusRunning:
			sequence = []engine.Status{engine.StatusFailed, engine.StatusCompensating}
		case engine.StatusSucceeded, engine.StatusFailed:
			sequence = []engine.Status{engine.StatusCompensating}
		}
	case engine.StatusSkipped:
		switch state.Status() {
		case engine.StatusPending:
			sequence = []engine.Status{engine.StatusSkipped}
		case engine.StatusReady:
			sequence = []engine.Status{engine.StatusSkipped}
		}
	case engine.StatusCompensated:
		// Compensating -> Compensated
		switch state.Status() {
		case engine.StatusPending:
			sequence = []engine.Status{engine.StatusReady, engine.StatusRunning, engine.StatusFailed, engine.StatusCompensating, engine.StatusCompensated}
		case engine.StatusReady:
			sequence = []engine.Status{engine.StatusRunning, engine.StatusFailed, engine.StatusCompensating, engine.StatusCompensated}
		case engine.StatusRunning:
			sequence = []engine.Status{engine.StatusFailed, engine.StatusCompensating, engine.StatusCompensated}
		case engine.StatusSucceeded, engine.StatusFailed:
			sequence = []engine.Status{engine.StatusCompensating, engine.StatusCompensated}
		case engine.StatusCompensating:
			sequence = []engine.Status{engine.StatusCompensated}
		}
	}

	if sequence == nil {
		return nil, fmt.Errorf("no valid transition sequence from %s to %s", state.Status(), target)
	}

	curr := state
	var err error
	for _, nextStatus := range sequence {
		curr, err = curr.WithStatus(curr.Version(), nextStatus)
		if err != nil {
			return nil, err
		}
	}
	return curr, nil
}

// AssessState determines if the graph execution has reached a terminal or compensating state.
// Optional causes allow callers to supply recent node failure errors directly (checked via engine.IsCritical).
func (d *TerminalDetector) AssessState(
	ctx context.Context,
	state *engine.GraphState,
	causes ...error,
) (*TerminalAssessment, error) {
	if d == nil {
		return nil, fmt.Errorf("TerminalDetector is nil")
	}
	if d.graph == nil {
		return nil, fmt.Errorf("graph is nil")
	}
	if state == nil {
		return nil, fmt.Errorf("state is nil")
	}

	onNodeFailure := PolicyFailGraph
	onCriticalFailure := PolicyCompensate

	if d.failurePolicy != nil {
		if d.failurePolicy.OnNodeFailure != "" {
			onNodeFailure = d.failurePolicy.OnNodeFailure
		}
		if d.failurePolicy.OnCriticalFailure != "" {
			onCriticalFailure = d.failurePolicy.OnCriticalFailure
		}
	}

	if onNodeFailure != PolicyFailGraph && onNodeFailure != PolicyContinue {
		return nil, fmt.Errorf("invalid onNodeFailure policy: %s", onNodeFailure)
	}
	if onCriticalFailure != PolicyCompensate && onCriticalFailure != PolicyFailGraph {
		return nil, fmt.Errorf("invalid onCriticalFailure policy: %s", onCriticalFailure)
	}

	nodes := d.graph.Nodes()
	nodeIDs := make([]engine.NodeID, 0, len(nodes))
	for _, n := range nodes {
		nodeIDs = append(nodeIDs, n.ID())
	}

	// Lexicographical sort to ensure deterministic evaluation
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })

	var failedNodes []engine.NodeID
	hasReadyOrRunning := false
	var pendingNodes []engine.NodeID

	for _, id := range nodeIDs {
		ns := state.NodeState(id)
		if ns == nil {
			continue
		}
		status := ns.Status()
		switch status {
		case engine.StatusFailed:
			failedNodes = append(failedNodes, id)
		case engine.StatusReady, engine.StatusRunning:
			hasReadyOrRunning = true
		case engine.StatusPending:
			pendingNodes = append(pendingNodes, id)
		}
	}

	var isCritical bool
	var failureCause error
	var firstFailedNodeID engine.NodeID
	var firstFailedNodeErrMsg string

	for _, id := range failedNodes {
		ns := state.NodeState(id)
		if ns != nil && firstFailedNodeID == "" {
			firstFailedNodeID = id
			firstFailedNodeErrMsg = ns.ErrorMessage()
		}
	}

	for _, err := range causes {
		if err != nil {
			if failureCause == nil {
				failureCause = err
			}
			if engine.IsCritical(err) {
				isCritical = true
				break
			}
		}
	}

	if !isCritical {
		for _, id := range failedNodes {
			err, exists := d.nodeErrors[id]
			if exists && err != nil {
				if failureCause == nil {
					failureCause = err
				}
				if engine.IsCritical(err) {
					isCritical = true
					break
				}
			}
		}
	}

	if len(failedNodes) > 0 && failureCause == nil {
		failureCause = fmt.Errorf("node %s failed: %s", firstFailedNodeID, firstFailedNodeErrMsg)
	}

	if len(failedNodes) > 0 {
		if isCritical {
			if onCriticalFailure == PolicyCompensate {
				nextState, err := transitionGraphTo(state, engine.StatusCompensating)
				if err != nil {
					return nil, err
				}
				return &TerminalAssessment{Phase: PhaseCompensating, FinalGraphState: nextState, FailureCause: failureCause}, nil
			}

			nextState, err := transitionGraphTo(state, engine.StatusFailed)
			if err != nil {
				return nil, err
			}
			var events []*engine.Event
			if d.emitter != nil {
				ev, err := engine.NewEvent(d.emitter.execID, d.emitter.seqGen.Next(), d.emitter.clock(), engine.EventTypeGraphFailed, "", engine.GraphFailedPayload{ErrorMessage: failureCause.Error(), Version: nextState.Version()})
				if err != nil {
					return nil, err
				}
				if d.emitter.sink != nil {
					if err := d.emitter.sink.Emit(ctx, ev); err != nil {
						return nil, err
					}
				}
				events = append(events, ev)
			}
			return &TerminalAssessment{Phase: PhaseFailed, FinalGraphState: nextState, TerminalEvents: events, FailureCause: failureCause}, nil
		}

		if onNodeFailure == PolicyFailGraph {
			nextState, err := transitionGraphTo(state, engine.StatusFailed)
			if err != nil {
				return nil, err
			}
			var events []*engine.Event
			if d.emitter != nil {
				ev, err := engine.NewEvent(d.emitter.execID, d.emitter.seqGen.Next(), d.emitter.clock(), engine.EventTypeGraphFailed, "", engine.GraphFailedPayload{ErrorMessage: failureCause.Error(), Version: nextState.Version()})
				if err != nil {
					return nil, err
				}
				if d.emitter.sink != nil {
					_ = d.emitter.sink.Emit(ctx, ev)
				}
				events = append(events, ev)
			}
			return &TerminalAssessment{Phase: PhaseFailed, FinalGraphState: nextState, TerminalEvents: events, FailureCause: failureCause}, nil
		}

		if onNodeFailure == PolicyContinue {
			if hasReadyOrRunning {
				return &TerminalAssessment{Phase: PhaseRunning, FinalGraphState: state}, nil
			}
			canProgress := false
			for _, pid := range pendingNodes {
				if canPendingNodeProgress(pid, d.graph, state) {
					canProgress = true
					break
				}
			}
			if canProgress {
				return &TerminalAssessment{Phase: PhaseRunning, FinalGraphState: state}, nil
			}

			nextState, err := transitionGraphTo(state, engine.StatusFailed)
			if err != nil {
				return nil, err
			}
			var events []*engine.Event
			if d.emitter != nil {
				ev, err := engine.NewEvent(d.emitter.execID, d.emitter.seqGen.Next(), d.emitter.clock(), engine.EventTypeGraphFailed, "", engine.GraphFailedPayload{ErrorMessage: failureCause.Error(), Version: nextState.Version()})
				if err != nil {
					return nil, err
				}
				if d.emitter.sink != nil {
					_ = d.emitter.sink.Emit(ctx, ev)
				}
				events = append(events, ev)
			}
			return &TerminalAssessment{Phase: PhaseFailed, FinalGraphState: nextState, TerminalEvents: events, FailureCause: failureCause}, nil
		}
	}

	leaves := graph.Leaves(d.graph)
	allLeavesTerminal := true
	for _, l := range leaves {
		ns := state.NodeState(l)
		if ns == nil || (ns.Status() != engine.StatusSucceeded && ns.Status() != engine.StatusSkipped) {
			allLeavesTerminal = false
			break
		}
	}

	if allLeavesTerminal && !hasReadyOrRunning {
		nextState, err := transitionGraphTo(state, engine.StatusSucceeded)
		if err != nil {
			return nil, err
		}
		var events []*engine.Event
		if d.emitter != nil {
			ev, err := engine.NewEvent(d.emitter.execID, d.emitter.seqGen.Next(), d.emitter.clock(), engine.EventTypeGraphCompleted, "", engine.GraphCompletedPayload{FinalStatus: engine.StatusSucceeded, Version: nextState.Version()})
			if err != nil {
				return nil, err
			}
			if d.emitter.sink != nil {
				if err := d.emitter.sink.Emit(ctx, ev); err != nil {
					return nil, err
				}
			}
			events = append(events, ev)
		}
		return &TerminalAssessment{Phase: PhaseCompleted, FinalGraphState: nextState, TerminalEvents: events}, nil
	}

	return &TerminalAssessment{Phase: PhaseRunning, FinalGraphState: state}, nil
}

// canPendingNodeProgress statically analyzes if a pending node could still execute
// given the current upstream states, following the exact JoinMode semantics.
func canPendingNodeProgress(nodeID engine.NodeID, g *engine.Graph, state *engine.GraphState) bool {
	node := g.Node(nodeID)
	if node == nil {
		return false
	}
	incoming := g.IncomingEdges(nodeID)
	if len(incoming) == 0 {
		return true // Roots can always progress if pending
	}

	joinMode := node.JoinMode()

	if joinMode == engine.JoinModeAllOf {
		// AllOf requires NO upstream to be Failed, Skipped, Compensating, or Compensated.
		for _, e := range incoming {
			ns := state.NodeState(e.From())
			if ns == nil {
				return false
			}
			st := ns.Status()
			if st == engine.StatusFailed || st == engine.StatusSkipped || st == engine.StatusCompensating || st == engine.StatusCompensated {
				return false
			}
			if st == engine.StatusPending && !canPendingNodeProgress(e.From(), g, state) {
				return false
			}
		}
		return true
	}

	if joinMode == engine.JoinModeAnyOf {
		// AnyOf requires at least ONE upstream to be Succeeded OR able to progress.
		for _, e := range incoming {
			ns := state.NodeState(e.From())
			if ns == nil {
				continue
			}
			st := ns.Status()
			if st == engine.StatusSucceeded {
				return true
			}
			if st == engine.StatusPending || st == engine.StatusReady || st == engine.StatusRunning {
				// Prevent infinite recursion on cycles if any, though DAGs shouldn't have them
				if st == engine.StatusPending {
					if canPendingNodeProgress(e.From(), g, state) {
						return true
					}
				} else {
					return true
				}
			}
		}
		return false
	}

	return false
}
