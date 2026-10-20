package orchestrator

import (
	"errors"
	"fmt"

	"github.com/gargi-indic/gargi-flow/internal/metrics"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"go.temporal.io/sdk/workflow"
)

// CompletedNodeRecord tracks a completed node and its declared compensation handler reference.
type CompletedNodeRecord struct {
	NodeID       engine.NodeID
	Compensation string // Handler reference name in GraphDefinition.Compensations
}

// SagaCoordinator tracks completed nodes in completion order and executes LIFO compensation rollbacks.
type SagaCoordinator struct {
	history []CompletedNodeRecord
}

// NewSagaCoordinator creates a new SagaCoordinator.
func NewSagaCoordinator() *SagaCoordinator {
	return &SagaCoordinator{
		history: make([]CompletedNodeRecord, 0),
	}
}

// RecordCompletion records a successfully completed node for possible Saga compensation.
func (s *SagaCoordinator) RecordCompletion(nodeID engine.NodeID, compensationRef string) {
	s.history = append(s.history, CompletedNodeRecord{
		NodeID:       nodeID,
		Compensation: compensationRef,
	})
}

// History returns a copy of the recorded completion history.
func (s *SagaCoordinator) History() []CompletedNodeRecord {
	cp := make([]CompletedNodeRecord, len(s.history))
	copy(cp, s.history)
	return cp
}

// ExecuteCompensation runs compensation handlers in reverse completion order (LIFO).
func (s *SagaCoordinator) ExecuteCompensation(
	ctx workflow.Context,
	def *engine.GraphDefinition,
	state *engine.GraphState,
) (*engine.GraphState, error) {
	dCtx, _ := workflow.NewDisconnectedContext(ctx)

	var compErrors []error

	for i := len(s.history) - 1; i >= 0; i-- {
		rec := s.history[i]
		if rec.Compensation == "" {
			continue
		}

		statusCompensating := engine.StatusCompensating
		var err error
		state, err = state.Apply(engine.StateMutation{
			NodeID:          rec.NodeID,
			ExpectedVersion: state.Version(),
			Status:          &statusCompensating,
		})
		if err != nil {
			compErrors = append(compErrors, fmt.Errorf("failed to transition node %q to compensating: %w", rec.NodeID, err))
			continue
		}

		compDef, ok := def.Compensations[rec.Compensation]
		if !ok {
			errMsg := fmt.Sprintf("compensation handler %q not found", rec.Compensation)
			statusCompensated := engine.StatusCompensated
			state, _ = state.Apply(engine.StateMutation{
				NodeID:          rec.NodeID,
				ExpectedVersion: state.Version(),
				Status:          &statusCompensated,
				ErrorMessage:    &errMsg,
			})
			compErrors = append(compErrors, errors.New(errMsg))
			continue
		}

		actOpts := BuildNodeActivityOptions(workflow.ActivityOptions{}, nil)
		actOpts.RetryPolicy = ResolveNodeRetryPolicy(nil, def.RetryPolicies)
		actCtx := workflow.WithActivityOptions(dCtx, actOpts)

		actInput := NodeActivityInput{
			ExecutionID: state.ExecutionID(),
			NodeID:      rec.NodeID,
			NodeType:    compDef.Type,
			Config:      compDef.Config,
			Inputs:      state.NodeState(rec.NodeID).Output(),
			GraphState:  state,
			Attempt:     1,
			MaxAttempts: int(actOpts.RetryPolicy.MaximumAttempts),
		}

		var res NodeActivityResult
		actErr := workflow.ExecuteActivity(actCtx, ExecuteNodeActivityName, actInput).Get(dCtx, &res)

		statusCompensated := engine.StatusCompensated
		if actErr != nil {
			metrics.RecordCompensation("failure")
			errMsg := actErr.Error()
			state, _ = state.Apply(engine.StateMutation{
				NodeID:          rec.NodeID,
				ExpectedVersion: state.Version(),
				Status:          &statusCompensated,
				ErrorMessage:    &errMsg,
			})
			compErrors = append(compErrors, fmt.Errorf("compensation %q for node %q failed: %w", rec.Compensation, rec.NodeID, actErr))
		} else {
			metrics.RecordCompensation("success")
			mut := engine.StateMutation{
				NodeID:          rec.NodeID,
				ExpectedVersion: state.Version(),
				Status:          &statusCompensated,
			}
			if res.Mutation != nil {
				mut.Set = res.Mutation.Set
				mut.Unset = res.Mutation.Unset
				mut.ErrorMessage = res.Mutation.ErrorMessage
			}
			state, _ = state.Apply(mut)
		}
	}

	if state.Status() == engine.StatusCompensating {
		state, _ = state.WithStatus(state.Version(), engine.StatusCompensated)
	}

	if len(compErrors) > 0 {
		return state, errors.Join(compErrors...)
	}
	return state, nil
}
