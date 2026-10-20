package orchestrator

import (
	"context"
	"sync"

	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

type signalWaitKey struct {
	execution engine.ExecutionID
	node      engine.NodeID
	signal    string
}

// TemporalSignalSource implements nodes.SignalSource for coordinating external signals with wait_for_signal nodes.
type TemporalSignalSource struct {
	mu      sync.Mutex
	waiters map[signalWaitKey]chan map[string]any
	buffer  map[signalWaitKey][]map[string]any
}

var _ nodes.SignalSource = (*TemporalSignalSource)(nil)

// NewTemporalSignalSource creates a new TemporalSignalSource.
func NewTemporalSignalSource() *TemporalSignalSource {
	return &TemporalSignalSource{
		waiters: make(map[signalWaitKey]chan map[string]any),
		buffer:  make(map[signalWaitKey][]map[string]any),
	}
}

// Wait blocks until the matching signal is delivered or the context is cancelled.
func (s *TemporalSignalSource) Wait(
	ctx context.Context,
	execution engine.ExecutionID,
	node engine.NodeID,
	signal string,
) (map[string]any, error) {
	key := signalWaitKey{
		execution: execution,
		node:      node,
		signal:    signal,
	}

	s.mu.Lock()
	if bufs, ok := s.buffer[key]; ok && len(bufs) > 0 {
		payload := bufs[0]
		s.buffer[key] = bufs[1:]
		s.mu.Unlock()
		return payload, nil
	}

	ch := make(chan map[string]any, 1)
	s.waiters[key] = ch
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.waiters, key)
		s.mu.Unlock()
		return nil, ctx.Err()
	case payload := <-ch:
		return payload, nil
	}
}

// Deliver buffers or sends the signal payload to the matching waiter.
func (s *TemporalSignalSource) Deliver(
	execution engine.ExecutionID,
	sig ExternalSignalPayload,
) {
	key := signalWaitKey{
		execution: execution,
		node:      sig.TargetNodeID,
		signal:    sig.SignalName,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if ch, ok := s.waiters[key]; ok {
		delete(s.waiters, key)
		ch <- sig.Payload
		return
	}

	s.buffer[key] = append(s.buffer[key], sig.Payload)
}
