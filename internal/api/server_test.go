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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

func ptr[T any](v T) *T {
	return &v
}

func setupTestServer(t *testing.T, st engine.StateStore) (gargiv1.GargiServiceClient, grpc_health_v1.HealthClient, *api.GRPCServer, *grpc.ClientConn) {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	tCfg := config.TemporalConfig{
		TaskQueue: "test-queue",
	}

	grpcServer := api.NewGRPCServer(nil, tCfg, st, nil)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	t.Cleanup(func() {
		grpcServer.GracefulStop()
	})

	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	client := gargiv1.NewGargiServiceClient(conn)
	healthClient := grpc_health_v1.NewHealthClient(conn)

	return client, healthClient, grpcServer, conn
}

const validJSONGraph = `{
  "apiVersion": "graph/v1",
  "id": "test-pipeline",
  "nodes": [
    {
      "id": "nodeA",
      "type": "passthrough"
    }
  ]
}`

const validYAMLGraph = `
apiVersion: graph/v1
id: test-pipeline-yaml
nodes:
  - id: nodeA
    type: passthrough
`

func TestRPC_SubmitExecution(t *testing.T) {
	memStore := memory.NewStore()
	client, _, _, _ := setupTestServer(t, memStore)
	ctx := context.Background()

	t.Run("empty request", func(t *testing.T) {
		_, err := client.SubmitExecution(ctx, &gargiv1.SubmitExecutionRequest{})
		if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got: %v", err)
		}
	})

	t.Run("invalid graph syntax", func(t *testing.T) {
		_, err := client.SubmitExecution(ctx, &gargiv1.SubmitExecutionRequest{
			GraphDefinition: "{invalid-json",
		})
		if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got: %v", err)
		}
	})

	t.Run("invalid graph structure", func(t *testing.T) {
		_, err := client.SubmitExecution(ctx, &gargiv1.SubmitExecutionRequest{
			GraphDefinition: `{"apiVersion": "v1", "id": "bad", "nodes": []}`,
		})
		if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got: %v", err)
		}
	})

	t.Run("successful JSON submission", func(t *testing.T) {
		inputs, _ := structpb.NewStruct(map[string]any{"param1": "val1"})
		resp, err := client.SubmitExecution(ctx, &gargiv1.SubmitExecutionRequest{
			GraphDefinition: validJSONGraph,
			ExecutionId:     "exec-json-1",
			Inputs:          inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.GetExecutionId() != "exec-json-1" {
			t.Errorf("expected exec-json-1, got %s", resp.GetExecutionId())
		}
		if resp.GetStatus() != gargiv1.ExecutionStatus_EXECUTION_STATUS_PENDING {
			t.Errorf("expected PENDING status, got %v", resp.GetStatus())
		}

		// Verify state was initialized in store
		st, err := memStore.GetState(ctx, "exec-json-1")
		if err != nil || st == nil {
			t.Fatalf("expected initial state stored in store: %v", err)
		}
	})

	t.Run("successful YAML submission with auto-generated UUID", func(t *testing.T) {
		resp, err := client.SubmitExecution(ctx, &gargiv1.SubmitExecutionRequest{
			GraphDefinition: validYAMLGraph,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.GetExecutionId() == "" {
			t.Errorf("expected auto-generated execution ID")
		}
	})
}

func TestRPC_GetExecutionAndNodeState(t *testing.T) {
	memStore := memory.NewStore()
	client, _, _, _ := setupTestServer(t, memStore)
	ctx := context.Background()

	execID := engine.ExecutionID("exec-query-1")
	def := &engine.GraphDefinition{
		ID: "pipeline",
		Nodes: []engine.NodeDefinition{
			{ID: "node1", Type: "passthrough"},
			{ID: "node2", Type: "passthrough"},
		},
	}
	g, err := def.ToGraph()
	if err != nil {
		t.Fatalf("failed to convert to graph: %v", err)
	}
	st, err := engine.NewGraphState(execID, g)
	if err != nil {
		t.Fatalf("failed to create graph state: %v", err)
	}

	stReady, err := st.Apply(engine.StateMutation{
		NodeID:          "node1",
		ExpectedVersion: st.Version(),
		Status:          ptr(engine.StatusReady),
	})
	if err != nil {
		t.Fatalf("mutation ready failed: %v", err)
	}

	stRunning, err := stReady.Apply(engine.StateMutation{
		NodeID:          "node1",
		ExpectedVersion: stReady.Version(),
		Status:          ptr(engine.StatusRunning),
	})
	if err != nil {
		t.Fatalf("mutation running failed: %v", err)
	}

	stSucceeded, err := stRunning.Apply(engine.StateMutation{
		NodeID:          "node1",
		ExpectedVersion: stRunning.Version(),
		Status:          ptr(engine.StatusSucceeded),
		Set:             map[string]any{"res": 42},
	})
	if err != nil {
		t.Fatalf("mutation succeeded failed: %v", err)
	}

	_ = memStore.SetState(ctx, execID, stSucceeded, 0)

	t.Run("GetExecution success", func(t *testing.T) {
		resp, err := client.GetExecution(ctx, &gargiv1.GetExecutionRequest{
			ExecutionId: string(execID),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.GetExecutionId() != string(execID) {
			t.Errorf("expected %s, got %s", execID, resp.GetExecutionId())
		}
		if len(resp.GetNodeStates()) != 2 {
			t.Errorf("expected 2 node states, got %d", len(resp.GetNodeStates()))
		}
		if resp.GetNodeStates()["node1"].GetStatus() != gargiv1.NodeStatus_NODE_STATUS_SUCCEEDED {
			t.Errorf("expected SUCCEEDED status for node1")
		}
	})

	t.Run("GetExecution not found", func(t *testing.T) {
		_, err := client.GetExecution(ctx, &gargiv1.GetExecutionRequest{
			ExecutionId: "nonexistent",
		})
		if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
			t.Fatalf("expected NotFound, got: %v", err)
		}
	})

	t.Run("GetNodeState success", func(t *testing.T) {
		resp, err := client.GetNodeState(ctx, &gargiv1.GetNodeStateRequest{
			ExecutionId: string(execID),
			NodeId:      "node1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.GetNodeState().GetStatus() != gargiv1.NodeStatus_NODE_STATUS_SUCCEEDED {
			t.Errorf("expected SUCCEEDED status")
		}
		if resp.GetNodeState().GetAttempt() == 0 {
			t.Errorf("expected attempt > 0, got %d", resp.GetNodeState().GetAttempt())
		}
	})

	t.Run("GetNodeState node not found", func(t *testing.T) {
		_, err := client.GetNodeState(ctx, &gargiv1.GetNodeStateRequest{
			ExecutionId: string(execID),
			NodeId:      "unknown_node",
		})
		if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
			t.Fatalf("expected NotFound, got: %v", err)
		}
	})
}

func TestRPC_CancelAndSignal(t *testing.T) {
	memStore := memory.NewStore()
	client, _, _, _ := setupTestServer(t, memStore)
	ctx := context.Background()

	t.Run("CancelExecution", func(t *testing.T) {
		resp, err := client.CancelExecution(ctx, &gargiv1.CancelExecutionRequest{
			ExecutionId: "exec-1",
			Reason:      "user requested cancel",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.GetStatus() != gargiv1.ExecutionStatus_EXECUTION_STATUS_CANCELLED {
			t.Errorf("expected CANCELLED status, got %v", resp.GetStatus())
		}
	})

	t.Run("SignalNode", func(t *testing.T) {
		payload, _ := structpb.NewValue("test_val")
		resp, err := client.SignalNode(ctx, &gargiv1.SignalNodeRequest{
			ExecutionId: "exec-1",
			NodeId:      "node1",
			SignalName:  "resume",
			Payload:     payload,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.GetAccepted() {
			t.Errorf("expected signal accepted")
		}
	})
}

func TestRPC_StreamExecutionUpdates(t *testing.T) {
	memStore := memory.NewStore()
	client, _, _, _ := setupTestServer(t, memStore)

	execID := engine.ExecutionID("exec-stream-1")
	def := &engine.GraphDefinition{
		ID: "stream-pipeline",
		Nodes: []engine.NodeDefinition{
			{ID: "nodeA", Type: "passthrough"},
		},
	}
	g, err := def.ToGraph()
	if err != nil {
		t.Fatalf("failed to convert to graph: %v", err)
	}
	st, err := engine.NewGraphState(execID, g)
	if err != nil {
		t.Fatalf("failed to create graph state: %v", err)
	}
	_ = memStore.SetState(context.Background(), execID, st, 0)

	// Append log entry
	_ = memStore.AppendLog(context.Background(), &engine.ExecutionLogEntry{
		ExecutionID: execID,
		Sequence:    1,
		Timestamp:   time.Now(),
		Message:     "execution_started",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := client.StreamExecutionUpdates(ctx, &gargiv1.StreamExecutionUpdatesRequest{
		ExecutionId: string(execID),
	})
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	// First event should be from execution log replay
	ev, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive first event: %v", err)
	}
	if ev.GetExecutionId() != string(execID) {
		t.Errorf("expected %s, got %s", execID, ev.GetExecutionId())
	}

	// Update node state asynchronously to trigger state change event
	go func() {
		time.Sleep(50 * time.Millisecond)
		stReady, err := st.Apply(engine.StateMutation{
			NodeID:          "nodeA",
			ExpectedVersion: st.Version(),
			Status:          ptr(engine.StatusReady),
		})
		if err != nil {
			return
		}
		stRunning, err := stReady.Apply(engine.StateMutation{
			NodeID:          "nodeA",
			ExpectedVersion: stReady.Version(),
			Status:          ptr(engine.StatusRunning),
		})
		if err != nil {
			return
		}
		_ = memStore.SetState(context.Background(), execID, stRunning, stRunning.Version())

		time.Sleep(50 * time.Millisecond)
		stSucceeded, err := stRunning.Apply(engine.StateMutation{
			NodeID:          "nodeA",
			ExpectedVersion: stRunning.Version(),
			Status:          ptr(engine.StatusSucceeded),
		})
		if err != nil {
			return
		}

		stGraphReady, err := stSucceeded.WithStatus(stSucceeded.Version(), engine.StatusReady)
		if err != nil {
			return
		}
		stGraphRunning, err := stGraphReady.WithStatus(stGraphReady.Version(), engine.StatusRunning)
		if err != nil {
			return
		}
		stTerminal, err := stGraphRunning.WithStatus(stGraphRunning.Version(), engine.StatusSucceeded)
		if err != nil {
			return
		}
		_ = memStore.SetState(context.Background(), execID, stTerminal, stTerminal.Version())
	}()

	// Receive state change event
	ev2, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive state event: %v", err)
	}
	if ev2.GetNodeId() != "nodeA" && ev2.GetEventType() != "node_status_change" {
		t.Errorf("unexpected event: %v", ev2)
	}
}

func TestHealthCheck(t *testing.T) {
	memStore := memory.NewStore()
	_, healthClient, _, _ := setupTestServer(t, memStore)
	ctx := context.Background()

	t.Run("check overall server", func(t *testing.T) {
		resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			t.Fatalf("health check failed: %v", err)
		}
		if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
			t.Errorf("expected SERVING, got %v", resp.GetStatus())
		}
	})

	t.Run("check GargiService", func(t *testing.T) {
		resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{
			Service: "gargi.v1.GargiService",
		})
		if err != nil {
			t.Fatalf("health check failed: %v", err)
		}
		if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
			t.Errorf("expected SERVING, got %v", resp.GetStatus())
		}
	})
}

func TestReflection(t *testing.T) {
	memStore := memory.NewStore()
	_, _, _, conn := setupTestServer(t, memStore)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	refClient := reflectionv1.NewServerReflectionClient(conn)
	stream, err := refClient.ServerReflectionInfo(ctx)
	if err != nil {
		t.Fatalf("failed to start reflection stream: %v", err)
	}

	err = stream.Send(&reflectionv1.ServerReflectionRequest{
		MessageRequest: &reflectionv1.ServerReflectionRequest_ListServices{
			ListServices: "",
		},
	})
	if err != nil {
		t.Fatalf("failed to send reflection request: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive reflection response: %v", err)
	}

	services := resp.GetListServicesResponse().GetService()
	found := false
	for _, s := range services {
		if s.GetName() == "gargi.v1.GargiService" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected gargi.v1.GargiService in reflection services list: %v", services)
	}
}

func TestInterceptors_PanicRecovery(t *testing.T) {
	logger := api.LoggingUnaryInterceptor(nil)
	if logger == nil {
		t.Fatal("expected non-nil interceptor")
	}

	rec := api.RecoveryUnaryInterceptor(nil)
	handler := func(ctx context.Context, req any) (any, error) {
		panic("simulated panic in handler")
	}

	_, err := rec(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/test.Method"}, handler)
	if err == nil {
		t.Fatal("expected error from panic recovery, got nil")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Internal {
		t.Fatalf("expected Internal status code, got: %v", err)
	}
}
