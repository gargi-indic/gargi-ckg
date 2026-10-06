package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

// httpCallConfig holds the configuration for making an HTTP request.
// Security note (SSRF): The URL is user-supplied configuration. The engine performs no
// allow-list or deny-list logic on the target host or network. Callers and integrators
// must restrict who can define graphs to prevent Server-Side Request Forgery (SSRF)
// or implement a custom HTTPClient that enforces network policies.
type httpCallConfig struct {
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	Body           any               `json:"body"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}

func (c *httpCallConfig) Validate() error {
	u, err := url.Parse(c.URL)
	if err != nil {
		return errors.New("invalid url")
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("url scheme must be http or https")
	}

	if u.Host == "" {
		return errors.New("url host cannot be empty")
	}

	if u.User != nil {
		return errors.New("url cannot contain userinfo")
	}

	if c.Method == "" {
		c.Method = "GET"
	}
	switch c.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		// valid
	default:
		return errors.New("invalid method: must be one of GET, POST, PUT, PATCH, DELETE")
	}

	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 30
	} else if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 300 {
		return errors.New("timeout_seconds must be between 1 and 300")
	}

	return nil
}

type httpCallNode struct {
	*engine.BaseNode
	cfg    httpCallConfig
	client HTTPDoer
}

var _ engine.NodeInterface = (*httpCallNode)(nil)

func (n *httpCallNode) Execute(ctx context.Context, nc engine.NodeContext) (*engine.StateMutation, error) {
	method := n.cfg.Method
	if method == "" {
		method = "GET"
	}

	timeoutSecs := n.cfg.TimeoutSeconds
	if timeoutSecs == 0 {
		timeoutSecs = 30
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	var bodyReader io.Reader
	contentType := ""

	if n.cfg.Body != nil {
		bodyBytes, err := json.Marshal(n.cfg.Body)
		if err != nil {
			return nil, engine.NewNonRetryableError(errors.New("failed to marshal body as JSON"))
		}
		bodyReader = bytes.NewReader(bodyBytes)
		contentType = "application/json"
	}

	req, err := http.NewRequestWithContext(reqCtx, method, n.cfg.URL, bodyReader)
	if err != nil {
		return nil, engine.NewNonRetryableError(errors.New("failed to create request"))
	}

	// Set headers
	hasContentType := false
	for k, v := range n.cfg.Headers {
		req.Header.Set(k, v)
		if strings.ToLower(k) == "content-type" {
			hasContentType = true
		}
	}

	if contentType != "" && !hasContentType {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := n.client.Do(req)

	// Prepare safe URL for error messages (strip query params and userinfo)
	safeURL := *req.URL
	safeURL.RawQuery = ""
	safeURL.User = nil
	safeURLStr := safeURL.String()

	if err != nil {
		// Parent context cancelled?
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err() // return unwrapped
		}
		// Request context deadline exceeded? (node's own timeout)
		if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			return nil, engine.NewRetryableError(fmt.Errorf("request timeout for %s", safeURLStr))
		}
		// Network/Transport error
		// err from http.Client.Do is often an *url.Error which includes the full URL (and query).
		// We explicitly do NOT include the original err message to prevent leaking headers/query.
		return nil, engine.NewRetryableError(fmt.Errorf("network error for %s", safeURLStr))
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Limit body to 1 MiB + 1 byte
	const maxBodySize = 1024 * 1024
	lr := io.LimitReader(resp.Body, maxBodySize+1)
	respBodyBytes, err := io.ReadAll(lr)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			return nil, engine.NewRetryableError(fmt.Errorf("read timeout for %s", safeURLStr))
		}
		return nil, engine.NewRetryableError(fmt.Errorf("failed to read response body for %s", safeURLStr))
	}

	if len(respBodyBytes) > maxBodySize {
		return nil, engine.NewNonRetryableError(fmt.Errorf("response body too large from %s", safeURLStr))
	}

	// Check status codes for classification
	var nodeErr error
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooEarly || resp.StatusCode == http.StatusTooManyRequests {
		nodeErr = engine.NewRetryableError(fmt.Errorf("retryable http status %d from %s", resp.StatusCode, safeURLStr))
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Includes un-followed 3xx and other 4xx
		nodeErr = engine.NewNonRetryableError(fmt.Errorf("non-retryable http status %d from %s", resp.StatusCode, safeURLStr))
	}

	if nodeErr != nil {
		return nil, nodeErr
	}

	var finalBody any = string(respBodyBytes)
	respContentType := resp.Header.Get("Content-Type")

	if strings.Contains(respContentType, "application/json") && len(respBodyBytes) > 0 {
		var jsBody any
		if err := json.Unmarshal(respBodyBytes, &jsBody); err == nil {
			finalBody = jsBody
		}
	}

	// We pass 0 for ExpectedVersion as the engine is responsible for stamping it.
	mut := engine.NewStateMutation(nc.NodeID(), 0, nil, map[string]any{
		"status_code":  resp.StatusCode,
		"body":         finalBody,
		"content_type": respContentType,
	}, nil, nil)

	return &mut, nil
}
