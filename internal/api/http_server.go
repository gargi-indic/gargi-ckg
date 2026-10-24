package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"

	gargiv1 "github.com/gargi-indic/gargi-flow/pkg/api/v1"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc/status"
)

//go:embed openapi.json
var openAPISpec []byte

// APIError represents the consistent JSON error payload for REST API responses.
//
//nolint:revive // APIError is descriptive and distinct from standard library error
type APIError struct {
	Code    int      `json:"code"`
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
	// Errors lists validation problems as {path, code, message}, sorted. It
	// is set for INVALID_ARGUMENT errors that carry a ValidationIssues
	// detail (SimulationService model and experiment validation).
	Errors []engine.ValidationIssue `json:"errors,omitempty"`
}

// CustomErrorHandler transforms gRPC and domain errors into consistent JSON APIError responses.
func CustomErrorHandler(
	_ context.Context,
	_ *runtime.ServeMux,
	_ runtime.Marshaler,
	w http.ResponseWriter,
	_ *http.Request,
	err error,
) {
	grpcErr := ToGRPCError(err)
	st := status.Convert(grpcErr)
	httpStatus := runtime.HTTPStatusFromCode(st.Code())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)

	apiErr := APIError{
		Code:    httpStatus,
		Status:  st.Code().String(),
		Message: st.Message(),
	}

	for _, d := range st.Details() {
		if issues, ok := d.(*gargiv1.ValidationIssues); ok {
			for _, is := range issues.GetErrors() {
				apiErr.Errors = append(apiErr.Errors, engine.ValidationIssue{Path: is.GetPath(), Code: engine.ErrorCode(is.GetCode()), Message: is.GetMessage()})
			}
			continue
		}
		apiErr.Details = append(apiErr.Details, fmt.Sprintf("%v", d))
	}

	_ = json.NewEncoder(w).Encode(apiErr)
}

// HTTPServer exposes the REST API, OpenAPI specifications, and health endpoints.
type HTTPServer struct {
	server *http.Server
	mux    *http.ServeMux
	logger *slog.Logger
}

// HTTPOption configures optional services of NewHTTPServer.
type HTTPOption func(*httpOptions)

type httpOptions struct {
	simulation gargiv1.SimulationServiceServer
}

// WithSimulationService exposes svc over REST: the SimulationService
// gateway routes (/v1/models..., /v1/experiments...) and the experiment
// progress SSE endpoint GET /v1/experiments/{id}/events.
func WithSimulationService(svc gargiv1.SimulationServiceServer) HTTPOption {
	return func(o *httpOptions) { o.simulation = svc }
}

// NewHTTPServer initializes the REST API gateway and HTTP router.
func NewHTTPServer(
	ctx context.Context,
	service gargiv1.GargiServiceServer,
	addr string,
	allowedOrigins []string,
	logger *slog.Logger,
	opts ...HTTPOption,
) (*HTTPServer, error) {
	if logger == nil {
		logger = slog.Default()
	}
	var options httpOptions
	for _, opt := range opts {
		opt(&options)
	}

	gwMux := runtime.NewServeMux(
		runtime.WithErrorHandler(CustomErrorHandler),
	)

	if err := gargiv1.RegisterGargiServiceHandlerServer(ctx, gwMux, service); err != nil {
		return nil, fmt.Errorf("registering grpc-gateway handler: %w", err)
	}
	if options.simulation != nil {
		if err := gargiv1.RegisterSimulationServiceHandlerServer(ctx, gwMux, options.simulation); err != nil {
			return nil, fmt.Errorf("registering simulation grpc-gateway handler: %w", err)
		}
	}

	rootMux := http.NewServeMux()

	// OpenAPI specifications
	serveOpenAPI := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAPISpec)
	}
	rootMux.HandleFunc("GET /openapi.json", serveOpenAPI)
	rootMux.HandleFunc("GET /swagger.json", serveOpenAPI)
	rootMux.HandleFunc("GET /v1/openapi.json", serveOpenAPI)

	// Liveness and readiness probes
	rootMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if srv, ok := service.(*Server); ok {
			status := srv.CheckLiveness(r.Context())
			_ = json.NewEncoder(w).Encode(status)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"SERVING"}`)
	})
	rootMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if srv, ok := service.(*Server); ok {
			status, ready := srv.CheckReadiness(r.Context())
			if !ready {
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				w.WriteHeader(http.StatusOK)
			}
			_ = json.NewEncoder(w).Encode(status)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"READY"}`)
	})

	// Prometheus metrics endpoint
	rootMux.Handle("GET /metrics", promhttp.Handler())

	// Server-Sent Events (SSE) streaming endpoint
	sseHandler := NewSSEHandler(service, logger)
	rootMux.Handle("GET /v1/executions/{id}/events", sseHandler)
	if options.simulation != nil {
		rootMux.Handle("GET /v1/experiments/{id}/events", NewExperimentSSEHandler(options.simulation, logger))
	}

	// WebSocket streaming endpoints
	wsHandler := NewWebSocketHandler(service, logger)
	rootMux.Handle("GET /v1/ws", wsHandler)
	rootMux.Handle("GET /v1/executions/{id}/ws", wsHandler)

	// Mount gRPC-Gateway router
	rootMux.Handle("/", gwMux)

	var handler http.Handler = rootMux
	if len(allowedOrigins) > 0 {
		handler = CORSMiddleware(allowedOrigins, handler)
	}

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	return &HTTPServer{
		server: httpSrv,
		mux:    rootMux,
		logger: logger,
	}, nil
}

// Handler returns the underlying http.Handler.
func (s *HTTPServer) Handler() http.Handler {
	return s.server.Handler
}

// Serve begins serving HTTP requests on the provided net.Listener.
func (s *HTTPServer) Serve(lis net.Listener) error {
	s.logger.Info("Starting REST API HTTP server", "addr", lis.Addr().String())
	if err := s.server.Serve(lis); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server serve: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.logger.Info("Gracefully shutting down REST API HTTP server")
	return s.server.Shutdown(ctx)
}
