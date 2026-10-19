// Package metrics provides Prometheus metric definitions and recording utilities for Gargi.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ExecutionsStarted tracks total graph executions started.
	ExecutionsStarted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gargi",
			Subsystem: "execution",
			Name:      "started_total",
			Help:      "Total number of graph executions started.",
		},
		[]string{"graph_id"},
	)

	// ExecutionsCompleted tracks total graph executions completed by terminal status.
	ExecutionsCompleted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gargi",
			Subsystem: "execution",
			Name:      "completed_total",
			Help:      "Total number of graph executions completed by terminal status.",
		},
		[]string{"status"},
	)

	// ExecutionsActive tracks currently active graph executions.
	ExecutionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "gargi",
			Subsystem: "execution",
			Name:      "active",
			Help:      "Current number of actively running graph executions.",
		},
	)

	// ExecutionDuration tracks overall duration of graph executions.
	ExecutionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gargi",
			Subsystem: "execution",
			Name:      "duration_seconds",
			Help:      "Duration of graph execution in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"status"},
	)

	// NodeExecutions tracks total node executions by node type and status.
	NodeExecutions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gargi",
			Subsystem: "node",
			Name:      "executions_total",
			Help:      "Total number of node executions by node type and status.",
		},
		[]string{"node_type", "status"},
	)

	// NodeExecutionDuration tracks node execution duration histograms by type and status.
	NodeExecutionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gargi",
			Subsystem: "node",
			Name:      "duration_seconds",
			Help:      "Duration of node execution in seconds.",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"node_type", "status"},
	)

	// NodeRetries tracks the number of retry attempts for nodes.
	NodeRetries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gargi",
			Subsystem: "node",
			Name:      "retries_total",
			Help:      "Total number of retry attempts per node type.",
		},
		[]string{"node_type"},
	)

	// Compensations tracks saga compensation operations by status.
	Compensations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gargi",
			Subsystem: "saga",
			Name:      "compensations_total",
			Help:      "Total number of saga compensation operations executed.",
		},
		[]string{"status"},
	)

	// ActiveStreams tracks currently open streaming connections by stream type (sse, websocket, grpc).
	ActiveStreams = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "gargi",
			Subsystem: "stream",
			Name:      "active",
			Help:      "Current number of active streaming client connections.",
		},
		[]string{"stream_type"},
	)
)

// RecordExecutionStarted records an execution starting.
func RecordExecutionStarted(graphID string) {
	if graphID == "" {
		graphID = "unknown"
	}
	ExecutionsStarted.WithLabelValues(graphID).Inc()
	ExecutionsActive.Inc()
}

// RecordExecutionCompleted records an execution finishing with status and duration.
func RecordExecutionCompleted(status string, duration time.Duration) {
	if status == "" {
		status = "unknown"
	}
	ExecutionsCompleted.WithLabelValues(status).Inc()
	ExecutionsActive.Dec()
	ExecutionDuration.WithLabelValues(status).Observe(duration.Seconds())
}

// RecordNodeExecution records a node execution completion and duration.
func RecordNodeExecution(nodeType, status string, duration time.Duration) {
	if nodeType == "" {
		nodeType = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	NodeExecutions.WithLabelValues(nodeType, status).Inc()
	NodeExecutionDuration.WithLabelValues(nodeType, status).Observe(duration.Seconds())
}

// RecordNodeRetry records a retry attempt on a node.
func RecordNodeRetry(nodeType string) {
	if nodeType == "" {
		nodeType = "unknown"
	}
	NodeRetries.WithLabelValues(nodeType).Inc()
}

// RecordCompensation records a saga compensation attempt.
func RecordCompensation(status string) {
	if status == "" {
		status = "unknown"
	}
	Compensations.WithLabelValues(status).Inc()
}

// RecordStreamConnected records a stream connection being established.
func RecordStreamConnected(streamType string) {
	ActiveStreams.WithLabelValues(streamType).Inc()
}

// RecordStreamDisconnected records a stream connection closing.
func RecordStreamDisconnected(streamType string) {
	ActiveStreams.WithLabelValues(streamType).Dec()
}
