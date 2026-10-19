package api_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/api"
	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/gargi-indic/gargi-ckg/internal/store/memory"
	gargiv1 "github.com/gargi-indic/gargi-ckg/pkg/api/v1"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
)

func setupTestGRPCServer(t *testing.T, store engine.StateStore) (gargiv1.GargiServiceClient, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	service := api.NewServer(nil, config.TemporalConfig{TaskQueue: "test-queue"}, store, nil)
	grpcServer := grpc.NewServer()
	gargiv1.RegisterGargiServiceServer(grpcServer, service)

	go func() {
		_ = grpcServer.Serve(lis)
	}()

	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := gargiv1.NewGargiServiceClient(conn)

	cleanup := func() {
		_ = conn.Close()
		grpcServer.GracefulStop()
	}

	return client, cleanup
}

const testGraphJSON = `{
	"apiVersion": "graph/v1",
	"id": "sync-async-graph",
	"nodes": [
		{
			"id": "task1",
			"type": "transform",
			"config": {"set": {"key": "val"}}
		}
	]
}`

func TestSubmitExecution_AsyncMode(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	client, cleanup := setupTestGRPCServer(t, store)
	defer cleanup()

	// Default async execution (wait: false)
	req := &gargiv1.SubmitExecutionRequest{
		ExecutionId:     "exec-async-1",
		GraphDefinition: testGraphJSON,
		Wait:            false,
	}

	resp, err := client.SubmitExecution(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "exec-async-1", resp.GetExecutionId())
	assert.Equal(t, gargiv1.ExecutionStatus_EXECUTION_STATUS_PENDING, resp.GetStatus())
	assert.NotNil(t, resp.GetStartedAt())
	assert.Nil(t, resp.GetCompletedAt())

	// Confirm store was populated with pending initial state
	st, err := store.GetState(ctx, "exec-async-1")
	require.NoError(t, err)
	require.NotNil(t, st)
	assert.Equal(t, engine.StatusPending, st.Status())
}

func TestSubmitExecution_SyncMode_TerminalWait(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	client, cleanup := setupTestGRPCServer(t, store)
	defer cleanup()

	execID := "exec-sync-complete-1"

	// Start asynchronous goroutine that marks execution as Succeeded after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		st, err := store.GetState(ctx, engine.ExecutionID(execID))
		if err == nil && st != nil {
			ready := engine.StatusReady
			running := engine.StatusRunning
			succeeded := engine.StatusSucceeded

			st1, err := st.Apply(engine.NewStateMutation("task1", 0, &ready, nil, nil, nil))
			require.NoError(t, err)
			st2, err := st1.Apply(engine.NewStateMutation("task1", 1, &running, nil, nil, nil))
			require.NoError(t, err)
			updatedSt, err := st2.Apply(engine.NewStateMutation("task1", 2, &succeeded, map[string]any{"result": "done"}, nil, nil))
			require.NoError(t, err)
			updatedSt, err = updatedSt.WithStatus(updatedSt.Version(), engine.StatusReady)
			require.NoError(t, err)
			updatedSt, err = updatedSt.WithStatus(updatedSt.Version(), engine.StatusRunning)
			require.NoError(t, err)
			updatedSt, err = updatedSt.WithStatus(updatedSt.Version(), engine.StatusSucceeded)
			require.NoError(t, err)
			_ = store.SetState(ctx, engine.ExecutionID(execID), updatedSt, st.Version())
		}
	}()

	start := time.Now()
	resp, err := client.SubmitExecution(ctx, &gargiv1.SubmitExecutionRequest{
		ExecutionId:     execID,
		GraphDefinition: testGraphJSON,
		Wait:            true,
		Timeout:         durationpb.New(2 * time.Second),
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 40*time.Millisecond)
	assert.Equal(t, execID, resp.GetExecutionId())
	assert.Equal(t, gargiv1.ExecutionStatus_EXECUTION_STATUS_SUCCEEDED, resp.GetStatus())
	assert.NotNil(t, resp.GetCompletedAt())
	assert.Equal(t, gargiv1.NodeStatus_NODE_STATUS_SUCCEEDED, resp.GetNodeStates()["task1"].GetStatus())
}

func TestSubmitExecution_SyncMode_Timeout(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	client, cleanup := setupTestGRPCServer(t, store)
	defer cleanup()

	execID := "exec-sync-timeout-1"

	// Submit with wait: true and a tight timeout of 60ms; store is never marked terminal
	start := time.Now()
	resp, err := client.SubmitExecution(ctx, &gargiv1.SubmitExecutionRequest{
		ExecutionId:     execID,
		GraphDefinition: testGraphJSON,
		Wait:            true,
		Timeout:         durationpb.New(60 * time.Millisecond),
	})
	elapsed := time.Since(start)

	assert.Nil(t, resp)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.DeadlineExceeded, st.Code())
	assert.Contains(t, st.Message(), "timed out waiting for execution completion")
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
}

func TestSubmitExecution_SyncMode_IdempotentTerminal(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	client, cleanup := setupTestGRPCServer(t, store)
	defer cleanup()

	execID := "exec-sync-idemp-1"

	// Pre-populate store with succeeded state
	node, err := engine.NewNode("task1", "transform", nil)
	require.NoError(t, err)
	g, err := engine.NewGraph("sync-async-graph", []*engine.Node{node}, nil)
	require.NoError(t, err)
	st, err := engine.NewGraphState(engine.ExecutionID(execID), g)
	require.NoError(t, err)
	ready := engine.StatusReady
	running := engine.StatusRunning
	succeeded := engine.StatusSucceeded

	st1, err := st.Apply(engine.NewStateMutation("task1", 0, &ready, nil, nil, nil))
	require.NoError(t, err)
	st2, err := st1.Apply(engine.NewStateMutation("task1", 1, &running, nil, nil, nil))
	require.NoError(t, err)
	finalSt, err := st2.Apply(engine.NewStateMutation("task1", 2, &succeeded, map[string]any{"ok": true}, nil, nil))
	require.NoError(t, err)
	finalSt, err = finalSt.WithStatus(finalSt.Version(), engine.StatusReady)
	require.NoError(t, err)
	finalSt, err = finalSt.WithStatus(finalSt.Version(), engine.StatusRunning)
	require.NoError(t, err)
	finalSt, err = finalSt.WithStatus(finalSt.Version(), engine.StatusSucceeded)
	require.NoError(t, err)
	require.NoError(t, store.SetState(ctx, engine.ExecutionID(execID), finalSt, 0))

	// Calling SubmitExecution with wait: true immediately returns the final state without waiting
	start := time.Now()
	resp, err := client.SubmitExecution(ctx, &gargiv1.SubmitExecutionRequest{
		ExecutionId:     execID,
		GraphDefinition: testGraphJSON,
		Wait:            true,
		Timeout:         durationpb.New(2 * time.Second),
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 100*time.Millisecond)
	assert.Equal(t, execID, resp.GetExecutionId())
	assert.Equal(t, gargiv1.ExecutionStatus_EXECUTION_STATUS_SUCCEEDED, resp.GetStatus())
	assert.Equal(t, gargiv1.NodeStatus_NODE_STATUS_SUCCEEDED, resp.GetNodeStates()["task1"].GetStatus())
}
