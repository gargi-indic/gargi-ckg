package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/gargi-indic/gargi-ckg/internal/logging"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

const (
	// PersistStateActivityName is the registered activity name for persisting state.
	PersistStateActivityName = "PersistStateActivity"
	// WriteStateActivityName is an alias activity name for persisting state.
	WriteStateActivityName = "WriteStateActivity"
)

// PersistStateInput is the serializable input payload for PersistStateActivity and WriteStateActivity.
type PersistStateInput struct {
	// ExecutionID is the unique identifier of the graph execution.
	ExecutionID engine.ExecutionID `json:"executionId"`
	// NodeID is the optional node identifier associated with this state transition.
	NodeID engine.NodeID `json:"nodeId,omitempty"`
	// Version is the expected monotonic version in StateStore prior to applying this state or mutation.
	Version uint64 `json:"version"`
	// State is the full GraphState snapshot to persist (used with SetState).
	State *engine.GraphState `json:"state,omitempty"`
	// Mutation is an optional incremental mutation to apply (used with PatchState).
	Mutation *engine.StateMutation `json:"mutation,omitempty"`
	// IdempotencyKey is an optional explicit idempotency key for this write operation.
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

// EffectiveIdempotencyKey returns the explicit IdempotencyKey if set, or computes the canonical key (executionID:nodeID:version).
func (in *PersistStateInput) EffectiveIdempotencyKey() string {
	if in.IdempotencyKey != "" {
		return in.IdempotencyKey
	}
	if in.NodeID != "" {
		return fmt.Sprintf("%s:%s:%d", in.ExecutionID, in.NodeID, in.Version)
	}
	return fmt.Sprintf("%s:%d", in.ExecutionID, in.Version)
}

// PersistStateResult is the output payload returned upon successful state persistence.
type PersistStateResult struct {
	// ExecutionID is the execution identifier that was persisted.
	ExecutionID engine.ExecutionID `json:"executionId"`
	// Version is the final monotonic version in the store after persistence.
	Version uint64 `json:"version"`
	// IdempotencyKey is the effective idempotency key used for tracking.
	IdempotencyKey string `json:"idempotencyKey"`
	// IdempotentNoop is true if the store was nil and the activity succeeded as a no-op.
	IdempotentNoop bool `json:"idempotentNoop,omitempty"`
	// IdempotentRecovery is true if a duplicate re-execution was detected and resolved cleanly.
	IdempotentRecovery bool `json:"idempotentRecovery,omitempty"`
}

// StateActivities provides Temporal activities for persisting execution state to engine.StateStore.
type StateActivities struct {
	store engine.StateStore
}

// NewStateActivities creates a new StateActivities instance with the provided engine.StateStore.
// If store is nil, activities succeed as a graceful no-op.
func NewStateActivities(store engine.StateStore) *StateActivities {
	return &StateActivities{
		store: store,
	}
}

// Store returns the underlying engine.StateStore (which may be nil).
func (a *StateActivities) Store() engine.StateStore {
	if a == nil {
		return nil
	}
	return a.store
}

// PersistStateActivity executes an idempotent write to engine.StateStore enforcing optimistic concurrency (CAS).
func (a *StateActivities) PersistStateActivity(
	ctx context.Context,
	input PersistStateInput,
) (*PersistStateResult, error) {
	wfID := ""
	runID := ""
	if activity.IsActivity(ctx) {
		actInfo := activity.GetInfo(ctx)
		wfID = actInfo.WorkflowExecution.ID
		runID = actInfo.WorkflowExecution.RunID
	}
	ctx = logging.WithCorrelation(ctx, string(input.ExecutionID), string(input.NodeID), wfID, runID)

	// Graceful degradation: If StateStore is nil (e.g., in-memory or Temporal-only mode without external store), succeed as a no-op.
	if a == nil || a.store == nil {
		version := input.Version
		if input.State != nil {
			version = input.State.Version()
		}
		return &PersistStateResult{
			ExecutionID:    input.ExecutionID,
			Version:        version,
			IdempotencyKey: input.EffectiveIdempotencyKey(),
			IdempotentNoop: true,
		}, nil
	}

	if input.ExecutionID == "" {
		return nil, temporal.NewNonRetryableApplicationError("execution ID cannot be empty", ErrorTypeNonRetryable, nil)
	}
	if input.State == nil && input.Mutation == nil {
		return nil, temporal.NewNonRetryableApplicationError("invalid PersistStateInput: either State or Mutation must be provided", ErrorTypeNonRetryable, nil)
	}

	var writeErr error
	var patchedState *engine.GraphState

	// Attempt write: PatchState if mutation is provided without full state, or SetState if full state is provided.
	if input.State != nil {
		writeErr = a.store.SetState(ctx, input.ExecutionID, input.State, input.Version)
		if writeErr == nil {
			return &PersistStateResult{
				ExecutionID:    input.ExecutionID,
				Version:        input.State.Version(),
				IdempotencyKey: input.EffectiveIdempotencyKey(),
			}, nil
		}
	} else {
		patchedState, writeErr = a.store.PatchState(ctx, input.ExecutionID, input.Mutation, input.Version)
		if writeErr == nil {
			return &PersistStateResult{
				ExecutionID:    input.ExecutionID,
				Version:        patchedState.Version(),
				IdempotencyKey: input.EffectiveIdempotencyKey(),
			}, nil
		}
	}

	// Handle transient storage unavailable or connection timeouts:
	// Return a retryable Temporal ApplicationError (gargi.retryable) so Temporal retries with backoff.
	if isRetryableStoreError(ctx, writeErr) {
		return nil, temporal.NewApplicationErrorWithCause(
			fmt.Sprintf("storage backend unavailable for execution %s: %v", input.ExecutionID, writeErr),
			ErrorTypeRetryable,
			writeErr,
		)
	}

	// Handle optimistic concurrency conflicts (StoreConflictError):
	// Enforce idempotent CAS semantics.
	if engine.IsStoreConflict(writeErr) {
		storedState, getErr := a.store.GetState(ctx, input.ExecutionID)
		if getErr != nil {
			if isRetryableStoreError(ctx, getErr) {
				return nil, temporal.NewApplicationErrorWithCause(
					fmt.Sprintf("verifying conflict state unavailable for execution %s: %v", input.ExecutionID, getErr),
					ErrorTypeRetryable,
					getErr,
				)
			}
			return nil, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("unrecoverable conflict verifying state for execution %s: %v", input.ExecutionID, getErr),
				ErrorTypeNonRetryable,
				writeErr,
			)
		}

		// Calculate target version with the state/mutation already applied.
		targetVersion := input.Version
		if input.State != nil {
			if input.State.Version() > targetVersion {
				targetVersion = input.State.Version()
			}
		} else if input.Mutation != nil {
			targetVersion = input.Version + 1
		}

		// Check if stored version >= target version with state already applied.
		if storedState.Version() >= targetVersion {
			alreadyApplied := true

			if input.NodeID != "" {
				storedNode := storedState.NodeState(input.NodeID)
				if storedNode == nil {
					alreadyApplied = false
				} else if input.State != nil {
					targetNode := input.State.NodeState(input.NodeID)
					if targetNode != nil && storedNode.Version() < targetNode.Version() {
						alreadyApplied = false
					}
				} else if input.Mutation != nil {
					if storedNode.Version() <= input.Version {
						alreadyApplied = false
					}
				}
			}

			if alreadyApplied {
				// Duplicate re-execution detected; succeed cleanly (idempotent recovery).
				return &PersistStateResult{
					ExecutionID:        input.ExecutionID,
					Version:            storedState.Version(),
					IdempotencyKey:     input.EffectiveIdempotencyKey(),
					IdempotentRecovery: true,
				}, nil
			}
		}

		// Stored version < targetVersion or state transition not applied: true unrecoverable conflict.
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("unrecoverable state conflict for execution %s (expected version %d, stored version %d): %v",
				input.ExecutionID, input.Version, storedState.Version(), writeErr),
			ErrorTypeNonRetryable,
			writeErr,
		)
	}

	// Any other unhandled failure is treated as non-retryable.
	return nil, temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("persisting state failed for execution %s: %v", input.ExecutionID, writeErr),
		ErrorTypeNonRetryable,
		writeErr,
	)
}

// WriteStateActivity is an alias for PersistStateActivity.
func (a *StateActivities) WriteStateActivity(
	ctx context.Context,
	input PersistStateInput,
) (*PersistStateResult, error) {
	return a.PersistStateActivity(ctx, input)
}

func isRetryableStoreError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if engine.IsStoreUnavailable(err) || errors.Is(err, engine.ErrStoreUnavailable) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	if engine.ClassOf(err) == engine.ClassRetryable {
		return true
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.Type() == ErrorTypeRetryable {
		return true
	}
	return false
}
