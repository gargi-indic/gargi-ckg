package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/metrics"
	gargiv1 "github.com/gargi-indic/gargi-flow/pkg/api/v1"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// ExperimentSSEHandler streams experiment progress over Server-Sent Events
// at GET /v1/experiments/{id}/events, with the conventions of SSEHandler:
// "id: <sequence>", "event: progress|done" and "data: <protojson of
// StreamExperimentProgressResponse>" frames, ": heartbeat" comments, and the
// response ends after the "done" event. Errors raised before the first
// event (for example an unknown experiment) are returned as the usual JSON
// error body with the matching HTTP status.
type ExperimentSSEHandler struct {
	service gargiv1.SimulationServiceServer
	logger  *slog.Logger
	opts    SSEOptions
}

// NewExperimentSSEHandler returns an ExperimentSSEHandler over service.
func NewExperimentSSEHandler(service gargiv1.SimulationServiceServer, logger *slog.Logger, opts ...SSEOptions) *ExperimentSSEHandler {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := DefaultSSEOptions()
	if len(opts) > 0 && opts[0].HeartbeatInterval > 0 {
		cfg.HeartbeatInterval = opts[0].HeartbeatInterval
	}
	return &ExperimentSSEHandler{service: service, logger: logger, opts: cfg}
}

// experimentSSEStream adapts an HTTP response to the server stream of
// StreamExperimentProgress. Response headers are committed by the first
// event or heartbeat.
type experimentSSEStream struct {
	ctx       context.Context
	mu        sync.Mutex
	w         http.ResponseWriter
	flusher   http.Flusher
	committed bool
}

// commit writes the SSE response headers once; callers hold mu.
func (a *experimentSSEStream) commit() {
	if a.committed {
		return
	}
	a.committed = true
	h := a.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	a.w.WriteHeader(http.StatusOK)
}

func (a *experimentSSEStream) Send(resp *gargiv1.StreamExperimentProgressResponse) error {
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(resp)
	if err != nil {
		return err
	}
	eventType := resp.GetEventType()
	if eventType == "" {
		eventType = "message"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.commit()
	if _, err := fmt.Fprintf(a.w, "id: %d\nevent: %s\ndata: %s\n\n", resp.GetSequence(), eventType, data); err != nil {
		return err
	}
	a.flusher.Flush()
	return nil
}

func (a *experimentSSEStream) writeHeartbeat() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.commit()
	if _, err := fmt.Fprint(a.w, ": heartbeat\n\n"); err == nil {
		a.flusher.Flush()
	}
}

// writeError writes err as the JSON error body when no event was sent yet,
// and reports whether it did.
func (a *experimentSSEStream) writeError(err error) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.committed {
		return false
	}
	a.committed = true
	st := status.Convert(ToGRPCError(err))
	httpStatus := runtime.HTTPStatusFromCode(st.Code())
	a.w.Header().Set("Content-Type", "application/json")
	a.w.WriteHeader(httpStatus)
	_ = json.NewEncoder(a.w).Encode(APIError{Code: httpStatus, Status: st.Code().String(), Message: st.Message()})
	return true
}

func (a *experimentSSEStream) SetHeader(metadata.MD) error  { return nil }
func (a *experimentSSEStream) SendHeader(metadata.MD) error { return nil }
func (a *experimentSSEStream) SetTrailer(metadata.MD)       {}
func (a *experimentSSEStream) Context() context.Context     { return a.ctx }
func (a *experimentSSEStream) SendMsg(any) error            { return nil }
func (a *experimentSSEStream) RecvMsg(any) error            { return nil }

// ServeHTTP handles GET /v1/experiments/{id}/events.
func (h *ExperimentSSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported by underlying connection", http.StatusInternalServerError)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"code":400,"status":"InvalidArgument","message":"experiment ID is required"}`, http.StatusBadRequest)
		return
	}

	metrics.RecordStreamConnected("sse")
	defer metrics.RecordStreamDisconnected("sse")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stream := &experimentSSEStream{ctx: ctx, w: w, flusher: flusher}

	heartbeat := time.NewTicker(h.opts.HeartbeatInterval)
	defer heartbeat.Stop()
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeat.C:
				stream.writeHeartbeat()
			}
		}
	})

	err := h.service.StreamExperimentProgress(&gargiv1.StreamExperimentProgressRequest{Id: id}, stream)
	cancel()
	wg.Wait()
	if err != nil && !stream.writeError(err) {
		h.logger.Debug("experiment SSE stream ended", "experiment_id", id, "error", err)
	}
}
