package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/api"
	"github.com/gargi-indic/gargi-flow/internal/config"
	"github.com/gargi-indic/gargi-flow/internal/store/memory"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

func setupTestHTTPServer(t *testing.T, st engine.StateStore) *httptest.Server {
	t.Helper()

	tCfg := config.TemporalConfig{TaskQueue: "test-queue"}
	service := api.NewServer(nil, tCfg, st, nil)

	httpServer, err := api.NewHTTPServer(context.Background(), service, ":0", nil)
	if err != nil {
		t.Fatalf("failed to create http server: %v", err)
	}

	ts := httptest.NewServer(httpServer.Handler())
	t.Cleanup(ts.Close)

	return ts
}

func TestHTTP_OpenAPIAndHealth(t *testing.T) {
	memStore := memory.NewStore()
	ts := setupTestHTTPServer(t, memStore)

	t.Run("GET /openapi.json", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/openapi.json")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var spec map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
			t.Fatalf("failed to parse openapi json: %v", err)
		}
		if _, ok := spec["swagger"]; !ok && spec["openapi"] == nil {
			t.Errorf("expected swagger or openapi field in spec: %v", spec)
		}
	})

	t.Run("GET /swagger.json", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/swagger.json")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GET /healthz", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/healthz")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "SERVING") {
			t.Errorf("unexpected body: %s", string(body))
		}
	})

	t.Run("GET /readyz", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/readyz")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "READY") {
			t.Errorf("unexpected body: %s", string(body))
		}
	})

	t.Run("GET /metrics", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/metrics")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "gargi_") && !strings.Contains(string(body), "promhttp_") {
			t.Errorf("expected prometheus metrics in response, got: %s", string(body))
		}
	})
}

func TestHTTP_RESTEndpoints(t *testing.T) {
	memStore := memory.NewStore()
	ts := setupTestHTTPServer(t, memStore)

	t.Run("POST /v1/executions success", func(t *testing.T) {
		reqBody := map[string]any{
			"graphDefinition": validJSONGraph,
			"executionId":     "http-exec-1",
		}
		reqBytes, _ := json.Marshal(reqBody)

		resp, err := http.Post(ts.URL+"/v1/executions", "application/json", bytes.NewReader(reqBytes))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}

		var res map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if res["executionId"] != "http-exec-1" {
			t.Errorf("expected executionId 'http-exec-1', got %v", res["executionId"])
		}
	})

	t.Run("GET /v1/executions/{id} success", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/executions/http-exec-1")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}

		var res map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if res["executionId"] != "http-exec-1" {
			t.Errorf("expected executionId 'http-exec-1', got %v", res["executionId"])
		}
	})

	t.Run("GET /v1/executions/{id} not found consistent error format", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/executions/nonexistent-id")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}

		var apiErr api.APIError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("decode APIError failed: %v", err)
		}
		if apiErr.Code != 404 {
			t.Errorf("expected error code 404, got %d", apiErr.Code)
		}
		if apiErr.Status != "NotFound" {
			t.Errorf("expected status 'NotFound', got %s", apiErr.Status)
		}
		if apiErr.Message == "" {
			t.Errorf("expected non-empty error message")
		}
	})

	t.Run("GET /v1/executions/{id}/nodes/{node_id} success", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/executions/http-exec-1/nodes/nodeA")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("POST /v1/executions/{id}/cancel", func(t *testing.T) {
		reqBody := map[string]any{"reason": "user aborted"}
		reqBytes, _ := json.Marshal(reqBody)

		resp, err := http.Post(ts.URL+"/v1/executions/http-exec-1/cancel", "application/json", bytes.NewReader(reqBytes))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("POST /v1/executions/{id}/nodes/{node_id}/signal", func(t *testing.T) {
		reqBody := map[string]any{
			"signalName": "resume",
			"payload":    "val1",
		}
		reqBytes, _ := json.Marshal(reqBody)

		resp, err := http.Post(ts.URL+"/v1/executions/http-exec-1/nodes/nodeA/signal", "application/json", bytes.NewReader(reqBytes))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("POST /v1/executions sync mode with wait and timeout", func(t *testing.T) {
		reqBody := map[string]any{
			"graphDefinition": validJSONGraph,
			"executionId":     "http-sync-exec-1",
			"wait":            true,
			"timeout":         "0.05s",
		}
		reqBytes, _ := json.Marshal(reqBody)

		resp, err := http.Post(ts.URL+"/v1/executions", "application/json", bytes.NewReader(reqBytes))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// Expect 504 Deadline Exceeded or 408 Timeout mapped by CustomErrorHandler
		if resp.StatusCode != http.StatusGatewayTimeout && resp.StatusCode != http.StatusRequestTimeout {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 504 or 408 for timeout, got %d: %s", resp.StatusCode, string(body))
		}
	})
}
