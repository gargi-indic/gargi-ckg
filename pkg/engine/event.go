package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// EventType defines the classification of internal reactive events.
type EventType string

const (
	EventTypeNodeStateChanged       EventType = "node_state_changed"
	EventTypeNodeCompleted          EventType = "node_completed"
	EventTypeNodeFailed             EventType = "node_failed"
	EventTypeExternalSignalReceived EventType = "external_signal_received"
	EventTypeGraphCompleted         EventType = "graph_completed"
	EventTypeGraphFailed            EventType = "graph_failed"
)

// Valid reports whether t is a known EventType constant.
func (t EventType) Valid() bool {
	switch t {
	case EventTypeNodeStateChanged, EventTypeNodeCompleted, EventTypeNodeFailed,
		EventTypeExternalSignalReceived, EventTypeGraphCompleted, EventTypeGraphFailed:
		return true
	default:
		return false
	}
}

// Standard Typed Payloads:

type NodeStateChangedPayload struct {
	OldStatus    Status         `json:"oldStatus"`
	NewStatus    Status         `json:"newStatus"`
	OutputPatch  map[string]any `json:"outputPatch,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
	Version      uint64         `json:"version"`
}

type NodeCompletedPayload struct {
	Status  Status         `json:"status"`
	Output  map[string]any `json:"output,omitempty"`
	Version uint64         `json:"version"`
}

type NodeFailedPayload struct {
	Status       Status     `json:"status"`
	ErrorClass   ErrorClass `json:"errorClass"`
	ErrorMessage string     `json:"errorMessage"`
	Version      uint64     `json:"version"`
}

type ExternalSignalReceivedPayload struct {
	SignalName string `json:"signalName"`
	Payload    any    `json:"payload,omitempty"`
}

type GraphCompletedPayload struct {
	FinalStatus Status `json:"finalStatus"`
	Version     uint64 `json:"version"`
}

type GraphFailedPayload struct {
	ErrorMessage string `json:"errorMessage"`
	Version      uint64 `json:"version"`
}

// Event represents a typed, immutable event envelope emitted during graph execution.
type Event struct {
	executionID ExecutionID
	sequence    uint64
	timestamp   time.Time
	eventType   EventType
	nodeID      NodeID // Empty for graph-level events
	payload     any
}

// NewEvent constructs an immutable Event envelope.
// Timestamp must be provided by the caller (never call time.Now() inside NewEvent)
// and is normalized to UTC (ts.UTC()).
func NewEvent(
	execID ExecutionID,
	seq uint64,
	ts time.Time,
	eventType EventType,
	nodeID NodeID,
	payload any,
) (*Event, error) {
	if execID == "" {
		return nil, errors.New("execution ID cannot be empty")
	}
	if seq == 0 {
		return nil, errors.New("sequence must be greater than zero")
	}
	if ts.IsZero() {
		return nil, errors.New("timestamp cannot be zero")
	}
	if !eventType.Valid() {
		return nil, fmt.Errorf("invalid event type: %s", eventType)
	}

	switch eventType {
	case EventTypeGraphCompleted, EventTypeGraphFailed:
		if nodeID != "" {
			return nil, fmt.Errorf("nodeID must be empty for graph-level event %s", eventType)
		}
	case EventTypeNodeStateChanged, EventTypeNodeCompleted, EventTypeNodeFailed, EventTypeExternalSignalReceived:
		if nodeID == "" {
			return nil, fmt.Errorf("nodeID must not be empty for node-level event %s", eventType)
		}
	}

	e := &Event{
		executionID: execID,
		sequence:    seq,
		timestamp:   ts.UTC(),
		eventType:   eventType,
		nodeID:      nodeID,
	}

	// Defensive copy logic for payload and dereferencing pointer payloads.
	var finalPayload any
	switch p := payload.(type) {
	case *NodeStateChangedPayload:
		if p != nil {
			finalPayload = NodeStateChangedPayload{
				OldStatus:    p.OldStatus,
				NewStatus:    p.NewStatus,
				OutputPatch:  deepCopyMap(p.OutputPatch),
				ErrorMessage: p.ErrorMessage,
				Version:      p.Version,
			}
		}
	case NodeStateChangedPayload:
		finalPayload = NodeStateChangedPayload{
			OldStatus:    p.OldStatus,
			NewStatus:    p.NewStatus,
			OutputPatch:  deepCopyMap(p.OutputPatch),
			ErrorMessage: p.ErrorMessage,
			Version:      p.Version,
		}
	case *NodeCompletedPayload:
		if p != nil {
			finalPayload = NodeCompletedPayload{
				Status:  p.Status,
				Output:  deepCopyMap(p.Output),
				Version: p.Version,
			}
		}
	case NodeCompletedPayload:
		finalPayload = NodeCompletedPayload{
			Status:  p.Status,
			Output:  deepCopyMap(p.Output),
			Version: p.Version,
		}
	case *NodeFailedPayload:
		if p != nil {
			finalPayload = NodeFailedPayload{
				Status:       p.Status,
				ErrorClass:   p.ErrorClass,
				ErrorMessage: p.ErrorMessage,
				Version:      p.Version,
			}
		}
	case NodeFailedPayload:
		finalPayload = NodeFailedPayload{
			Status:       p.Status,
			ErrorClass:   p.ErrorClass,
			ErrorMessage: p.ErrorMessage,
			Version:      p.Version,
		}
	case *ExternalSignalReceivedPayload:
		if p != nil {
			finalPayload = ExternalSignalReceivedPayload{
				SignalName: p.SignalName,
				Payload:    deepCopyAny(p.Payload),
			}
		}
	case ExternalSignalReceivedPayload:
		finalPayload = ExternalSignalReceivedPayload{
			SignalName: p.SignalName,
			Payload:    deepCopyAny(p.Payload),
		}
	case *GraphCompletedPayload:
		if p != nil {
			finalPayload = *p
		}
	case GraphCompletedPayload:
		finalPayload = p
	case *GraphFailedPayload:
		if p != nil {
			finalPayload = *p
		}
	case GraphFailedPayload:
		finalPayload = p
	default:
		// Fallback for any unknown types, just deep copy them if applicable
		finalPayload = deepCopyAny(payload)
	}

	e.payload = finalPayload
	return e, nil
}

func (e *Event) ExecutionID() ExecutionID {
	if e == nil {
		return ""
	}
	return e.executionID
}

func (e *Event) Sequence() uint64 {
	if e == nil {
		return 0
	}
	return e.sequence
}

func (e *Event) Timestamp() time.Time {
	if e == nil {
		return time.Time{}
	}
	return e.timestamp
}

func (e *Event) Type() EventType {
	if e == nil {
		return ""
	}
	return e.eventType
}

func (e *Event) NodeID() NodeID {
	if e == nil {
		return ""
	}
	return e.nodeID
}

func (e *Event) Payload() any {
	if e == nil {
		return nil
	}
	switch p := e.payload.(type) {
	case NodeStateChangedPayload:
		return NodeStateChangedPayload{
			OldStatus:    p.OldStatus,
			NewStatus:    p.NewStatus,
			OutputPatch:  deepCopyMap(p.OutputPatch),
			ErrorMessage: p.ErrorMessage,
			Version:      p.Version,
		}
	case NodeCompletedPayload:
		return NodeCompletedPayload{
			Status:  p.Status,
			Output:  deepCopyMap(p.Output),
			Version: p.Version,
		}
	case ExternalSignalReceivedPayload:
		return ExternalSignalReceivedPayload{
			SignalName: p.SignalName,
			Payload:    deepCopyAny(p.Payload),
		}
	default:
		return deepCopyAny(e.payload)
	}
}

type eventJSON struct {
	ExecutionID ExecutionID     `json:"executionId"`
	Sequence    uint64          `json:"sequence"`
	Timestamp   time.Time       `json:"timestamp"`
	Type        EventType       `json:"type"`
	NodeID      NodeID          `json:"nodeId,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

func (e Event) MarshalJSON() ([]byte, error) {
	var payloadBytes json.RawMessage
	if e.payload != nil {
		var err error
		payloadBytes, err = json.Marshal(e.payload)
		if err != nil {
			return nil, err
		}
	}

	return json.Marshal(eventJSON{
		ExecutionID: e.executionID,
		Sequence:    e.sequence,
		Timestamp:   e.timestamp,
		Type:        e.eventType,
		NodeID:      e.nodeID,
		Payload:     payloadBytes,
	})
}

func (e *Event) UnmarshalJSON(data []byte) error {
	var raw eventJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var payload any
	if len(raw.Payload) > 0 && string(raw.Payload) != "null" {
		switch raw.Type {
		case EventTypeNodeStateChanged:
			var p NodeStateChangedPayload
			if err := json.Unmarshal(raw.Payload, &p); err != nil {
				return err
			}
			payload = p
		case EventTypeNodeCompleted:
			var p NodeCompletedPayload
			if err := json.Unmarshal(raw.Payload, &p); err != nil {
				return err
			}
			payload = p
		case EventTypeNodeFailed:
			var p NodeFailedPayload
			if err := json.Unmarshal(raw.Payload, &p); err != nil {
				return err
			}
			payload = p
		case EventTypeExternalSignalReceived:
			var p ExternalSignalReceivedPayload
			if err := json.Unmarshal(raw.Payload, &p); err != nil {
				return err
			}
			payload = p
		case EventTypeGraphCompleted:
			var p GraphCompletedPayload
			if err := json.Unmarshal(raw.Payload, &p); err != nil {
				return err
			}
			payload = p
		case EventTypeGraphFailed:
			var p GraphFailedPayload
			if err := json.Unmarshal(raw.Payload, &p); err != nil {
				return err
			}
			payload = p
		default:
			// Just leave it as map[string]any or any, but since NewEvent will reject invalid event types,
			// it doesn't matter too much what we parse into here if it's invalid.
			if err := json.Unmarshal(raw.Payload, &payload); err != nil {
				return err
			}
		}
	}

	parsedEvent, err := NewEvent(
		raw.ExecutionID,
		raw.Sequence,
		raw.Timestamp,
		raw.Type,
		raw.NodeID,
		payload,
	)
	if err != nil {
		return err
	}

	*e = *parsedEvent
	return nil
}
