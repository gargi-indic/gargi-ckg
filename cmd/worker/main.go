// Package main provides the entry point for the worker.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/gargi-indic/gargi-ckg/internal/orchestrator"
	"github.com/gargi-indic/gargi-ckg/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "worker failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) (string, bool), stdout io.Writer, _ io.Writer) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
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
	logger.Info("Starting worker", "store_driver", cfg.Store.Driver)

	st, err := store.New(ctx, cfg.Store)
	if err != nil {
		return fmt.Errorf("initializing state store: %w", err)
	}
	defer st.Close()

	temporalClient, err := orchestrator.NewClient(cfg.Temporal, logger)
	if err != nil {
		return fmt.Errorf("creating temporal client: %w", err)
	}
	defer temporalClient.Close()

	wm := orchestrator.NewWorkerManager(temporalClient, cfg.Temporal, logger)
	wm.RegisterActivity(orchestrator.NewStateActivities(st))

	return wm.Run(ctx)
}
