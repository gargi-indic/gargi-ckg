// Package main provides the entry point for the server.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/gargi-indic/gargi-ckg/internal/config"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "server failed: %v\n", err)
		os.Exit(1)
	}
}

func run(_ context.Context, args []string, getenv func(string) (string, bool), stdout io.Writer, _ io.Writer) error {
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
	logger.Info("Starting server", "store_driver", cfg.Store.Driver)

	// TODO(Issue #40): Instantiate the correct store adapter based on cfg.Store.Driver
	// e.g., switch cfg.Store.Driver { case "memory": ... case "redis": ... }

	// TODO(API Epic): Instantiate and start the API Server and Transport using cfg.Server and cfg.Temporal

	return nil
}
