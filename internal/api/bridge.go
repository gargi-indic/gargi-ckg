package api

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	gargiv1 "github.com/gargi-indic/gargi-flow/pkg/api/v1"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EventSubscriber defines the capability for subscribing to reactive execution events.
// Stores like Redis and Memory implement this interface for multi-replica event distribution.
type EventSubscriber interface {
	Subscribe(ctx context.Context, executionID engine.ExecutionID) (<-chan *engine.Event, error)
}

// BackpressurePolicy defines the policy when a subscriber's buffer overflows.
type BackpressurePolicy string

const (
	// BackpressurePolicyDisconnect closes the subscriber stream with an error when buffer overflows.
	// This ensures strict consistency for real-time consumers by avoiding missing/stale updates.
	BackpressurePolicyDisconnect BackpressurePolicy = "disconnect"

	// BackpressurePolicyDropOldest discards the oldest unread event when the buffer overflows.
	BackpressurePolicyDropOldest BackpressurePolicy = "drop_oldest"
)

// FanoutBridgeOptions configures the update fan-out bridge.
type FanoutBridgeOptions struct {
	// BufferSize is the bounded buffer capacity for subscriber events. Default is 256.
	BufferSize int

	// BackpressurePolicy determines action when BufferSize is exceeded. Default is Disconnect.
	BackpressurePolicy BackpressurePolicy

	// GapFillBatchSize is the maximum number of logs fetched per gap-fill query. Default is 200.
	GapFillBatchSize int

	// StatePollInterval is the interval to poll state store for direct state mutations. Default is 50ms.
	StatePollInterval time.Duration
}

// DefaultFanoutBridgeOptions returns default options for the FanoutBridge.
func DefaultFanoutBridgeOptions() FanoutBridgeOptions {
	return FanoutBridgeOptions{
		BufferSize:         256,
		BackpressurePolicy: BackpressurePolicyDisconnect,
		GapFillBatchSize:   200,
		StatePollInterval:  50 * time.Millisecond,
	}
}

// FanoutBridge bridges internal events across API replicas to gRPC, SSE, and WebSocket subscribers.
// It handles gap-filling from execution logs on reconnect and enforces backpressure policies.
type FanoutBridge struct {
	store      engine.StateStore
	subscriber EventSubscriber
	logStore   engine.ExecutionLog
	logger     *slog.Logger
	opts       FanoutBridgeOptions
}

// NewFanoutBridge creates a new FanoutBridge.
func NewFanoutBridge(store engine.StateStore, logger *slog.Logger, opts ...FanoutBridgeOptions) *FanoutBridge {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := DefaultFanoutBridgeOptions()
	if len(opts) > 0 {
		userOpts := opts[0]
		if userOpts.BufferSize > 0 {
			cfg.BufferSize = userOpts.BufferSize
		}
		if userOpts.BackpressurePolicy != "" {
			cfg.BackpressurePolicy = userOpts.BackpressurePolicy
		}
		if userOpts.GapFillBatchSize > 0 {
			cfg.GapFillBatchSize = userOpts.GapFillBatchSize
		}
		if userOpts.StatePollInterval > 0 {
			cfg.StatePollInterval = userOpts.StatePollInterval
		}
	}

	var sub EventSubscriber
	if s, ok := store.(EventSubscriber); ok {
		sub = s
	} else if comp, ok := store.(interface{ Events() engine.EventPublisher }); ok && comp.Events() != nil {
		if s, ok := comp.Events().(EventSubscriber); ok {
			sub = s
		}
	}

	var logStore engine.ExecutionLog
	if l, ok := store.(engine.ExecutionLog); ok {
		logStore = l
	} else if comp, ok := store.(interface{ Log() engine.ExecutionLog }); ok && comp.Log() != nil {
		logStore = comp.Log()
	}

	return &FanoutBridge{
		store:      store,
		subscriber: sub,
		logStore:   logStore,
		logger:     logger,
		opts:       cfg,
	}
}

// eventQueue provides a thread-safe bounded ring buffer with configurable backpressure.
type eventQueue struct {
	mu       sync.Mutex
	items    []*engine.Event
	capacity int
	policy   BackpressurePolicy
	notify   chan struct{}
	closed   bool
	err      error
}

func newEventQueue(capacity int, policy BackpressurePolicy) *eventQueue {
	return &eventQueue{
		capacity: capacity,
		policy:   policy,
		items:    make([]*engine.Event, 0, capacity),
		notify:   make(chan struct{}, 1),
	}
}

func (q *eventQueue) push(ev *engine.Event) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return errors.New("bridge: queue closed")
	}

	if len(q.items) >= q.capacity {
		switch q.policy {
		case BackpressurePolicyDropOldest:
			if len(q.items) > 0 {
				q.items = q.items[1:]
			}
			q.items = append(q.items, ev)
		case BackpressurePolicyDisconnect:
			fallthrough
		default:
			q.err = errors.New("bridge: slow consumer buffer overflow")
			q.closed = true
			select {
			case q.notify <- struct{}{}:
			default:
			}
			return q.err
		}
	} else {
		q.items = append(q.items, ev)
	}

	select {
	case q.notify <- struct{}{}:
	default:
	}
	return nil
}

func (q *eventQueue) pop() (*engine.Event, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) > 0 {
		ev := q.items[0]
		q.items = q.items[1:]
		if len(q.items) > 0 {
			select {
			case q.notify <- struct{}{}:
			default:
			}
		}
		return ev, nil
	}

	if q.err != nil {
		return nil, q.err
	}
	return nil, nil
}

func (q *eventQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Stream streams execution updates with log gap-fill, live event fanout, and direct state synchronization.
func (b *FanoutBridge) Stream(
	ctx context.Context,
	req *gargiv1.StreamExecutionUpdatesRequest,
	stream gargiv1.GargiService_StreamExecutionUpdatesServer,
) error {
	if req == nil || req.GetExecutionId() == "" {
		return errors.New("bridge: execution_id is required")
	}

	execID := engine.ExecutionID(req.GetExecutionId())
	fromSeq := req.GetFromSequence()
	var highestSeq uint64

	// Start live subscription early to ensure no events are dropped while gap-filling
	var liveCh <-chan *engine.Event
	if b.subscriber != nil {
		ch, err := b.subscriber.Subscribe(ctx, execID)
		if err == nil {
			liveCh = ch
		} else {
			b.logger.Warn("bridge: failed to subscribe to live events", "execution_id", execID, "error", err)
		}
	}

	// Step 1: Gap-fill from execution log if logStore is available
	if b.logStore != nil {
		logs, err := b.logStore.GetLogs(ctx, execID, fromSeq, b.opts.GapFillBatchSize)
		if err == nil {
			for _, entry := range logs {
				if entry == nil {
					continue
				}
				resp := &gargiv1.StreamExecutionUpdatesResponse{
					ExecutionId:  string(entry.ExecutionID),
					Sequence:     entry.Sequence,
					Timestamp:    timestamppb.New(entry.Timestamp),
					EventType:    entry.Message,
					ErrorMessage: "",
				}
				if len(entry.Details) > 0 {
					if pbVal, err := structpb.NewValue(entry.Details); err == nil {
						resp.Payload = pbVal
					}
					if nID, ok := entry.Details["nodeId"].(string); ok {
						resp.NodeId = nID
					} else if nID, ok := entry.Details["node_id"].(string); ok {
						resp.NodeId = nID
					}

					var fromStatusStr, toStatusStr string
					if oldSt, ok := entry.Details["oldStatus"].(string); ok {
						fromStatusStr = oldSt
					} else if oldSt, ok := entry.Details["from_status"].(string); ok {
						fromStatusStr = oldSt
					}

					if newSt, ok := entry.Details["newStatus"].(string); ok {
						toStatusStr = newSt
					} else if newSt, ok := entry.Details["status"].(string); ok {
						toStatusStr = newSt
					} else if newSt, ok := entry.Details["to_status"].(string); ok {
						toStatusStr = newSt
					}

					if fromStatusStr != "" {
						resp.FromStatus = toPBNodeStatus(engine.Status(fromStatusStr))
					}
					if toStatusStr != "" {
						resp.ToStatus = toPBNodeStatus(engine.Status(toStatusStr))
					}
				}

				if err := stream.Send(resp); err != nil {
					return err
				}

				if entry.Sequence > atomic.LoadUint64(&highestSeq) {
					atomic.StoreUint64(&highestSeq, entry.Sequence)
				}
			}
		}
	}

	// Step 2: Stream live events and poll store state concurrently
	q := newEventQueue(b.opts.BufferSize, b.opts.BackpressurePolicy)
	defer q.close()

	// Ingress reader goroutine: pushes from liveCh into the thread-safe bounded event queue
	if liveCh != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-liveCh:
					if !ok || event == nil {
						return
					}
					// Deduplicate events already emitted during gap-fill
					if event.Sequence() > 0 && event.Sequence() <= atomic.LoadUint64(&highestSeq) {
						continue
					}
					if err := q.push(event); err != nil {
						return
					}
				}
			}
		}()
	}

	pollTicker := time.NewTicker(b.opts.StatePollInterval)
	defer pollTicker.Stop()

	lastStatuses := make(map[engine.NodeID]engine.Status)

	// Egress loop: processes live events from queue and polls state changes from store
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-q.notify:
			for {
				event, err := q.pop()
				if err != nil {
					return err
				}
				if event == nil {
					break
				}

				resp := eventToUpdate(event, lastStatuses)

				if err := stream.Send(resp); err != nil {
					return err
				}

				for {
					curr := atomic.LoadUint64(&highestSeq)
					if event.Sequence() <= curr {
						break
					}
					if atomic.CompareAndSwapUint64(&highestSeq, curr, event.Sequence()) {
						break
					}
				}

				// Complete stream on terminal event
				if event.Type() == engine.EventTypeGraphCompleted || event.Type() == engine.EventTypeGraphFailed {
					return nil
				}
			}

		case <-pollTicker.C:
			if b.store == nil {
				continue
			}
			st, err := b.store.GetState(ctx, execID)
			if err != nil || st == nil {
				continue
			}

			for _, nodeState := range st.NodeStates() {
				nodeID := nodeState.NodeID()
				currStatus := nodeState.Status()
				prevStatus, exists := lastStatuses[nodeID]

				if !exists || currStatus != prevStatus {
					lastStatuses[nodeID] = currStatus
					highestSeq++

					var outVal *structpb.Value
					if outMap := nodeState.Output(); len(outMap) > 0 {
						outVal, _ = structpb.NewValue(outMap)
					}

					resp := &gargiv1.StreamExecutionUpdatesResponse{
						ExecutionId:  string(execID),
						Sequence:     highestSeq,
						Timestamp:    timestamppb.Now(),
						EventType:    "node_state_changed",
						NodeId:       string(nodeID),
						FromStatus:   toPBNodeStatus(prevStatus),
						ToStatus:     toPBNodeStatus(currStatus),
						Payload:      outVal,
						ErrorMessage: nodeState.ErrorMessage(),
					}

					if err := stream.Send(resp); err != nil {
						return err
					}
				}
			}

			if st.Status().IsTerminal() {
				highestSeq++
				eventType := "graph_completed"
				if st.Status() == engine.StatusFailed {
					eventType = "graph_failed"
				}
				_ = stream.Send(&gargiv1.StreamExecutionUpdatesResponse{
					ExecutionId: string(execID),
					Sequence:    highestSeq,
					Timestamp:   timestamppb.Now(),
					EventType:   eventType,
					ToStatus:    toPBNodeStatus(st.Status()),
				})
				return nil
			}
		}
	}
}

// eventToUpdate converts an engine event into the StreamExecutionUpdates
// message. Node completion and failure events are reported as
// node_state_changed transitions; lastStatuses tracks each node's previous
// status across calls and is updated.
func eventToUpdate(event *engine.Event, lastStatuses map[engine.NodeID]engine.Status) *gargiv1.StreamExecutionUpdatesResponse {
	resp := &gargiv1.StreamExecutionUpdatesResponse{
		ExecutionId: string(event.ExecutionID()),
		Sequence:    event.Sequence(),
		Timestamp:   timestamppb.New(event.Timestamp()),
		EventType:   string(event.Type()),
		NodeId:      string(event.NodeID()),
	}

	switch p := event.Payload().(type) {
	case engine.NodeStateChangedPayload:
		resp.FromStatus = toPBNodeStatus(p.OldStatus)
		resp.ToStatus = toPBNodeStatus(p.NewStatus)
		resp.ErrorMessage = p.ErrorMessage
		lastStatuses[event.NodeID()] = p.NewStatus
		if len(p.OutputPatch) > 0 {
			if pbVal, err := structpb.NewValue(p.OutputPatch); err == nil {
				resp.Payload = pbVal
			}
		}
	case engine.NodeCompletedPayload:
		resp.EventType = "node_state_changed"
		if prev, ok := lastStatuses[event.NodeID()]; ok {
			resp.FromStatus = toPBNodeStatus(prev)
		}
		resp.ToStatus = toPBNodeStatus(p.Status)
		lastStatuses[event.NodeID()] = p.Status
		if len(p.Output) > 0 {
			if pbVal, err := structpb.NewValue(p.Output); err == nil {
				resp.Payload = pbVal
			}
		}
	case engine.NodeFailedPayload:
		resp.EventType = "node_state_changed"
		if prev, ok := lastStatuses[event.NodeID()]; ok {
			resp.FromStatus = toPBNodeStatus(prev)
		}
		resp.ToStatus = toPBNodeStatus(p.Status)
		resp.ErrorMessage = p.ErrorMessage
		lastStatuses[event.NodeID()] = p.Status
	default:
		if p != nil {
			if pbVal, err := structpb.NewValue(p); err == nil {
				resp.Payload = pbVal
			}
		}
	}
	return resp
}
