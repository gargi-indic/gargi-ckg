package orchestrator

import (
	"context"
	"fmt"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"go.temporal.io/sdk/client"
)

const (
	// SignalChannelName is the Temporal signal channel name used for external node signals.
	SignalChannelName = "external_node_signal"
	// QueryTypeRejectedSignals is the Temporal query name returning signals sent to unknown or terminal nodes.
	QueryTypeRejectedSignals = "rejected_signals"
)

// ExternalSignalPayload defines the schema for external signal delivery.
type ExternalSignalPayload struct {
	TargetNodeID engine.NodeID  `json:"targetNodeId"`
	SignalName   string         `json:"signalName"`
	Payload      map[string]any `json:"payload,omitempty"`
}

// RejectedSignal records a signal that was rejected because the target node was unknown or already terminal.
type RejectedSignal struct {
	Signal ExternalSignalPayload `json:"signal"`
	Reason string                `json:"reason"`
}

// SendNodeSignal sends a signal to a running graph execution workflow via the Temporal client.
func SendNodeSignal(
	ctx context.Context,
	c client.Client,
	workflowID string,
	runID string,
	signal ExternalSignalPayload,
) error {
	if c == nil {
		return fmt.Errorf("client cannot be nil")
	}
	if workflowID == "" {
		return fmt.Errorf("workflowID cannot be empty")
	}
	if signal.TargetNodeID == "" {
		return fmt.Errorf("target node ID cannot be empty")
	}
	if signal.SignalName == "" {
		return fmt.Errorf("signal name cannot be empty")
	}

	return c.SignalWorkflow(ctx, workflowID, runID, SignalChannelName, signal)
}

// SignalRouter manages buffered and rejected signals within the workflow execution.
type SignalRouter struct {
	bufferedSignals map[engine.NodeID][]ExternalSignalPayload
	rejectedSignals []RejectedSignal
}

// NewSignalRouter creates a new SignalRouter.
func NewSignalRouter() *SignalRouter {
	return &SignalRouter{
		bufferedSignals: make(map[engine.NodeID][]ExternalSignalPayload),
		rejectedSignals: make([]RejectedSignal, 0),
	}
}

// RouteSignal buffers a signal for the target node.
func (r *SignalRouter) RouteSignal(signal ExternalSignalPayload) {
	r.bufferedSignals[signal.TargetNodeID] = append(r.bufferedSignals[signal.TargetNodeID], signal)
}

// ReceiveSignal finds, removes, and returns the first buffered signal for nodeID.
func (r *SignalRouter) ReceiveSignal(nodeID engine.NodeID) (map[string]any, bool) {
	return r.ReceiveNamedSignal(nodeID, "")
}

// ReceiveNamedSignal finds, removes, and returns the first buffered signal for nodeID matching signalName.
func (r *SignalRouter) ReceiveNamedSignal(nodeID engine.NodeID, signalName string) (map[string]any, bool) {
	signals := r.bufferedSignals[nodeID]
	for i, sig := range signals {
		if signalName == "" || sig.SignalName == signalName {
			r.bufferedSignals[nodeID] = append(signals[:i], signals[i+1:]...)
			return sig.Payload, true
		}
	}
	return nil, false
}

// RecordRejected records a signal that was rejected.
func (r *SignalRouter) RecordRejected(signal ExternalSignalPayload, reason string) {
	r.rejectedSignals = append(r.rejectedSignals, RejectedSignal{
		Signal: signal,
		Reason: reason,
	})
}

// RejectedSignals returns a copy of the rejected signals.
func (r *SignalRouter) RejectedSignals() []RejectedSignal {
	out := make([]RejectedSignal, len(r.rejectedSignals))
	copy(out, r.rejectedSignals)
	return out
}
