package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_Worker(t *testing.T) {
	t.Run("invalid flag", func(t *testing.T) {
		err := run(context.Background(), []string{"-invalid-flag"}, os.LookupEnv, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("missing config file", func(t *testing.T) {
		err := run(context.Background(), []string{"-config", "nonexistent.yaml"}, os.LookupEnv, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "loading config") {
			t.Fatalf("expected loading config error, got: %v", err)
		}
	})

	t.Run("invalid config validation", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgFile := filepath.Join(tmpDir, "config.yaml")
		_ = os.WriteFile(cfgFile, []byte("store:\n  driver: unknown_driver\n"), 0o644)

		err := run(context.Background(), []string{"-config", cfgFile}, os.LookupEnv, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "validating config") {
			t.Fatalf("expected validating config error, got: %v", err)
		}
	})
}
