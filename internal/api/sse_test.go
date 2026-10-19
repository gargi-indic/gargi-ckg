package api_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/api"
	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/gargi-indic/gargi-ckg/internal/store/memory"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func TestSSE_Streaming(t *testing.T) {
	memStore := memory.NewStore()
	tCfg := config.TemporalConfig{TaskQueue: "test-queue"}
	service := api.NewServer(nil, tCfg, memStore, nil)

	// Custom short heartbeat for testing
	sseHandler := api.NewSSEHandler(service, nil, api.SSEOptions{
		HeartbeatInterval: 40 * time.Millisecond,
	})

	mux := http.NewServeMux()
	mux.Handle("GET /v1/executions/{id}/events", sseHandler)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	execID := engine.ExecutionID("exec-sse-1")
	def := &engine.GraphDefinition{
		ID: "sse-pipeline",
		Nodes: []engine.NodeDefinition{
			{ID: "node1", Type: "passthrough"},
		},
	}
	g, err := def.ToGraph()
	if err != nil {
		t.Fatalf("failed to convert to graph: %v", err)
	}
	st, err := engine.NewGraphState(execID, g)
	if err != nil {
		t.Fatalf("failed to create graph state: %v", err)
	}
	_ = memStore.SetState(context.Background(), execID, st, 0)

	// Append log entry sequence 1
	_ = memStore.AppendLog(context.Background(), &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    1,
		Timestamp:   time.Now(),
		Message:     "execution_started",
	})
	// Append log entry sequence 2
	_ = memStore.AppendLog(context.Background(), &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    2,
		Timestamp:   time.Now(),
		Message:     "execution_initializing",
	})

	t.Run("stream events from sequence 1", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/executions/exec-sse-1/events", nil)
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Errorf("expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
		}

		reader := bufio.NewReader(resp.Body)
		var receivedLines []string

		// Read first 10 lines
		for len(receivedLines) < 8 {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			receivedLines = append(receivedLines, strings.TrimSpace(line))
		}

		joined := strings.Join(receivedLines, " ")
		if !strings.Contains(joined, "id: 1") {
			t.Errorf("expected id: 1 in stream, got %v", receivedLines)
		}
		if !strings.Contains(joined, "execution_started") {
			t.Errorf("expected execution_started in stream, got %v", receivedLines)
		}
	})

	t.Run("resume via Last-Event-ID header", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/executions/exec-sse-1/events", nil)
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		req.Header.Set("Last-Event-ID", "2")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		reader := bufio.NewReader(resp.Body)
		var receivedLines []string

		for len(receivedLines) < 4 {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			receivedLines = append(receivedLines, strings.TrimSpace(line))
		}

		joined := strings.Join(receivedLines, " ")
		if !strings.Contains(joined, "id: 2") {
			t.Errorf("expected id: 2 in resumed stream, got %v", receivedLines)
		}
		if strings.Contains(joined, "execution_started") {
			t.Errorf("did not expect event 1 (execution_started) when resuming from event 2")
		}
	})

	t.Run("resume via lastEventId query parameter", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/executions/exec-sse-1/events?lastEventId=2", nil)
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		reader := bufio.NewReader(resp.Body)
		var receivedLines []string

		for len(receivedLines) < 4 {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			receivedLines = append(receivedLines, strings.TrimSpace(line))
		}

		joined := strings.Join(receivedLines, " ")
		if !strings.Contains(joined, "id: 2") {
			t.Errorf("expected id: 2 in query-resumed stream, got %v", receivedLines)
		}
	})

	t.Run("heartbeat emission", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		// Request starting from high sequence so no logs are emitted immediately
		req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/executions/exec-sse-1/events?lastEventId=999", nil)
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		reader := bufio.NewReader(resp.Body)
		foundHeartbeat := false

		for i := 0; i < 10; i++ {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			if strings.TrimSpace(line) == ": heartbeat" {
				foundHeartbeat = true
				break
			}
		}

		if !foundHeartbeat {
			t.Errorf("expected heartbeat comment ': heartbeat' within 1s")
		}
	})
}
