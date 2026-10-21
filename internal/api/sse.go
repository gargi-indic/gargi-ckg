package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/metrics"
	gargiv1 "github.com/gargi-indic/gargi-flow/pkg/api/v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
)

// SSEOptions configures the behavior of the Server-Sent Events handler.
type SSEOptions struct {
	HeartbeatInterval time.Duration
}

// DefaultSSEOptions returns standard defaults for SSE.
func DefaultSSEOptions() SSEOptions {
	return SSEOptions{
		HeartbeatInterval: 15 * time.Second,
	}
}

// SSEHandler streams execution updates over Server-Sent Events (SSE).
type SSEHandler struct {
	service gargiv1.GargiServiceServer
	logger  *slog.Logger
	opts    SSEOptions
}

// NewSSEHandler creates a new SSEHandler.
func NewSSEHandler(service gargiv1.GargiServiceServer, logger *slog.Logger, opts ...SSEOptions) *SSEHandler {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := DefaultSSEOptions()
	if len(opts) > 0 && opts[0].HeartbeatInterval > 0 {
		cfg.HeartbeatInterval = opts[0].HeartbeatInterval
	}
	return &SSEHandler{
		service: service,
		logger:  logger,
		opts:    cfg,
	}
}

type sseStreamAdapter struct {
	ctx     context.Context
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
}

func (a *sseStreamAdapter) Send(resp *gargiv1.StreamExecutionUpdatesResponse) error {
	opts := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}
	data, err := opts.Marshal(resp)
	if err != nil {
		return err
	}

	eventType := resp.GetEventType()
	if eventType == "" {
		eventType = "message"
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	_, err = fmt.Fprintf(a.w, "id: %d\nevent: %s\ndata: %s\n\n", resp.GetSequence(), eventType, string(data))
	if err != nil {
		return err
	}
	a.flusher.Flush()
	return nil
}

func (a *sseStreamAdapter) writeHeartbeat() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := fmt.Fprintf(a.w, ": heartbeat\n\n")
	if err != nil {
		return err
	}
	a.flusher.Flush()
	return nil
}

func (a *sseStreamAdapter) SetHeader(metadata.MD) error  { return nil }
func (a *sseStreamAdapter) SendHeader(metadata.MD) error { return nil }
func (a *sseStreamAdapter) SetTrailer(metadata.MD)       {}
func (a *sseStreamAdapter) Context() context.Context     { return a.ctx }
func (a *sseStreamAdapter) SendMsg(_ any) error          { return nil }
func (a *sseStreamAdapter) RecvMsg(_ any) error          { return nil }

// ServeHTTP handles GET /v1/executions/{id}/events as text/event-stream.
func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported by underlying connection", http.StatusInternalServerError)
		return
	}

	execID := r.PathValue("id")
	if execID == "" {
		// Fallback for custom routing
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 && parts[0] == "v1" && parts[1] == "executions" {
			execID = parts[2]
		}
	}
	if execID == "" {
		http.Error(w, `{"code":400,"status":"InvalidArgument","message":"execution ID is required"}`, http.StatusBadRequest)
		return
	}

	// Resume via Last-Event-ID header or lastEventId query parameter
	var fromSequence uint64
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = r.URL.Query().Get("lastEventId")
	}
	if lastEventID != "" {
		if parsed, err := strconv.ParseUint(lastEventID, 10, 64); err == nil {
			fromSequence = parsed
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	metrics.RecordStreamConnected("sse")
	defer metrics.RecordStreamDisconnected("sse")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	adapter := &sseStreamAdapter{
		ctx:     ctx,
		w:       w,
		flusher: flusher,
	}

	// Heartbeat ticker to keep connections alive through proxies and firewalls
	heartbeatTicker := time.NewTicker(h.opts.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeatTicker.C:
				_ = adapter.writeHeartbeat()
			}
		}
	}()

	req := &gargiv1.StreamExecutionUpdatesRequest{
		ExecutionId:  execID,
		FromSequence: fromSequence,
	}

	if err := h.service.StreamExecutionUpdates(req, adapter); err != nil {
		h.logger.Debug("SSE stream completed or cancelled", "execution_id", execID, "error", err)
	}

	cancel()
	wg.Wait()
}
