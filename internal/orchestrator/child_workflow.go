package orchestrator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/workflow"
)

// SubGraphNodeType is the node type identifier for composite sub-graph execution.
const SubGraphNodeType = "sub_graph"

// SubGraphConfig holds the configuration for a sub_graph node.
type SubGraphConfig struct {
	Definition        *engine.GraphDefinition `json:"definition,omitempty"`
	ParentClosePolicy string                  `json:"parentClosePolicy,omitempty"` // TERMINATE, REQUEST_CANCEL, ABANDON
}

// ResolveParentClosePolicy resolves a string policy into a Temporal enums.ParentClosePolicy.
func ResolveParentClosePolicy(policy string) (enums.ParentClosePolicy, error) {
	switch policy {
	case "", "TERMINATE":
		return enums.PARENT_CLOSE_POLICY_TERMINATE, nil
	case "REQUEST_CANCEL":
		return enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL, nil
	case "ABANDON":
		return enums.PARENT_CLOSE_POLICY_ABANDON, nil
	default:
		return 0, fmt.Errorf("invalid parent close policy: %q", policy)
	}
}

// DecodeSubGraphConfig decodes a map into a SubGraphConfig, disallowing unknown fields.
func DecodeSubGraphConfig(raw map[string]any) (SubGraphConfig, error) {
	if raw == nil {
		raw = make(map[string]any)
	}

	b, err := json.Marshal(raw)
	if err != nil {
		return SubGraphConfig{}, fmt.Errorf("failed to marshal config: %w", err)
	}

	var cfg SubGraphConfig
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return SubGraphConfig{}, fmt.Errorf("failed to decode config: %w", err)
	}

	if cfg.Definition == nil {
		return SubGraphConfig{}, errors.New("sub_graph definition is required")
	}

	if _, err := ResolveParentClosePolicy(cfg.ParentClosePolicy); err != nil {
		return SubGraphConfig{}, err
	}

	return cfg, nil
}

// StartSubGraphChildWorkflow starts a child GraphExecutionWorkflow and returns its future.
func StartSubGraphChildWorkflow(
	ctx workflow.Context,
	parentExecID engine.ExecutionID,
	nodeID engine.NodeID,
	cfg SubGraphConfig,
	initialInputs map[string]any,
) (workflow.ChildWorkflowFuture, error) {
	if cfg.Definition == nil {
		return nil, errors.New("sub_graph definition is required")
	}

	policy, err := ResolveParentClosePolicy(cfg.ParentClosePolicy)
	if err != nil {
		return nil, err
	}

	childID := fmt.Sprintf("%s/%s", parentExecID, nodeID)
	opts := workflow.ChildWorkflowOptions{
		WorkflowID:        childID,
		ParentClosePolicy: policy,
	}

	childCtx := workflow.WithChildOptions(ctx, opts)
	childInput := GraphWorkflowInput{
		ExecutionID:  engine.ExecutionID(childID),
		Definition:   cfg.Definition,
		InitialState: initialInputs,
	}

	return workflow.ExecuteChildWorkflow(childCtx, GraphExecutionWorkflowName, childInput), nil
}

// ExecuteSubGraphChildWorkflow launches a child GraphExecutionWorkflow and waits for its result.
func ExecuteSubGraphChildWorkflow(
	ctx workflow.Context,
	parentExecID engine.ExecutionID,
	nodeID engine.NodeID,
	cfg SubGraphConfig,
	initialInputs map[string]any,
) (*GraphWorkflowResult, error) {
	fut, err := StartSubGraphChildWorkflow(ctx, parentExecID, nodeID, cfg, initialInputs)
	if err != nil {
		return nil, err
	}

	var res GraphWorkflowResult
	if err := fut.Get(ctx, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// MergeChildWorkflowResult merges the results of a child workflow execution.
func MergeChildWorkflowResult(nodeID engine.NodeID, childRes *GraphWorkflowResult) (*NodeActivityResult, error) {
	if childRes == nil {
		return nil, WrapTemporalError(engine.NewNonRetryableError(errors.New("child workflow result is nil")))
	}

	if childRes.Status != engine.StatusSucceeded {
		errMsg := childRes.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("child workflow completed with status %s", childRes.Status)
		}

		var cause error
		if childRes.Status == engine.StatusCompensated || childRes.Status == engine.StatusCompensating {
			cause = engine.NewCriticalError(errors.New(errMsg))
		} else {
			cause = engine.NewNonRetryableError(errors.New(errMsg))
		}
		return nil, WrapTemporalError(cause)
	}

	merged := make(map[string]any)
	nodesOutputs := make(map[string]any)

	if childRes.FinalState != nil {
		nodeStates := childRes.FinalState.NodeStates()
		// NodeStates is supposed to be sorted deterministically, but we sort to be absolutely sure.
		sort.Slice(nodeStates, func(i, j int) bool {
			return nodeStates[i].NodeID() < nodeStates[j].NodeID()
		})

		for _, ns := range nodeStates {
			if ns.Status() == engine.StatusSucceeded {
				out := ns.Output()
				if out == nil {
					out = make(map[string]any)
				}
				for k, v := range out {
					merged[k] = v
				}
				nodesOutputs[string(ns.NodeID())] = out
			}
		}
	}

	if _, exists := merged["nodes"]; !exists {
		merged["nodes"] = nodesOutputs
	}

	mut := engine.NewStateMutation(nodeID, 0, nil, merged, nil, nil)
	return &NodeActivityResult{
		NodeID:   nodeID,
		Mutation: &mut,
	}, nil
}
