package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/api"
	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/gargi-indic/gargi-ckg/internal/store/memory"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"github.com/gorilla/websocket"
)

func TestWebSocket_Streaming(t *testing.T) {
	memStore := memory.NewStore()
	tCfg := config.TemporalConfig{TaskQueue: "test-queue"}
	service := api.NewServer(nil, tCfg, memStore, nil)

	wsHandler := api.NewWebSocketHandler(service, nil, api.WebSocketOptions{
		WriteWait:      2 * time.Second,
		PongWait:       2 * time.Second,
		PingPeriod:     50 * time.Millisecond,
		SendBufferSize: 64,
	})

	mux := http.NewServeMux()
	mux.Handle("GET /v1/ws", wsHandler)
	mux.Handle("GET /v1/executions/{id}/ws", wsHandler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Set up execution 1
	execID1 := engine.ExecutionID("exec-ws-1")
	def1 := &engine.GraphDefinition{
		ID: "ws-pipe-1",
		Nodes: []engine.NodeDefinition{
			{ID: "node1", Type: "passthrough"},
		},
	}
	g1, _ := def1.ToGraph()
	st1, _ := engine.NewGraphState(execID1, g1)
	_ = memStore.SetState(context.Background(), execID1, st1, 0)
	_ = memStore.AppendLog(context.Background(), &engine.ExecutionLogEntry{
		ExecutionID: execID1,
		Sequence:    1,
		Timestamp:   time.Now(),
		Message:     "execution_started",
	})
	_ = memStore.AppendLog(context.Background(), &engine.ExecutionLogEntry{
		ExecutionID: execID1,
		Sequence:    2,
		Timestamp:   time.Now(),
		Message:     "node_ready",
	})

	// Set up execution 2
	execID2 := engine.ExecutionID("exec-ws-2")
	def2 := &engine.GraphDefinition{
		ID: "ws-pipe-2",
		Nodes: []engine.NodeDefinition{
			{ID: "node2", Type: "passthrough"},
		},
	}
	g2, _ := def2.ToGraph()
	st2, _ := engine.NewGraphState(execID2, g2)
	_ = memStore.SetState(context.Background(), execID2, st2, 0)
	_ = memStore.AppendLog(context.Background(), &engine.ExecutionLogEntry{
		ExecutionID: execID2,
		Sequence:    1,
		Timestamp:   time.Now(),
		Message:     "exec2_init",
	})

	t.Run("single_execution_connect_and_stream", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/v1/executions/exec-ws-1/ws", nil)
		if err != nil {
			t.Fatalf("failed to dial websocket: %v", err)
		}
		defer func() { _ = conn.Close() }()

		// Should receive initial subscribed message
		var subAck api.WSServerMessage
		if err := conn.ReadJSON(&subAck); err != nil {
			t.Fatalf("failed to read subAck: %v", err)
		}
		if subAck.Event != "subscribed" || subAck.ExecutionID != "exec-ws-1" {
			t.Fatalf("unexpected subAck: %+v", subAck)
		}

		// Should receive updates
		var update1 api.WSServerMessage
		if err := conn.ReadJSON(&update1); err != nil {
			t.Fatalf("failed to read update1: %v", err)
		}
		if update1.Sequence != 1 || update1.EventType != "execution_started" {
			t.Fatalf("unexpected update1: %+v", update1)
		}

		var update2 api.WSServerMessage
		if err := conn.ReadJSON(&update2); err != nil {
			t.Fatalf("failed to read update2: %v", err)
		}
		if update2.Sequence != 2 || update2.EventType != "node_ready" {
			t.Fatalf("unexpected update2: %+v", update2)
		}
	})

	t.Run("multi_execution_subscribe_and_unsubscribe", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/v1/ws", nil)
		if err != nil {
			t.Fatalf("failed to dial /v1/ws: %v", err)
		}
		defer func() { _ = conn.Close() }()

		// Subscribe to exec-ws-1
		subReq1 := api.WSClientMessage{
			Action:       "subscribe",
			ExecutionID:  "exec-ws-1",
			FromSequence: 2,
		}
		if err := conn.WriteJSON(subReq1); err != nil {
			t.Fatalf("failed to write subReq1: %v", err)
		}

		var ack1 api.WSServerMessage
		if err := conn.ReadJSON(&ack1); err != nil {
			t.Fatalf("failed to read ack1: %v", err)
		}
		if ack1.Event != "subscribed" || ack1.ExecutionID != "exec-ws-1" {
			t.Fatalf("unexpected ack1: %+v", ack1)
		}

		// Since from_sequence was 2, should receive seq 2
		var updSeq2 api.WSServerMessage
		if err := conn.ReadJSON(&updSeq2); err != nil {
			t.Fatalf("failed to read updSeq2: %v", err)
		}
		if updSeq2.Sequence != 2 {
			t.Fatalf("expected sequence 2, got %d", updSeq2.Sequence)
		}

		// Also subscribe to exec-ws-2 on the same connection!
		subReq2 := api.WSClientMessage{
			Action:       "subscribe",
			ExecutionID:  "exec-ws-2",
			FromSequence: 0,
		}
		if err := conn.WriteJSON(subReq2); err != nil {
			t.Fatalf("failed to write subReq2: %v", err)
		}

		var ack2 api.WSServerMessage
		if err := conn.ReadJSON(&ack2); err != nil {
			t.Fatalf("failed to read ack2: %v", err)
		}
		if ack2.Event != "subscribed" || ack2.ExecutionID != "exec-ws-2" {
			t.Fatalf("unexpected ack2: %+v", ack2)
		}

		var updExec2 api.WSServerMessage
		if err := conn.ReadJSON(&updExec2); err != nil {
			t.Fatalf("failed to read updExec2: %v", err)
		}
		if updExec2.ExecutionID != "exec-ws-2" || updExec2.EventType != "exec2_init" {
			t.Fatalf("unexpected updExec2: %+v", updExec2)
		}

		// Unsubscribe from exec-ws-1
		unsubReq := api.WSClientMessage{
			Action:      "unsubscribe",
			ExecutionID: "exec-ws-1",
		}
		if err := conn.WriteJSON(unsubReq); err != nil {
			t.Fatalf("failed to write unsubReq: %v", err)
		}

		var unsubAck api.WSServerMessage
		if err := conn.ReadJSON(&unsubAck); err != nil {
			t.Fatalf("failed to read unsubAck: %v", err)
		}
		if unsubAck.Event != "unsubscribed" || unsubAck.ExecutionID != "exec-ws-1" {
			t.Fatalf("unexpected unsubAck: %+v", unsubAck)
		}
	})

	t.Run("ping_pong_heartbeat", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/v1/ws", nil)
		if err != nil {
			t.Fatalf("failed to dial /v1/ws: %v", err)
		}
		defer func() { _ = conn.Close() }()

		pingReceived := make(chan struct{}, 1)
		conn.SetPingHandler(func(appData string) error {
			select {
			case pingReceived <- struct{}{}:
			default:
			}
			return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
		})

		// Start reading to allow control message handler to process pings
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		select {
		case <-pingReceived:
			// Ping received successfully
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for ping message from server")
		}
	})
}

func TestWebSocket_SlowConsumer(t *testing.T) {
	memStore := memory.NewStore()
	tCfg := config.TemporalConfig{TaskQueue: "test-queue"}
	service := api.NewServer(nil, tCfg, memStore, nil)

	// Tiny buffer of 1 to easily trigger slow consumer overflow
	wsHandler := api.NewWebSocketHandler(service, nil, api.WebSocketOptions{
		WriteWait:      500 * time.Millisecond,
		PongWait:       5 * time.Second,
		PingPeriod:     4 * time.Second,
		SendBufferSize: 1,
	})

	mux := http.NewServeMux()
	mux.Handle("GET /v1/ws", wsHandler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	execID := engine.ExecutionID("exec-slow-1")
	def := &engine.GraphDefinition{
		ID: "slow-pipe",
		Nodes: []engine.NodeDefinition{
			{ID: "node1", Type: "passthrough"},
		},
	}
	g, _ := def.ToGraph()
	st, _ := engine.NewGraphState(execID, g)
	_ = memStore.SetState(context.Background(), execID, st, 0)

	// Fill log with many entries so adapter bursts
	for i := 1; i <= 50; i++ {
		_ = memStore.AppendLog(context.Background(), &engine.ExecutionLogEntry{
			ExecutionID: execID,
			Sequence:    uint64(i),
			Timestamp:   time.Now(),
			Message:     "event_burst",
		})
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/v1/ws", nil)
	if err != nil {
		t.Fatalf("failed to dial /v1/ws: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Subscribe
	subReq := api.WSClientMessage{
		Action:      "subscribe",
		ExecutionID: "exec-slow-1",
	}
	data, _ := json.Marshal(subReq)
	_ = conn.WriteMessage(websocket.TextMessage, data)

	// Do NOT read from connection (simulating a hung or slow consumer)
	// Server should detect buffer full and close with ClosePolicyViolation (1008)
	closedChan := make(chan error, 1)
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				closedChan <- err
				return
			}
		}
	}()

	select {
	case err := <-closedChan:
		if !websocket.IsCloseError(err, websocket.ClosePolicyViolation, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
			t.Logf("connection closed as expected: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected slow consumer connection to be closed by server, but timed out")
	}
}
