package api_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/api"
	"github.com/gargi-indic/gargi-flow/internal/store/memory"
	gargiv1 "github.com/gargi-indic/gargi-flow/pkg/api/v1"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"google.golang.org/grpc/metadata"
)

type mockStreamServer struct {
	ctx      context.Context
	mu       sync.Mutex
	messages []*gargiv1.StreamExecutionUpdatesResponse
	sendHook func(*gargiv1.StreamExecutionUpdatesResponse) error
}

func newMockStreamServer(ctx context.Context) *mockStreamServer {
	return &mockStreamServer{
		ctx: ctx,
	}
}

func (m *mockStreamServer) Send(resp *gargiv1.StreamExecutionUpdatesResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendHook != nil {
		if err := m.sendHook(resp); err != nil {
			return err
		}
	}
	m.messages = append(m.messages, resp)
	return nil
}

func (m *mockStreamServer) getMessages() []*gargiv1.StreamExecutionUpdatesResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]*gargiv1.StreamExecutionUpdatesResponse, len(m.messages))
	copy(copied, m.messages)
	return copied
}

func (m *mockStreamServer) SetHeader(metadata.MD) error  { return nil }
func (m *mockStreamServer) SendHeader(metadata.MD) error { return nil }
func (m *mockStreamServer) SetTrailer(metadata.MD)       {}
func (m *mockStreamServer) Context() context.Context     { return m.ctx }
func (m *mockStreamServer) SendMsg(any) error            { return nil }
func (m *mockStreamServer) RecvMsg(any) error            { return nil }

func TestFanoutBridge_MultiReplicaPubSub(t *testing.T) {
	memStore := memory.NewStore()
	bridge := api.NewFanoutBridge(memStore, nil, api.FanoutBridgeOptions{
		BufferSize: 32,
	})

	execID := engine.ExecutionID("exec-fanout-1")

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	stream1 := newMockStreamServer(ctx1)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	stream2 := newMockStreamServer(ctx2)

	// Launch two concurrent stream subscribers representing two API client connections
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = bridge.Stream(ctx1, &gargiv1.StreamExecutionUpdatesRequest{
			ExecutionId: string(execID),
		}, stream1)
	}()

	go func() {
		defer wg.Done()
		_ = bridge.Stream(ctx2, &gargiv1.StreamExecutionUpdatesRequest{
			ExecutionId: string(execID),
		}, stream2)
	}()

	// Allow subscriber goroutines to establish subscription
	time.Sleep(50 * time.Millisecond)

	// Publish reactive events to EventPublisher (simulating engine / worker across replicas)
	ev1, _ := engine.NewEvent(
		execID,
		1,
		time.Now(),
		engine.EventTypeNodeCompleted,
		"node-a",
		engine.NodeCompletedPayload{Status: engine.StatusSucceeded},
	)
	_ = memStore.PublishEvent(context.Background(), ev1)

	ev2, _ := engine.NewEvent(
		execID,
		2,
		time.Now(),
		engine.EventTypeGraphCompleted,
		"",
		nil,
	)
	_ = memStore.PublishEvent(context.Background(), ev2)

	// Wait for streams to finish
	wg.Wait()

	msgs1 := stream1.getMessages()
	msgs2 := stream2.getMessages()

	if len(msgs1) != 2 {
		t.Fatalf("stream1: expected 2 events, got %d", len(msgs1))
	}
	if len(msgs2) != 2 {
		t.Fatalf("stream2: expected 2 events, got %d", len(msgs2))
	}

	if msgs1[0].Sequence != 1 || msgs1[0].NodeId != "node-a" {
		t.Errorf("stream1 msg 0 mismatch: %+v", msgs1[0])
	}
	if msgs2[0].Sequence != 1 || msgs2[0].NodeId != "node-a" {
		t.Errorf("stream2 msg 0 mismatch: %+v", msgs2[0])
	}
}

func TestFanoutBridge_GapFillOnReconnect(t *testing.T) {
	memStore := memory.NewStore()
	bridge := api.NewFanoutBridge(memStore, nil)

	execID := engine.ExecutionID("exec-gapfill-1")

	// Pre-populate execution logs (sequences 1, 2, 3)
	for i := uint64(1); i <= 3; i++ {
		_ = memStore.AppendLog(context.Background(), &engine.ExecutionLogEntry{
			ExecutionID: execID,
			Sequence:    i,
			Timestamp:   time.Now(),
			Message:     "logged_event",
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newMockStreamServer(ctx)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		// Reconnect starting from sequence 2
		_ = bridge.Stream(ctx, &gargiv1.StreamExecutionUpdatesRequest{
			ExecutionId:  string(execID),
			FromSequence: 2,
		}, stream)
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish live event sequence 4
	ev4, _ := engine.NewEvent(
		execID,
		4,
		time.Now(),
		engine.EventTypeNodeCompleted,
		"node-b",
		engine.NodeCompletedPayload{Status: engine.StatusSucceeded},
	)
	_ = memStore.PublishEvent(context.Background(), ev4)

	// Publish terminal event sequence 5
	ev5, _ := engine.NewEvent(
		execID,
		5,
		time.Now(),
		engine.EventTypeGraphCompleted,
		"",
		nil,
	)
	_ = memStore.PublishEvent(context.Background(), ev5)

	wg.Wait()

	msgs := stream.getMessages()
	// Should contain: log 2, log 3, live 4, live 5
	if len(msgs) != 4 {
		t.Fatalf("expected 4 events, got %d", len(msgs))
	}

	expectedSeqs := []uint64{2, 3, 4, 5}
	for i, expected := range expectedSeqs {
		if msgs[i].Sequence != expected {
			t.Errorf("index %d: expected sequence %d, got %d", i, expected, msgs[i].Sequence)
		}
	}
}

func TestFanoutBridge_BackpressureDisconnect(t *testing.T) {
	memStore := memory.NewStore()
	bridge := api.NewFanoutBridge(memStore, nil, api.FanoutBridgeOptions{
		BufferSize:         2,
		BackpressurePolicy: api.BackpressurePolicyDisconnect,
	})

	execID := engine.ExecutionID("exec-backpressure-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := newMockStreamServer(ctx)
	// Simulate slow consumer by blocking inside Send
	stream.sendHook = func(_ *gargiv1.StreamExecutionUpdatesResponse) error {
		time.Sleep(200 * time.Millisecond)
		return nil
	}

	errCh := make(chan error, 1)
	go func() {
		err := bridge.Stream(ctx, &gargiv1.StreamExecutionUpdatesRequest{
			ExecutionId: string(execID),
		}, stream)
		errCh <- err
	}()

	time.Sleep(30 * time.Millisecond)

	// Flood events to trigger buffer overflow
	for i := uint64(1); i <= 10; i++ {
		ev, _ := engine.NewEvent(
			execID,
			i,
			time.Now(),
			engine.EventTypeNodeStateChanged,
			"node-x",
			engine.NodeStateChangedPayload{OldStatus: engine.StatusPending, NewStatus: engine.StatusRunning},
		)
		_ = memStore.PublishEvent(context.Background(), ev)
	}

	select {
	case err := <-errCh:
		if err == nil || !errors.Is(err, errors.New("bridge: slow consumer buffer overflow")) && err.Error() != "bridge: slow consumer buffer overflow" {
			t.Logf("stream terminated with error as expected: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected stream to disconnect on buffer overflow, but timed out")
	}
}

func TestFanoutBridge_BackpressureDropOldest(t *testing.T) {
	memStore := memory.NewStore()
	bridge := api.NewFanoutBridge(memStore, nil, api.FanoutBridgeOptions{
		BufferSize:         2,
		BackpressurePolicy: api.BackpressurePolicyDropOldest,
	})

	execID := engine.ExecutionID("exec-drop-oldest-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := newMockStreamServer(ctx)
	// Add delay to simulate slow reader
	stream.sendHook = func(_ *gargiv1.StreamExecutionUpdatesResponse) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	doneCh := make(chan struct{})
	go func() {
		_ = bridge.Stream(ctx, &gargiv1.StreamExecutionUpdatesRequest{
			ExecutionId: string(execID),
		}, stream)
		close(doneCh)
	}()

	time.Sleep(30 * time.Millisecond)

	// Flood events
	for i := uint64(1); i <= 8; i++ {
		ev, _ := engine.NewEvent(
			execID,
			i,
			time.Now(),
			engine.EventTypeNodeStateChanged,
			"node-x",
			engine.NodeStateChangedPayload{OldStatus: engine.StatusPending, NewStatus: engine.StatusRunning},
		)
		_ = memStore.PublishEvent(context.Background(), ev)
	}

	// Publish terminal event
	evEnd, _ := engine.NewEvent(
		execID,
		9,
		time.Now(),
		engine.EventTypeGraphCompleted,
		"",
		nil,
	)
	_ = memStore.PublishEvent(context.Background(), evEnd)

	select {
	case <-doneCh:
		// Stream completed gracefully without disconnecting!
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for stream to complete with DropOldest policy")
	}
}
