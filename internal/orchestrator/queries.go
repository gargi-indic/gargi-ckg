package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

// Query types for the graph workflow execution state.
const (
	// QueryTypeGraphState is the query type to get the entire graph state.
	QueryTypeGraphState = "graph_state"
	// QueryTypeNodeState is the query type to get the state of a specific node.
	QueryTypeNodeState = "node_state"
)

// NodeStateQueryArgs holds the arguments for a node_state query.
type NodeStateQueryArgs struct {
	NodeID engine.NodeID `json:"nodeId"`
}

// RegisterWorkflowQueries sets up read-only query handlers ("graph_state" and "node_state") on the workflow context.
func RegisterWorkflowQueries(
	ctx workflow.Context,
	getState func() *engine.GraphState,
) error {
	err := workflow.SetQueryHandler(ctx, QueryTypeGraphState, func() (*engine.GraphState, error) {
		st := getState()
		if st == nil {
			return nil, errors.New("graph state is not initialized")
		}
		return st, nil
	})
	if err != nil {
		return err
	}

	err = workflow.SetQueryHandler(ctx, QueryTypeNodeState, func(arg any) (*engine.NodeState, error) {
		var nodeID engine.NodeID

		switch v := arg.(type) {
		case string:
			nodeID = engine.NodeID(v)
		case engine.NodeID:
			nodeID = v
		case map[string]any:
			if idAny, ok := v["nodeId"]; ok {
				if idStr, okStr := idAny.(string); okStr {
					nodeID = engine.NodeID(idStr)
				}
			}
		case NodeStateQueryArgs:
			nodeID = v.NodeID
		}

		if nodeID == "" {
			return nil, errors.New("nodeID is required")
		}

		st := getState()
		if st == nil {
			return nil, errors.New("graph state is not initialized")
		}

		ns := st.NodeState(nodeID)
		if ns == nil {
			return nil, fmt.Errorf("node %q not found in graph state", nodeID)
		}

		return ns, nil
	})
	return err
}

// FetchStateWithFallback attempts to fetch the latest GraphState from the StateStore.
// If the StateStore is nil, or if it returns StoreNotFoundError or StoreUnavailableError,
// it falls back to querying the running Temporal workflow via temporalClient.QueryWorkflow(ctx, workflowID, runID, QueryTypeGraphState).
func FetchStateWithFallback(
	ctx context.Context,
	store engine.StateStore,
	temporalClient client.Client,
	workflowID string,
	runID string,
) (*engine.GraphState, error) {
	if store != nil {
		st, err := store.GetState(ctx, engine.ExecutionID(workflowID))
		if err == nil && st != nil {
			return st, nil
		}
		if err != nil && !engine.IsStoreNotFound(err) && !engine.IsStoreUnavailable(err) && temporalClient == nil {
			return nil, err
		}
	}

	if temporalClient != nil {
		qres, err := temporalClient.QueryWorkflow(ctx, workflowID, runID, QueryTypeGraphState)
		if err != nil {
			return nil, fmt.Errorf("fallback workflow query failed: %w", err)
		}
		var st engine.GraphState
		if err := qres.Get(&st); err != nil {
			return nil, fmt.Errorf("decoding workflow query state: %w", err)
		}
		return &st, nil
	}

	return nil, fmt.Errorf("state store unavailable and no temporal client provided for fallback")
}
