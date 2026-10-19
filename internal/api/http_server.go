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

	gargiv1 "github.com/gargi-indic/gargi-ckg/pkg/api/v1"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc/status"
)

//go:embed openapi.json
var openAPISpec []byte

// APIError represents the consistent JSON error payload for REST API responses.
type APIError struct {
	Code    int      `json:"code"`
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

// CustomErrorHandler transforms gRPC and domain errors into consistent JSON APIError responses.
func CustomErrorHandler(
	ctx context.Context,
	mux *runtime.ServeMux,
	marshaler runtime.Marshaler,
	w http.ResponseWriter,
	r *http.Request,
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

// NewHTTPServer initializes the REST API gateway and HTTP router.
func NewHTTPServer(
	ctx context.Context,
	service gargiv1.GargiServiceServer,
	addr string,
	logger *slog.Logger,
) (*HTTPServer, error) {
	if logger == nil {
		logger = slog.Default()
	}

	gwMux := runtime.NewServeMux(
		runtime.WithErrorHandler(CustomErrorHandler),
	)

	if err := gargiv1.RegisterGargiServiceHandlerServer(ctx, gwMux, service); err != nil {
		return nil, fmt.Errorf("registering grpc-gateway handler: %w", err)
	}

	rootMux := http.NewServeMux()

	// OpenAPI specifications
	serveOpenAPI := func(w http.ResponseWriter, r *http.Request) {
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

	// WebSocket streaming endpoints
	wsHandler := NewWebSocketHandler(service, logger)
	rootMux.Handle("GET /v1/ws", wsHandler)
	rootMux.Handle("GET /v1/executions/{id}/ws", wsHandler)

	// Mount gRPC-Gateway router
	rootMux.Handle("/", gwMux)

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: rootMux,
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
