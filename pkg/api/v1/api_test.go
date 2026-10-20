package gargiv1_test

import (
	"context"
	"net"
	"testing"
	"time"

	gargiv1 "github.com/gargi-indic/gargi-flow/pkg/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mockGargiServer struct {
	gargiv1.UnimplementedGargiServiceServer
}

func (s *mockGargiServer) SubmitExecution(_ context.Context, req *gargiv1.SubmitExecutionRequest) (*gargiv1.SubmitExecutionResponse, error) {
	return &gargiv1.SubmitExecutionResponse{
		ExecutionId: req.GetExecutionId(),
		Status:      gargiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING,
		StartedAt:   timestamppb.Now(),
	}, nil
}

func (s *mockGargiServer) GetExecution(_ context.Context, req *gargiv1.GetExecutionRequest) (*gargiv1.GetExecutionResponse, error) {
	return &gargiv1.GetExecutionResponse{
		ExecutionId: req.GetExecutionId(),
		Status:      gargiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING,
		Version:     1,
		NodeStates: map[string]*gargiv1.NodeStateDetail{
			"nodeA": {
				NodeId:      "nodeA",
				Status:      gargiv1.NodeStatus_NODE_STATUS_SUCCEEDED,
				Attempt:     1,
				StartedAt:   timestamppb.Now(),
				CompletedAt: timestamppb.Now(),
			},
		},
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
	}, nil
}

func (s *mockGargiServer) GetNodeState(_ context.Context, req *gargiv1.GetNodeStateRequest) (*gargiv1.GetNodeStateResponse, error) {
	return &gargiv1.GetNodeStateResponse{
		ExecutionId: req.GetExecutionId(),
		NodeState: &gargiv1.NodeStateDetail{
			NodeId:  req.GetNodeId(),
			Status:  gargiv1.NodeStatus_NODE_STATUS_RUNNING,
			Attempt: 1,
		},
	}, nil
}

func (s *mockGargiServer) CancelExecution(_ context.Context, req *gargiv1.CancelExecutionRequest) (*gargiv1.CancelExecutionResponse, error) {
	return &gargiv1.CancelExecutionResponse{
		ExecutionId: req.GetExecutionId(),
		Status:      gargiv1.ExecutionStatus_EXECUTION_STATUS_CANCELLED,
		CancelledAt: timestamppb.Now(),
	}, nil
}

func (s *mockGargiServer) SignalNode(_ context.Context, _ *gargiv1.SignalNodeRequest) (*gargiv1.SignalNodeResponse, error) {
	return &gargiv1.SignalNodeResponse{
		Accepted: true,
		Message:  "signal received",
	}, nil
}

func (s *mockGargiServer) StreamExecutionUpdates(req *gargiv1.StreamExecutionUpdatesRequest, stream gargiv1.GargiService_StreamExecutionUpdatesServer) error {
	return stream.Send(&gargiv1.StreamExecutionUpdatesResponse{
		ExecutionId: req.GetExecutionId(),
		Sequence:    1,
		Timestamp:   timestamppb.Now(),
		EventType:   "node_transition",
		NodeId:      "nodeA",
		FromStatus:  gargiv1.NodeStatus_NODE_STATUS_READY,
		ToStatus:    gargiv1.NodeStatus_NODE_STATUS_RUNNING,
	})
}

func TestProtobufMessages_Serialization(t *testing.T) {
	inputs, err := structpb.NewStruct(map[string]any{
		"limit": 100,
		"env":   "prod",
	})
	if err != nil {
		t.Fatalf("failed to create structpb: %v", err)
	}

	req := &gargiv1.SubmitExecutionRequest{
		GraphDefinition: `{"nodes": []}`,
		ExecutionId:     "exec-test-1",
		Inputs:          inputs,
		Metadata: map[string]string{
			"user": "test-user",
		},
	}

	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal proto: %v", err)
	}

	var unmarshaled gargiv1.SubmitExecutionRequest
	if err := proto.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal proto: %v", err)
	}

	if unmarshaled.GetExecutionId() != "exec-test-1" {
		t.Errorf("expected execution ID 'exec-test-1', got '%s'", unmarshaled.GetExecutionId())
	}
	if unmarshaled.GetMetadata()["user"] != "test-user" {
		t.Errorf("expected metadata user 'test-user', got '%s'", unmarshaled.GetMetadata()["user"])
	}
}

func TestProtobufGRPC_ClientServer(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	gargiv1.RegisterGargiServiceServer(server, &mockGargiServer{})

	go func() {
		if err := server.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			t.Errorf("server error: %v", err)
		}
	}()
	t.Cleanup(func() {
		server.Stop()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	//nolint:staticcheck // DialContext used with bufnet for in-memory testing
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial bufnet: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := gargiv1.NewGargiServiceClient(conn)

	t.Run("SubmitExecution", func(t *testing.T) {
		resp, err := client.SubmitExecution(ctx, &gargiv1.SubmitExecutionRequest{
			ExecutionId: "test-exec-123",
		})
		if err != nil {
			t.Fatalf("SubmitExecution failed: %v", err)
		}
		if resp.GetExecutionId() != "test-exec-123" {
			t.Errorf("unexpected execution ID: %s", resp.GetExecutionId())
		}
		if resp.GetStatus() != gargiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING {
			t.Errorf("unexpected status: %v", resp.GetStatus())
		}
	})

	t.Run("GetExecution", func(t *testing.T) {
		resp, err := client.GetExecution(ctx, &gargiv1.GetExecutionRequest{
			ExecutionId: "test-exec-123",
		})
		if err != nil {
			t.Fatalf("GetExecution failed: %v", err)
		}
		if resp.GetNodeStates()["nodeA"].GetStatus() != gargiv1.NodeStatus_NODE_STATUS_SUCCEEDED {
			t.Errorf("unexpected node status: %v", resp.GetNodeStates()["nodeA"].GetStatus())
		}
	})

	t.Run("GetNodeState", func(t *testing.T) {
		resp, err := client.GetNodeState(ctx, &gargiv1.GetNodeStateRequest{
			ExecutionId: "test-exec-123",
			NodeId:      "nodeA",
		})
		if err != nil {
			t.Fatalf("GetNodeState failed: %v", err)
		}
		if resp.GetNodeState().GetStatus() != gargiv1.NodeStatus_NODE_STATUS_RUNNING {
			t.Errorf("unexpected node status: %v", resp.GetNodeState().GetStatus())
		}
	})

	t.Run("CancelExecution", func(t *testing.T) {
		resp, err := client.CancelExecution(ctx, &gargiv1.CancelExecutionRequest{
			ExecutionId: "test-exec-123",
			Reason:      "user aborted",
		})
		if err != nil {
			t.Fatalf("CancelExecution failed: %v", err)
		}
		if resp.GetStatus() != gargiv1.ExecutionStatus_EXECUTION_STATUS_CANCELLED {
			t.Errorf("unexpected status: %v", resp.GetStatus())
		}
	})

	t.Run("SignalNode", func(t *testing.T) {
		resp, err := client.SignalNode(ctx, &gargiv1.SignalNodeRequest{
			ExecutionId: "test-exec-123",
			NodeId:      "nodeA",
			SignalName:  "resume",
		})
		if err != nil {
			t.Fatalf("SignalNode failed: %v", err)
		}
		if !resp.GetAccepted() {
			t.Errorf("expected signal to be accepted")
		}
	})

	t.Run("StreamExecutionUpdates", func(t *testing.T) {
		stream, err := client.StreamExecutionUpdates(ctx, &gargiv1.StreamExecutionUpdatesRequest{
			ExecutionId: "test-exec-123",
		})
		if err != nil {
			t.Fatalf("StreamExecutionUpdates failed: %v", err)
		}
		event, err := stream.Recv()
		if err != nil {
			t.Fatalf("stream recv failed: %v", err)
		}
		if event.GetExecutionId() != "test-exec-123" || event.GetNodeId() != "nodeA" {
			t.Errorf("unexpected event received: %v", event)
		}
	})
}
