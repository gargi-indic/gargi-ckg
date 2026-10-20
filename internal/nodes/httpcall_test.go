package nodes_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

func TestHttpCallNode(t *testing.T) {
	// Let's test execution directly with our fake HTTP doer
	fakeDoer := &fakeHTTPDoer{}
	deps := nodes.Dependencies{
		HTTPClient: fakeDoer,
	}
	reg := engine.NewRegistry()
	_ = nodes.RegisterDefaults(reg, deps)

	t.Run("security: query string and headers not leaked on error", func(t *testing.T) {
		fakeDoer.doFunc = func(_ *http.Request) (*http.Response, error) {
			return nil, errors.New("underlying network error with SECRET_TOKEN=abc")
		}

		node, _ := reg.Instantiate("http_call", map[string]any{
			"url": "http://example.com/api?sensitive_query=yes",
			"headers": map[string]string{
				"Authorization": "Bearer my_super_secret_token",
			},
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		_, err := node.Execute(context.Background(), nc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		errMsg := err.Error()
		if strings.Contains(errMsg, "sensitive_query") {
			t.Errorf("error leaked query string: %v", errMsg)
		}
		if strings.Contains(errMsg, "SECRET_TOKEN") || strings.Contains(errMsg, "my_super_secret_token") {
			t.Errorf("error leaked internal error detail or header: %v", errMsg)
		}
		if !strings.Contains(errMsg, "http://example.com/api") {
			t.Errorf("expected URL base in error, got: %v", errMsg)
		}
	})

	t.Run("timeout (retryable)", func(t *testing.T) {
		fakeDoer.doFunc = func(req *http.Request) (*http.Response, error) {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(10 * time.Millisecond):
				return nil, errors.New("should have timed out")
			}
		}

		node, _ := reg.Instantiate("http_call", map[string]any{
			"url":             "http://example.com/api",
			"timeout_seconds": float64(1), // use 1s but fake handles it
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		_, err := node.Execute(ctx, nc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if engine.ClassOf(err) != engine.ClassRetryable {
			t.Errorf("expected retryable error, got %v", engine.ClassOf(err))
		}
		if !strings.Contains(err.Error(), "request timeout") {
			t.Errorf("expected request timeout message, got %v", err)
		}
	})

	t.Run("parent ctx cancelled returns ctx.Err() unwrapped", func(t *testing.T) {
		fakeDoer.doFunc = func(_ *http.Request) (*http.Response, error) {
			return nil, context.Canceled
		}

		node, _ := reg.Instantiate("http_call", map[string]any{
			"url": "http://example.com/api",
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := node.Execute(ctx, nc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled unwrapped error, got %v", err)
		}
	})

	t.Run("unfollowed 3xx (non-retryable)", func(t *testing.T) {
		fakeDoer.doFunc = func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusFound,
				Body:       io.NopCloser(strings.NewReader("redirecting")),
				Header:     make(http.Header),
			}, nil
		}

		node, _ := reg.Instantiate("http_call", map[string]any{
			"url": "http://example.com/api",
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		_, err := node.Execute(context.Background(), nc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if engine.ClassOf(err) != engine.ClassNonRetryable {
			t.Errorf("expected non-retryable error, got %v", engine.ClassOf(err))
		}
	})

	t.Run("successful request with JSON response", func(t *testing.T) {
		fakeDoer.doFunc = func(_ *http.Request) (*http.Response, error) {
			hdr := make(http.Header)
			hdr.Set("Content-Type", "application/json")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status": "ok"}`)),
				Header:     hdr,
			}, nil
		}

		node, _ := reg.Instantiate("http_call", map[string]any{
			"url": "http://example.com/api?secret=abc",
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		mut, err := node.Execute(context.Background(), nc)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if mut.Set["status_code"] != 200 {
			t.Errorf("expected status 200, got %v", mut.Set["status_code"])
		}

		bodyMap, ok := mut.Set["body"].(map[string]any)
		if !ok || bodyMap["status"] != "ok" {
			t.Errorf("expected parsed json body, got %v", mut.Set["body"])
		}
	})

	t.Run("body too large (non-retryable)", func(t *testing.T) {
		fakeDoer.doFunc = func(_ *http.Request) (*http.Response, error) {
			// Return a body that is 1MB + 2 bytes
			largeBody := make([]byte, 1024*1024+2)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(largeBody)),
				Header:     make(http.Header),
			}, nil
		}

		node, _ := reg.Instantiate("http_call", map[string]any{
			"url": "http://example.com/api",
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		_, err := node.Execute(context.Background(), nc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if engine.ClassOf(err) != engine.ClassNonRetryable {
			t.Errorf("expected non-retryable error, got %v", engine.ClassOf(err))
		}
	})

	t.Run("5xx (retryable)", func(t *testing.T) {
		fakeDoer.doFunc = func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("error")),
				Header:     make(http.Header),
			}, nil
		}

		node, _ := reg.Instantiate("http_call", map[string]any{
			"url": "http://example.com/api",
		})

		g, _ := engine.NewGraph("test-graph", nil, nil)
		state, _ := engine.NewGraphState("exec1", g)

		nc := &fakeNodeContext{
			nodeID: "node1",
			state:  state,
		}

		_, err := node.Execute(context.Background(), nc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if engine.ClassOf(err) != engine.ClassRetryable {
			t.Errorf("expected retryable error, got %v", engine.ClassOf(err))
		}
	})
}
