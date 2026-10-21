package e2e_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/api"
	"github.com/gargi-indic/gargi-flow/internal/store/memory"
	gargiv1 "github.com/gargi-indic/gargi-flow/pkg/api/v1"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGargiService struct {
	gargiv1.UnimplementedGargiServiceServer
	bridge *api.FanoutBridge
}

func (m *mockGargiService) StreamExecutionUpdates(req *gargiv1.StreamExecutionUpdatesRequest, server gargiv1.GargiService_StreamExecutionUpdatesServer) error {
	return m.bridge.Stream(server.Context(), req, server)
}

func publishTestEvent(t *testing.T, st *memory.Store, execID engine.ExecutionID, seq uint64, eventType engine.EventType, nodeID engine.NodeID, payload any) {
	ev, err := engine.NewEvent(execID, seq, time.Now().UTC(), eventType, nodeID, payload)
	require.NoError(t, err)
	err = st.PublishEvent(context.Background(), ev)
	require.NoError(t, err)
}

func TestE2E_FanoutFanin_SSE_Streaming(t *testing.T) {
	st := memory.NewStore()

	// Create API Bridge
	bridge := api.NewFanoutBridge(st, nil)
	mockServer := &mockGargiService{bridge: bridge}

	sseHandler := api.NewSSEHandler(mockServer, nil)
	mux := http.NewServeMux()
	mux.Handle("GET /v1/executions/{id}/events", sseHandler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	execID := engine.ExecutionID("exec-fanout-sse")

	// Start SSE connection
	req, _ := http.NewRequest("GET", ts.URL+"/v1/executions/"+string(execID)+"/events", nil)
	client := ts.Client()

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	eventsCh := make(chan map[string]any, 100)

	go func() {
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			if strings.HasPrefix(line, "data: ") {
				var event map[string]any
				err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event)
				if err != nil {
					continue
				}
				eventsCh <- event
				if event["eventType"] == "graph_completed" || event["eventType"] == "graph_failed" {
					break
				}
			}
		}
		close(eventsCh)
	}()

	time.Sleep(100 * time.Millisecond) // Let subscriber connect

	// Publish node transition events for Fanout-Fanin
	// Source: PENDING -> RUNNING -> SUCCEEDED
	publishTestEvent(t, st, execID, 1, engine.EventTypeNodeStateChanged, "source", engine.NodeStateChangedPayload{
		OldStatus: engine.StatusPending,
		NewStatus: engine.StatusRunning,
	})
	publishTestEvent(t, st, execID, 2, engine.EventTypeNodeCompleted, "source", engine.NodeCompletedPayload{
		Status: engine.StatusSucceeded,
	})

	// Worker A: PENDING -> RUNNING -> SUCCEEDED
	publishTestEvent(t, st, execID, 3, engine.EventTypeNodeStateChanged, "worker_a", engine.NodeStateChangedPayload{
		OldStatus: engine.StatusPending,
		NewStatus: engine.StatusRunning,
	})
	publishTestEvent(t, st, execID, 4, engine.EventTypeNodeCompleted, "worker_a", engine.NodeCompletedPayload{
		Status: engine.StatusSucceeded,
	})

	// Worker B: PENDING -> RUNNING -> SUCCEEDED
	publishTestEvent(t, st, execID, 5, engine.EventTypeNodeStateChanged, "worker_b", engine.NodeStateChangedPayload{
		OldStatus: engine.StatusPending,
		NewStatus: engine.StatusRunning,
	})
	publishTestEvent(t, st, execID, 6, engine.EventTypeNodeCompleted, "worker_b", engine.NodeCompletedPayload{
		Status: engine.StatusSucceeded,
	})

	// Aggregator: PENDING -> RUNNING -> SUCCEEDED
	publishTestEvent(t, st, execID, 7, engine.EventTypeNodeStateChanged, "aggregator", engine.NodeStateChangedPayload{
		OldStatus: engine.StatusPending,
		NewStatus: engine.StatusRunning,
	})
	publishTestEvent(t, st, execID, 8, engine.EventTypeNodeCompleted, "aggregator", engine.NodeCompletedPayload{
		Status: engine.StatusSucceeded,
	})

	// Graph Completed
	publishTestEvent(t, st, execID, 9, engine.EventTypeGraphCompleted, "", engine.GraphCompletedPayload{
		FinalStatus: engine.StatusSucceeded,
	})

	// Collect events
	events := []map[string]any{}
	timer := time.NewTimer(5 * time.Second)

collect:
	for {
		select {
		case ev, ok := <-eventsCh:
			if !ok {
				break collect
			}
			events = append(events, ev)
		case <-timer.C:
			t.Fatal("Timeout waiting for events")
		}
	}

	// Verify terminal states
	terminalNodes := map[string]bool{}
	graphCompleted := false

	for _, e := range events {
		if e["event_type"] == "node_state_changed" {
			if e["to_status"] == "NODE_STATUS_SUCCEEDED" {
				terminalNodes[e["node_id"].(string)] = true
				// Assert we had FromStatus
				assert.Equal(t, "NODE_STATUS_RUNNING", e["from_status"], "Missing or incorrect fromStatus")
			}
		}
		if e["event_type"] == "graph_completed" {
			graphCompleted = true
		}
	}

	assert.True(t, terminalNodes["source"], "source node missing SUCCEEDED")
	assert.True(t, terminalNodes["worker_a"], "worker_a node missing SUCCEEDED")
	assert.True(t, terminalNodes["worker_b"], "worker_b node missing SUCCEEDED")
	assert.True(t, terminalNodes["aggregator"], "aggregator node missing SUCCEEDED")
	assert.True(t, graphCompleted, "missing graph_completed event")
}
