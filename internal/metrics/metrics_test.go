package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetrics_Recording(t *testing.T) {
	// Execution
	RecordExecutionStarted("graph-1")
	RecordExecutionCompleted("succeeded", 250*time.Millisecond)

	startedVal := testutil.ToFloat64(ExecutionsStarted.WithLabelValues("graph-1"))
	if startedVal < 1 {
		t.Errorf("expected ExecutionsStarted >= 1, got %f", startedVal)
	}

	completedVal := testutil.ToFloat64(ExecutionsCompleted.WithLabelValues("succeeded"))
	if completedVal < 1 {
		t.Errorf("expected ExecutionsCompleted >= 1, got %f", completedVal)
	}

	// Node Execution
	RecordNodeExecution("compute", "succeeded", 50*time.Millisecond)
	nodeVal := testutil.ToFloat64(NodeExecutions.WithLabelValues("compute", "succeeded"))
	if nodeVal < 1 {
		t.Errorf("expected NodeExecutions >= 1, got %f", nodeVal)
	}

	// Retry
	RecordNodeRetry("network_call")
	retryVal := testutil.ToFloat64(NodeRetries.WithLabelValues("network_call"))
	if retryVal < 1 {
		t.Errorf("expected NodeRetries >= 1, got %f", retryVal)
	}

	// Compensation
	RecordCompensation("success")
	RecordCompensation("failure")
	compSuccess := testutil.ToFloat64(Compensations.WithLabelValues("success"))
	if compSuccess < 1 {
		t.Errorf("expected Compensations success >= 1, got %f", compSuccess)
	}

	// Stream
	RecordStreamConnected("sse")
	activeVal := testutil.ToFloat64(ActiveStreams.WithLabelValues("sse"))
	if activeVal < 1 {
		t.Errorf("expected ActiveStreams >= 1, got %f", activeVal)
	}
	RecordStreamDisconnected("sse")
	activeValAfter := testutil.ToFloat64(ActiveStreams.WithLabelValues("sse"))
	if activeValAfter != 0 {
		t.Errorf("expected ActiveStreams == 0, got %f", activeValAfter)
	}
}

func TestMetrics_Registry(t *testing.T) {
	// Verify all registered metrics can be gathered by DefaultGatherer
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("unexpected error gathering metrics: %v", err)
	}

	names := make(map[string]bool)
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}

	expectedPrefixes := []string{
		"gargi_execution_started_total",
		"gargi_execution_completed_total",
		"gargi_execution_active",
		"gargi_execution_duration_seconds",
		"gargi_node_executions_total",
		"gargi_node_duration_seconds",
		"gargi_node_retries_total",
		"gargi_saga_compensations_total",
		"gargi_stream_active",
	}

	for _, prefix := range expectedPrefixes {
		found := false
		for n := range names {
			if strings.HasPrefix(n, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected metric %q to be registered in DefaultGatherer", prefix)
		}
	}
}
