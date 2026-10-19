package api

import (
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/config"
	gargiv1 "github.com/gargi-indic/gargi-ckg/pkg/api/v1"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// GRPCServer manages the lifecycle of the gRPC API server, registering interceptors,
// reflection, and the health check service.
type GRPCServer struct {
	grpcServer *grpc.Server
	service    *Server
	healthSrv  *health.Server
	logger     *slog.Logger
}

// NewGRPCServer constructs and configures a new GRPCServer instance.
func NewGRPCServer(
	tc client.Client,
	tCfg config.TemporalConfig,
	st engine.StateStore,
	logger *slog.Logger,
	opts ...grpc.ServerOption,
) *GRPCServer {
	if logger == nil {
		logger = slog.Default()
	}

	// Chain standard interceptors for recovery, logging, and deadlines
	defaultOpts := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			RecoveryUnaryInterceptor(logger),
			LoggingUnaryInterceptor(logger),
			DeadlineUnaryInterceptor(30*time.Second),
		),
		grpc.ChainStreamInterceptor(
			RecoveryStreamInterceptor(logger),
			LoggingStreamInterceptor(logger),
		),
	}

	allOpts := append(defaultOpts, opts...)
	server := grpc.NewServer(allOpts...)

	service := NewServer(tc, tCfg, st, logger)
	gargiv1.RegisterGargiServiceServer(server, service)

	// Health service
	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthSrv)
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthSrv.SetServingStatus("gargi.v1.GargiService", grpc_health_v1.HealthCheckResponse_SERVING)

	// gRPC Reflection
	reflection.Register(server)

	return &GRPCServer{
		grpcServer: server,
		service:    service,
		healthSrv:  healthSrv,
		logger:     logger,
	}
}

// Service returns the underlying GargiServiceServer implementation.
func (s *GRPCServer) Service() *Server {
	return s.service
}

// HealthServer returns the gRPC health server instance.
func (s *GRPCServer) HealthServer() *health.Server {
	return s.healthSrv
}

// Server returns the underlying *grpc.Server.
func (s *GRPCServer) Server() *grpc.Server {
	return s.grpcServer
}

// Serve begins serving gRPC traffic on the provided net.Listener.
func (s *GRPCServer) Serve(lis net.Listener) error {
	s.logger.Info("Starting gRPC server", "addr", lis.Addr().String())
	if err := s.grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("gRPC server serve: %w", err)
	}
	return nil
}

// GracefulStop gracefully stops the gRPC server.
func (s *GRPCServer) GracefulStop() {
	s.logger.Info("Gracefully stopping gRPC server")
	s.healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	s.healthSrv.SetServingStatus("gargi.v1.GargiService", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	s.grpcServer.GracefulStop()
}

// Stop immediately stops the gRPC server.
func (s *GRPCServer) Stop() {
	s.grpcServer.Stop()
}
