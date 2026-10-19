// Package main provides the entry point for the server.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/gargi-indic/gargi-ckg/internal/api"
	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/gargi-indic/gargi-ckg/internal/orchestrator"
	"github.com/gargi-indic/gargi-ckg/internal/store"
	"go.temporal.io/sdk/client"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "server failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) (string, bool), stdout io.Writer, _ io.Writer) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to configuration file")

	if err := fs.Parse(args); err != nil {
		return err
	}

	path := *configPath
	if path == "" {
		if envPath, ok := getenv("GARGI_CONFIG"); ok {
			path = envPath
		} else {
			path = "config/config.example.yaml"
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(stdout, nil))
	logger.Info("Starting server", "store_driver", cfg.Store.Driver, "grpc_addr", cfg.Server.GRPCAddress, "http_addr", cfg.Server.HTTPAddress)

	st, err := store.New(ctx, cfg.Store)
	if err != nil {
		return fmt.Errorf("initializing state store: %w", err)
	}
	defer st.Close()

	var temporalClient client.Client
	if tc, err := orchestrator.NewClient(cfg.Temporal, logger); err == nil {
		temporalClient = tc
		defer temporalClient.Close()
	} else {
		logger.Warn("Temporal client unavailable; running in degraded mode", "error", err)
	}

	grpcServer := api.NewGRPCServer(temporalClient, cfg.Temporal, st, logger)
	httpServer, err := api.NewHTTPServer(ctx, grpcServer.Service(), cfg.Server.HTTPAddress, logger)
	if err != nil {
		return fmt.Errorf("initializing HTTP REST server: %w", err)
	}

	grpcLis, err := net.Listen("tcp", cfg.Server.GRPCAddress)
	if err != nil {
		return fmt.Errorf("listening on gRPC address %s: %w", cfg.Server.GRPCAddress, err)
	}
	defer grpcLis.Close()

	httpLis, err := net.Listen("tcp", cfg.Server.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listening on HTTP address %s: %w", cfg.Server.HTTPAddress, err)
	}
	defer httpLis.Close()

	errCh := make(chan error, 2)
	go func() {
		errCh <- grpcServer.Serve(grpcLis)
	}()
	go func() {
		errCh <- httpServer.Serve(httpLis)
	}()

	select {
	case <-ctx.Done():
		grpcServer.GracefulStop()
		_ = httpServer.Shutdown(context.Background())
		return nil
	case err := <-errCh:
		grpcServer.GracefulStop()
		_ = httpServer.Shutdown(context.Background())
		return err
	}
}
