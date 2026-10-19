// Package logging provides structured logging and correlation context for the engine.
package logging

import (
	"context"
	"log/slog"
)

type contextKey string

const (
	keyExecutionID contextKey = "execution_id"
	keyNodeID      contextKey = "node_id"
	keyWorkflowID  contextKey = "workflow_id"
	keyRunID       contextKey = "run_id"
	keyTraceID     contextKey = "trace_id"
)

// WithExecutionID returns a context carrying the execution ID.
func WithExecutionID(ctx context.Context, executionID string) context.Context {
	if executionID == "" {
		return ctx
	}
	return context.WithValue(ctx, keyExecutionID, executionID)
}

// WithNodeID returns a context carrying the node ID.
func WithNodeID(ctx context.Context, nodeID string) context.Context {
	if nodeID == "" {
		return ctx
	}
	return context.WithValue(ctx, keyNodeID, nodeID)
}

// WithWorkflowIDs returns a context carrying Temporal workflow and run IDs.
func WithWorkflowIDs(ctx context.Context, workflowID, runID string) context.Context {
	if workflowID != "" {
		ctx = context.WithValue(ctx, keyWorkflowID, workflowID)
	}
	if runID != "" {
		ctx = context.WithValue(ctx, keyRunID, runID)
	}
	return ctx
}

// WithCorrelation returns a context with all correlation identifiers set.
func WithCorrelation(ctx context.Context, executionID, nodeID, workflowID, runID string) context.Context {
	ctx = WithExecutionID(ctx, executionID)
	ctx = WithNodeID(ctx, nodeID)
	ctx = WithWorkflowIDs(ctx, workflowID, runID)
	return ctx
}

// CorrelationHandler is a custom slog.Handler that extracts correlation attributes from context.Context.
type CorrelationHandler struct {
	slog.Handler
}

// NewCorrelationHandler wraps an existing slog.Handler to inject correlation IDs from context.
func NewCorrelationHandler(h slog.Handler) *CorrelationHandler {
	return &CorrelationHandler{Handler: h}
}

// Handle extracts correlation attributes from the context and appends them to the record before passing downstream.
func (h *CorrelationHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if execID, ok := ctx.Value(keyExecutionID).(string); ok && execID != "" {
			r.AddAttrs(slog.String("execution_id", execID))
		}
		if nodeID, ok := ctx.Value(keyNodeID).(string); ok && nodeID != "" {
			r.AddAttrs(slog.String("node_id", nodeID))
		}
		if wfID, ok := ctx.Value(keyWorkflowID).(string); ok && wfID != "" {
			r.AddAttrs(slog.String("workflow_id", wfID))
		}
		if runID, ok := ctx.Value(keyRunID).(string); ok && runID != "" {
			r.AddAttrs(slog.String("run_id", runID))
		}
		if traceID, ok := ctx.Value(keyTraceID).(string); ok && traceID != "" {
			r.AddAttrs(slog.String("trace_id", traceID))
		}
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs returns a new CorrelationHandler wrapping the parent's WithAttrs.
func (h *CorrelationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CorrelationHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a new CorrelationHandler wrapping the parent's WithGroup.
func (h *CorrelationHandler) WithGroup(name string) slog.Handler {
	return &CorrelationHandler{Handler: h.Handler.WithGroup(name)}
}
