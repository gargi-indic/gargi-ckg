package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignalRouter(t *testing.T) {
	router := orchestrator.NewSignalRouter()

	sig1 := orchestrator.ExternalSignalPayload{
		TargetNodeID: "node1",
		SignalName:   "sig1",
		Payload:      map[string]any{"data": 1},
	}
	sig2 := orchestrator.ExternalSignalPayload{
		TargetNodeID: "node1",
		SignalName:   "sig2",
		Payload:      map[string]any{"data": 2},
	}
	sig3 := orchestrator.ExternalSignalPayload{
		TargetNodeID: "node2",
		SignalName:   "sig3",
		Payload:      map[string]any{"data": 3},
	}

	router.RouteSignal(sig1)
	router.RouteSignal(sig2)
	router.RouteSignal(sig3)

	payload, ok := router.ReceiveNamedSignal("node1", "sig2")
	assert.True(t, ok)
	assert.Equal(t, map[string]any{"data": 2}, payload)

	payload, ok = router.ReceiveSignal("node1")
	assert.True(t, ok)
	assert.Equal(t, map[string]any{"data": 1}, payload)

	payload, ok = router.ReceiveSignal("node1")
	assert.False(t, ok)
	assert.Nil(t, payload)

	payload, ok = router.ReceiveNamedSignal("node2", "sig1")
	assert.False(t, ok)
	assert.Nil(t, payload)

	payload, ok = router.ReceiveSignal("node2")
	assert.True(t, ok)
	assert.Equal(t, map[string]any{"data": 3}, payload)

	router.RecordRejected(sig1, "test rejection")
	rejected := router.RejectedSignals()
	require.Len(t, rejected, 1)
	assert.Equal(t, "test rejection", rejected[0].Reason)
	assert.Equal(t, sig1, rejected[0].Signal)
}

func TestTemporalSignalSource(t *testing.T) {
	source := orchestrator.NewTemporalSignalSource()

	sig1 := orchestrator.ExternalSignalPayload{
		TargetNodeID: "node1",
		SignalName:   "sig1",
		Payload:      map[string]any{"data": 1},
	}

	t.Run("deliver then wait", func(t *testing.T) {
		source.Deliver("exec1", sig1)
		payload, err := source.Wait(context.Background(), "exec1", "node1", "sig1")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"data": 1}, payload)
	})

	t.Run("wait then deliver", func(t *testing.T) {
		errCh := make(chan error)
		payloadCh := make(chan map[string]any)

		go func() {
			payload, err := source.Wait(context.Background(), "exec2", "node1", "sig1")
			errCh <- err
			payloadCh <- payload
		}()

		time.Sleep(10 * time.Millisecond) // Ensure wait starts
		source.Deliver("exec2", sig1)

		err := <-errCh
		payload := <-payloadCh

		require.NoError(t, err)
		assert.Equal(t, map[string]any{"data": 1}, payload)
	})

	t.Run("wait context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		payload, err := source.Wait(ctx, "exec3", "node1", "sig1")
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, payload)
	})
}
