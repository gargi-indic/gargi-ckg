package nodes

import (
	"context"
	"errors"
	"time"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

type waitSignalConfig struct {
	Signal         string `json:"signal"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (c *waitSignalConfig) Validate() error {
	if c.Signal == "" {
		return errors.New("signal is required")
	}
	if c.TimeoutSeconds < 0 {
		return errors.New("timeout_seconds cannot be negative")
	}
	return nil
}

type waitSignalNode struct {
	*engine.BaseNode
	cfg     waitSignalConfig
	signals SignalSource
}

var _ engine.NodeInterface = (*waitSignalNode)(nil)

// Execute blocks until the signal arrives or the timeout/context expires.
// The orchestrator is responsible for activity heartbeats and timeouts.
func (n *waitSignalNode) Execute(ctx context.Context, nc engine.NodeContext) (*engine.StateMutation, error) {
	waitCtx := ctx
	var cancel context.CancelFunc

	if n.cfg.TimeoutSeconds > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, time.Duration(n.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	payload, err := n.signals.Wait(waitCtx, nc.ExecutionID(), nc.NodeID(), n.cfg.Signal)
	if err != nil {
		// Parent context cancelled?
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err() // return unwrapped
		}

		// Node's own timeout fired?
		if n.cfg.TimeoutSeconds > 0 && errors.Is(waitCtx.Err(), context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, engine.NewNonRetryableError(errors.New("wait_for_signal timeout exceeded"))
		}

		// Check if it's already a classified NodeError
		// The requirement: "an error from Wait is returned as-is if already classified, otherwise wrapped as retryable"
		var nodeErr *engine.NodeError
		if errors.As(err, &nodeErr) {
			return nil, err
		}

		return nil, engine.NewRetryableError(err)
	}

	// Deep copy the payload
	copiedPayload := deepCopyMap(payload)

	mut := engine.NewStateMutation(nc.NodeID(), nc.State().Version(), nil, map[string]any{
		"signal":  n.cfg.Signal,
		"payload": copiedPayload,
	}, nil, nil)

	return &mut, nil
}
