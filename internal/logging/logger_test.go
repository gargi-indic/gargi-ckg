package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogging_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := logging.New(logging.Config{
		Level:  "info",
		Format: "json",
		Output: buf,
	})

	ctx := logging.WithCorrelation(context.Background(), "exec-123", "node-456", "wf-789", "run-abc")
	logger.InfoContext(ctx, "processing node step", "attempt", 1)

	var logEntry map[string]any
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err, "log output must be valid JSON: %s", buf.String())

	assert.Equal(t, "processing node step", logEntry["msg"])
	assert.Equal(t, "INFO", logEntry["level"])
	assert.Equal(t, "exec-123", logEntry["execution_id"])
	assert.Equal(t, "node-456", logEntry["node_id"])
	assert.Equal(t, "wf-789", logEntry["workflow_id"])
	assert.Equal(t, "run-abc", logEntry["run_id"])
	assert.Equal(t, float64(1), logEntry["attempt"])
}

func TestLogging_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := logging.New(logging.Config{
		Level:  "debug",
		Format: "text",
		Output: buf,
	})

	ctx := logging.WithExecutionID(context.Background(), "exec-999")
	logger.DebugContext(ctx, "debug message")

	out := buf.String()
	assert.Contains(t, out, "level=DEBUG")
	assert.Contains(t, out, "msg=\"debug message\"")
	assert.Contains(t, out, "execution_id=exec-999")
}

func TestLogging_ProductionDefault(t *testing.T) {
	t.Setenv("GARGI_ENV", "production")

	buf := &bytes.Buffer{}
	logger := logging.New(logging.Config{
		Output: buf,
	})

	logger.Info("production event", "service", "gargi")

	var logEntry map[string]any
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err, "default in production should be JSON")
	assert.Equal(t, "production event", logEntry["msg"])
	assert.Equal(t, "gargi", logEntry["service"])
}

func TestLogging_WithCorrelationLogger(t *testing.T) {
	buf := &bytes.Buffer{}
	base := logging.New(logging.Config{
		Format: "json",
		Output: buf,
	})

	sub := logging.WithCorrelationLogger(base, "exec-1", "node-1", "wf-1", "run-1")
	sub.Info("sublogger message")

	var logEntry map[string]any
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "exec-1", logEntry["execution_id"])
	assert.Equal(t, "node-1", logEntry["node_id"])
	assert.Equal(t, "wf-1", logEntry["workflow_id"])
	assert.Equal(t, "run-1", logEntry["run_id"])
}

func TestTemporalLoggerAdapter(t *testing.T) {
	buf := &bytes.Buffer{}
	slogLogger := logging.New(logging.Config{
		Level:  "debug",
		Format: "json",
		Output: buf,
	})

	adapter := logging.NewTemporalLoggerAdapter(slogLogger)

	adapter.Debug("temporal debug", "component", "workflow", "attempt", 1)
	adapter.Info("temporal info", "status", "started")
	adapter.Warn("temporal warn", "retry_in", "5s")
	adapter.Error("temporal error", "err", "timeout")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Equal(t, 4, len(lines))

	var debugEntry, infoEntry, warnEntry, errorEntry map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &debugEntry))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &infoEntry))
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &warnEntry))
	require.NoError(t, json.Unmarshal([]byte(lines[3]), &errorEntry))

	assert.Equal(t, "DEBUG", debugEntry["level"])
	assert.Equal(t, "temporal debug", debugEntry["msg"])
	assert.Equal(t, "workflow", debugEntry["component"])

	assert.Equal(t, "INFO", infoEntry["level"])
	assert.Equal(t, "started", infoEntry["status"])

	assert.Equal(t, "WARN", warnEntry["level"])
	assert.Equal(t, "5s", warnEntry["retry_in"])

	assert.Equal(t, "ERROR", errorEntry["level"])
	assert.Equal(t, "timeout", errorEntry["err"])
}
