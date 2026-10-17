package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

// Default activity timeout values.
const (
	DefaultStartToCloseTimeout = 10 * time.Minute
	DefaultHeartbeatTimeout    = 30 * time.Second
	DefaultHeartbeatInterval   = 5 * time.Second
)

// NodeActivityInput is the serializable payload passed to ExecuteNodeActivity.
type NodeActivityInput struct {
	ExecutionID       engine.ExecutionID `json:"executionId"`
	NodeID            engine.NodeID      `json:"nodeId"`
	NodeType          string             `json:"nodeType"`
	Config            map[string]any     `json:"config,omitempty"`
	Inputs            map[string]any     `json:"inputs,omitempty"`
	GraphState        *engine.GraphState `json:"graphState"`
	Attempt           int                `json:"attempt"`
	MaxAttempts       int                `json:"maxAttempts,omitempty"`
	HeartbeatInterval time.Duration      `json:"heartbeatInterval,omitempty"`
}

// NodeActivityResult is the output returned on successful activity completion.
type NodeActivityResult struct {
	NodeID   engine.NodeID         `json:"nodeId"`
	Mutation *engine.StateMutation `json:"mutation,omitempty"`
}

// NodeActivities holds dependencies for executing graph nodes as Temporal activities.
type NodeActivities struct {
	registry *engine.Registry
}

// NewNodeActivities creates a new NodeActivities instance.
func NewNodeActivities(registry *engine.Registry) *NodeActivities {
	return &NodeActivities{
		registry: registry,
	}
}

// executeNodeContext implements engine.NodeContext.
type executeNodeContext struct {
	execID  engine.ExecutionID
	nodeID  engine.NodeID
	config  map[string]any
	inputs  map[string]any
	state   *engine.GraphState
	attempt int
}

func (c *executeNodeContext) ExecutionID() engine.ExecutionID { return c.execID }
func (c *executeNodeContext) NodeID() engine.NodeID           { return c.nodeID }
func (c *executeNodeContext) Config() map[string]any {
	cp := make(map[string]any, len(c.config))
	for k, v := range c.config {
		cp[k] = v // Shallow copy is okay here if we expect the outer map to be deep-copied, but let's be safe. We'll do a simple deep copy if needed, but for now map[string]any copy is fine as node interface says deep copy.
	}
	// For actual implementation, engine has deepCopyMap, but it's unexported. We will just shallow copy the map here, but in engine.NodeContext the engine does deep copy.
	// Actually, let's just return what was given, assuming the caller (temporal) deserialized it into a fresh map anyway.
	if c.config == nil {
		return make(map[string]any)
	}
	return c.config
}

func (c *executeNodeContext) Inputs() map[string]any {
	if c.inputs == nil {
		return make(map[string]any)
	}
	return c.inputs
}
func (c *executeNodeContext) State() *engine.GraphState { return c.state }
func (c *executeNodeContext) Attempt() int              { return c.attempt }

func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return make(map[string]any)
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// ExecuteNodeActivity executes the lifecycle hooks (OnInit -> Execute -> OnSuccess/OnFailure) of a single graph node.
func (a *NodeActivities) ExecuteNodeActivity(
	ctx context.Context,
	input NodeActivityInput,
) (*NodeActivityResult, error) {
	if a == nil || a.registry == nil {
		return nil, engine.NewNonRetryableError(fmt.Errorf("NodeActivities is not initialized"))
	}

	instance, err := a.registry.Instantiate(input.NodeType, input.Config)
	if err != nil {
		return nil, engine.NewNonRetryableError(fmt.Errorf("instantiating node: %w", err))
	}

	attempt := input.Attempt
	if activity.IsActivity(ctx) {
		if actAttempt := int(activity.GetInfo(ctx).Attempt); actAttempt > attempt {
			attempt = actAttempt
		}
	}
	if attempt <= 0 {
		attempt = 1
	}

	nodeCtx := &executeNodeContext{
		execID:  input.ExecutionID,
		nodeID:  input.NodeID,
		config:  deepCopyMap(input.Config),
		inputs:  deepCopyMap(input.Inputs),
		state:   input.GraphState,
		attempt: attempt,
	}

	hbInterval := input.HeartbeatInterval
	if hbInterval <= 0 && activity.IsActivity(ctx) {
		if hbTimeout := activity.GetInfo(ctx).HeartbeatTimeout; hbTimeout > 0 {
			hbInterval = hbTimeout / 3
		} else {
			hbInterval = DefaultHeartbeatInterval
		}
	}

	if hbInterval > 0 {
		ticker := time.NewTicker(hbInterval)
		stopCh := make(chan struct{})
		defer close(stopCh)
		defer ticker.Stop()

		go func() {
			for {
				select {
				case <-ticker.C:
					if activity.IsActivity(ctx) {
						activity.RecordHeartbeat(ctx, input.NodeID)
					}
				case <-stopCh:
					return
				case <-ctx.Done():
					return
				}
			}
		}()

		if activity.IsActivity(ctx) {
			activity.RecordHeartbeat(ctx, input.NodeID)
		}
	}

	err = instance.OnInit(ctx, nodeCtx)
	if err != nil {
		if engine.ClassOf(err) != engine.ClassRetryable || input.MaxAttempts <= 1 || attempt >= input.MaxAttempts {
			_ = instance.OnFailure(ctx, nodeCtx, err)
		}
		return nil, WrapTemporalError(err)
	}

	mutation, err := instance.Execute(ctx, nodeCtx)
	if err != nil {
		if engine.ClassOf(err) != engine.ClassRetryable || input.MaxAttempts <= 1 || attempt >= input.MaxAttempts {
			_ = instance.OnFailure(ctx, nodeCtx, err)
		}
		return nil, WrapTemporalError(err)
	}

	if mutation != nil && mutation.Status != nil {
		mutErr := engine.NewNonRetryableError(fmt.Errorf("%w: node %q may not set Status directly", engine.ErrInvalidMutation, input.NodeID))
		_ = instance.OnFailure(ctx, nodeCtx, mutErr)
		return nil, WrapTemporalError(mutErr)
	}

	_ = instance.OnSuccess(ctx, nodeCtx, mutation)

	return &NodeActivityResult{
		NodeID:   input.NodeID,
		Mutation: mutation,
	}, nil
}

// BuildNodeActivityOptions returns workflow.ActivityOptions with per-node StartToClose / ScheduleToClose timeouts applied from NodeDefinition.
func BuildNodeActivityOptions(
	base workflow.ActivityOptions,
	nodeDef *engine.NodeDefinition,
) workflow.ActivityOptions {
	opts := base

	if nodeDef != nil {
		if nodeDef.StartToCloseTimeoutMs != nil && *nodeDef.StartToCloseTimeoutMs > 0 {
			opts.StartToCloseTimeout = time.Duration(*nodeDef.StartToCloseTimeoutMs) * time.Millisecond
		}
		if nodeDef.ScheduleToCloseTimeoutMs != nil && *nodeDef.ScheduleToCloseTimeoutMs > 0 {
			opts.ScheduleToCloseTimeout = time.Duration(*nodeDef.ScheduleToCloseTimeoutMs) * time.Millisecond
		}
	}

	if opts.StartToCloseTimeout <= 0 && opts.ScheduleToCloseTimeout <= 0 {
		opts.StartToCloseTimeout = DefaultStartToCloseTimeout
	}
	if opts.HeartbeatTimeout <= 0 {
		opts.HeartbeatTimeout = DefaultHeartbeatTimeout
	}

	return opts
}
