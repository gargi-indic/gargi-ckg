package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/nodes"
	"github.com/gargi-indic/gargi-ckg/internal/reactive"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// GraphExecutionWorkflowName is the name of the workflow.
	GraphExecutionWorkflowName = "GraphExecutionWorkflow"
	// ExecuteNodeActivityName is the name of the activity.
	ExecuteNodeActivityName = "ExecuteNodeActivity"
	// MaxRecommendedPayloadBytes is the payload size warning threshold.
	MaxRecommendedPayloadBytes = 2 * 1024 * 1024 // 2 MB warning threshold
)

// GraphWorkflowInput is the input payload for GraphExecutionWorkflow.
type GraphWorkflowInput struct {
	ExecutionID  engine.ExecutionID      `json:"executionId"`
	Definition   *engine.GraphDefinition `json:"definition"`
	InitialState map[string]any          `json:"initialState,omitempty"`
}

// GraphWorkflowResult is the output returned when GraphExecutionWorkflow finishes.
type GraphWorkflowResult struct {
	ExecutionID engine.ExecutionID `json:"executionId"`
	FinalState  *engine.GraphState `json:"finalState"`
	Status      engine.Status      `json:"status"`
	Error       string             `json:"error,omitempty"`
}

// WorkflowRunner binds an optional custom engine.Registry to GraphExecutionWorkflow.
type WorkflowRunner struct {
	registry *engine.Registry
}

// NewWorkflowRunner creates a WorkflowRunner with the given registry (or a default registry if nil).
func NewWorkflowRunner(registry *engine.Registry) *WorkflowRunner {
	return &WorkflowRunner{registry: registry}
}

// GraphExecutionWorkflow is the top-level Temporal workflow function driving the reactive scheduler.
func GraphExecutionWorkflow(
	ctx workflow.Context,
	input GraphWorkflowInput,
) (*GraphWorkflowResult, error) {
	return NewWorkflowRunner(nil).GraphExecutionWorkflow(ctx, input)
}

func checkPayloadSize(ctx workflow.Context, tag string, v any) {
	b, err := json.Marshal(v)
	if err == nil && len(b) > MaxRecommendedPayloadBytes {
		workflow.GetLogger(ctx).Warn("payload size exceeds recommended threshold",
			"tag", tag,
			"size_bytes", len(b),
			"threshold_bytes", MaxRecommendedPayloadBytes,
		)
	}
}

type activityOutcome struct {
	nodeID engine.NodeID
	result *NodeActivityResult
	err    error
}

// GraphExecutionWorkflow executes a reactive graph workflow using r's registry.
func (r *WorkflowRunner) GraphExecutionWorkflow(
	ctx workflow.Context,
	input GraphWorkflowInput,
) (*GraphWorkflowResult, error) {
	if input.Definition == nil {
		return nil, temporal.NewNonRetryableApplicationError("GraphDefinition cannot be nil", "InvalidInput", nil)
	}

	checkPayloadSize(ctx, "input_definition", input.Definition)

	execID := input.ExecutionID
	if execID == "" {
		execID = engine.ExecutionID(workflow.GetInfo(ctx).WorkflowExecution.ID)
	}

	g, err := input.Definition.ToGraph()
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(fmt.Sprintf("failed to parse graph definition: %v", err), "InvalidDefinition", err)
	}

	state, err := engine.NewGraphState(execID, g)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(fmt.Sprintf("failed to create graph state: %v", err), "InternalError", err)
	}

	state, err = state.WithStatus(state.Version(), engine.StatusReady)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError("failed to transition graph state to Ready", "InternalError", err)
	}
	state, err = state.WithStatus(state.Version(), engine.StatusRunning)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError("failed to transition graph state to Running", "InternalError", err)
	}

	reg := r.registry
	if reg == nil {
		reg = engine.NewRegistry()
		_ = nodes.RegisterDefaults(reg, nodes.Dependencies{})
	}

	// Register fallback for unknown node types so we don't fail during schedule (EvaluateReactivity).
	for _, nodeDef := range input.Definition.Nodes {
		if !reg.Has(nodeDef.Type) {
			_ = reg.Register(nodeDef.Type, func(_ any) (engine.NodeInterface, error) {
				return &engine.BaseNode{}, nil
			}, nil)
		}
	}

	nodeDefs := make(map[engine.NodeID]engine.NodeDefinition, len(input.Definition.Nodes))
	for _, n := range input.Definition.Nodes {
		nodeDefs[n.ID] = n
	}

	clock := func() time.Time { return workflow.Now(ctx) }
	seqGen := reactive.NewSequenceGenerator(0)
	emitter := reactive.NewTransitionEmitter(execID, seqGen, clock, nil)
	resolver := reactive.NewPreconditionResolver(nil)
	scheduler := reactive.NewScheduler(g, reg, resolver, emitter)
	detector := reactive.NewTerminalDetector(g, input.Definition.FailurePolicy, emitter)

	stdCtx := context.Background()

	selector := workflow.NewSelector(ctx)
	inFlightCount := 0
	var completedOutcomes []activityOutcome

	for {
		plan, nextState, err := scheduler.EvaluateStep(stdCtx, state)
		if err != nil {
			return &GraphWorkflowResult{ExecutionID: execID, FinalState: state, Status: engine.StatusFailed, Error: err.Error()}, temporal.NewNonRetryableApplicationError(fmt.Sprintf("scheduler evaluation failed: %v", err), "InternalError", err)
		}
		state = nextState

		for _, nodeID := range plan.ReadyNodes {

			// Scheduler already transitioned to Ready, we skip this transition because it is done in EvaluateStep.
			// Wait, the planner in scheduler transitions to StatusReady, but we should transition to StatusRunning when starting activity.
			oldNS := state.NodeState(nodeID)
			statusRunning := engine.StatusRunning
			mutRunning := engine.StateMutation{NodeID: nodeID, ExpectedVersion: state.Version(), Status: &statusRunning}

			state, err = state.Apply(mutRunning)
			if err != nil {
				return nil, err
			}
			_, _ = emitter.EmitTransition(stdCtx, oldNS, state.NodeState(nodeID), mutRunning)

			inputs := plan.ResolvedInputs[nodeID]
			if inputs == nil {
				inputs = make(map[string]any)
			}
			if len(g.IncomingEdges(nodeID)) == 0 && len(input.InitialState) > 0 {
				for k, v := range input.InitialState {
					if _, exists := inputs[k]; !exists {
						inputs[k] = v
					}
				}
			}

			nodeDef := nodeDefs[nodeID]
			actOpts := BuildNodeActivityOptions(workflow.ActivityOptions{}, &nodeDef)
			actOpts.RetryPolicy = ResolveNodeRetryPolicy(&nodeDef, input.Definition.RetryPolicies)
			actCtx := workflow.WithActivityOptions(ctx, actOpts)

			actInput := NodeActivityInput{
				ExecutionID: execID,
				NodeID:      nodeID,
				NodeType:    nodeDef.Type,
				Config:      nodeDef.Config,
				Inputs:      inputs,
				GraphState:  state,
				Attempt:     1,
				MaxAttempts: int(actOpts.RetryPolicy.MaximumAttempts),
			}

			inFlightCount++
			fut := workflow.ExecuteActivity(actCtx, ExecuteNodeActivityName, actInput)

			nID := nodeID
			selector.AddFuture(fut, func(f workflow.Future) {
				inFlightCount--
				var res NodeActivityResult
				err := f.Get(ctx, &res)
				completedOutcomes = append(completedOutcomes, activityOutcome{nodeID: nID, result: &res, err: err})
			})
		}

		if inFlightCount == 0 && len(completedOutcomes) == 0 {
			assessment, err := detector.AssessState(stdCtx, state)
			if err != nil {
				return nil, err
			}
			if assessment.Phase != reactive.PhaseRunning {
				state = assessment.FinalGraphState
				break
			}
			if len(plan.ReadyNodes) == 0 && len(plan.SkippedNodes) == 0 {
				break
			}
		} else if inFlightCount > 0 && len(completedOutcomes) == 0 {
			selector.Select(ctx)
		}

		if len(completedOutcomes) > 0 {
			for _, outcome := range completedOutcomes {
				oldNS := state.NodeState(outcome.nodeID)
				if outcome.err == nil {
					statusSucceeded := engine.StatusSucceeded
					mut := engine.StateMutation{
						NodeID:          outcome.nodeID,
						ExpectedVersion: state.Version(),
						Status:          &statusSucceeded,
					}
					if outcome.result != nil && outcome.result.Mutation != nil {
						mut.Set = outcome.result.Mutation.Set
						mut.Unset = outcome.result.Mutation.Unset
						mut.ErrorMessage = outcome.result.Mutation.ErrorMessage
					}
					state, err = state.Apply(mut)
					if err != nil {
						return nil, err
					}
					_, _ = emitter.EmitTransition(stdCtx, oldNS, state.NodeState(outcome.nodeID), mut)
				} else {
					var cause error
					if IsCriticalError(outcome.err) {
						cause = engine.NewCriticalError(outcome.err)
					} else {
						cause = engine.NewNonRetryableError(outcome.err)
					}
					detector.RecordNodeFailure(outcome.nodeID, cause)

					statusFailed := engine.StatusFailed
					errMsg := cause.Error()
					mut := engine.StateMutation{
						NodeID:          outcome.nodeID,
						ExpectedVersion: state.Version(),
						Status:          &statusFailed,
						ErrorMessage:    &errMsg,
					}
					state, err = state.Apply(mut)
					if err != nil {
						return nil, err
					}
					_, _ = emitter.EmitTransition(stdCtx, oldNS, state.NodeState(outcome.nodeID), mut, cause)
				}
			}
			completedOutcomes = nil

			assessment, err := detector.AssessState(stdCtx, state)
			if err != nil {
				return nil, err
			}
			if assessment.Phase != reactive.PhaseRunning {
				state = assessment.FinalGraphState
				break
			}
		}
	}

	checkPayloadSize(ctx, "final_state", state)

	var errStr string
	if state.Status() == engine.StatusFailed {
		errStr = "graph execution failed"
	}

	return &GraphWorkflowResult{
		ExecutionID: execID,
		FinalState:  state,
		Status:      state.Status(),
		Error:       errStr,
	}, nil
}
