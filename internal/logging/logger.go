package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Config defines structured logging configuration.
type Config struct {
	Level  string    `yaml:"level" json:"level"`
	Format string    `yaml:"format" json:"format"`
	Output io.Writer `yaml:"-" json:"-"`
}

// New creates a new configured slog.Logger with correlation ID context extraction.
func New(cfg Config) *slog.Logger {
	out := cfg.Output
	if out == nil {
		out = os.Stdout
	}

	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(cfg.Level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: lvl,
	}

	format := strings.ToLower(strings.TrimSpace(cfg.Format))
	// In production environments (or when explicitly requested), default to JSON format
	if format == "" {
		if env := strings.ToLower(os.Getenv("GARGI_ENV")); env == "production" || env == "prod" {
			format = "json"
		} else if env := strings.ToLower(os.Getenv("ENVIRONMENT")); env == "production" || env == "prod" {
			format = "json"
		} else {
			format = "text"
		}
	}

	var baseHandler slog.Handler
	if format == "json" {
		baseHandler = slog.NewJSONHandler(out, opts)
	} else {
		baseHandler = slog.NewTextHandler(out, opts)
	}

	return slog.New(NewCorrelationHandler(baseHandler))
}

// WithCorrelation creates a sub-logger enriched with execution, node, and workflow correlation IDs.
func WithCorrelationLogger(l *slog.Logger, executionID, nodeID, workflowID, runID string) *slog.Logger {
	if l == nil {
		l = slog.Default()
	}
	args := make([]any, 0, 8)
	if executionID != "" {
		args = append(args, slog.String("execution_id", executionID))
	}
	if nodeID != "" {
		args = append(args, slog.String("node_id", nodeID))
	}
	if workflowID != "" {
		args = append(args, slog.String("workflow_id", workflowID))
	}
	if runID != "" {
		args = append(args, slog.String("run_id", runID))
	}
	if len(args) == 0 {
		return l
	}
	return l.With(args...)
}
