package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"

	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/gargi-indic/gargi-ckg/internal/graph"
	"github.com/gargi-indic/gargi-ckg/internal/orchestrator"
	gargiv1 "github.com/gargi-indic/gargi-ckg/pkg/api/v1"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server implements gargiv1.GargiServiceServer.
type Server struct {
	gargiv1.UnimplementedGargiServiceServer
	temporalClient client.Client
	temporalConfig config.TemporalConfig
	store          engine.StateStore
	logger         *slog.Logger
	parser         *graph.Parser
	bridge         *FanoutBridge
}

// NewServer creates a new GargiServiceServer instance.
func NewServer(
	tc client.Client,
	tCfg config.TemporalConfig,
	st engine.StateStore,
	logger *slog.Logger,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		temporalClient: tc,
		temporalConfig: tCfg,
		store:          st,
		logger:         logger,
		parser:         graph.NewParser(),
		bridge:         NewFanoutBridge(st, logger),
	}
}

// SubmitExecution submits a new graph definition for asynchronous execution.
func (s *Server) SubmitExecution(ctx context.Context, req *gargiv1.SubmitExecutionRequest) (*gargiv1.SubmitExecutionResponse, error) {
	if req == nil || req.GetGraphDefinition() == "" {
		return nil, status.Error(codes.InvalidArgument, "graph_definition is required")
	}

	rawDef := []byte(req.GetGraphDefinition())
	def, err := s.parser.Parse(rawDef, engine.FormatJSON)
	if err != nil {
		def, err = s.parser.Parse(rawDef, engine.FormatYAML)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to parse graph definition: %v", err)
		}
	}

	valErrs := graph.ValidateStructure(def)
	if valErrs.Len() > 0 {
		return nil, status.Errorf(codes.InvalidArgument, "invalid graph definition: %s", valErrs.Error())
	}

	execID := req.GetExecutionId()
	if execID == "" {
		execID = generateUUID()
	}

	var inputs map[string]any
	if req.GetInputs() != nil {
		inputs = req.GetInputs().AsMap()
	}

	wfInput := orchestrator.GraphWorkflowInput{
		ExecutionID:  engine.ExecutionID(execID),
		Definition:   def,
		InitialState: inputs,
	}

	wfOptions := client.StartWorkflowOptions{
		ID:        execID,
		TaskQueue: s.temporalConfig.EffectiveWorkflowTaskQueue(),
	}

	if s.temporalClient != nil {
		_, err := s.temporalClient.ExecuteWorkflow(ctx, wfOptions, orchestrator.GraphExecutionWorkflowName, wfInput)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "starting workflow: %v", err)
		}
	}

	if s.store != nil {
		if g, err := def.ToGraph(); err == nil {
			if initialState, err := engine.NewGraphState(engine.ExecutionID(execID), g); err == nil {
				_ = s.store.SetState(ctx, engine.ExecutionID(execID), initialState, 0)
			}
		}
	}

	return &gargiv1.SubmitExecutionResponse{
		ExecutionId: execID,
		Status:      gargiv1.ExecutionStatus_EXECUTION_STATUS_PENDING,
		StartedAt:   timestamppb.Now(),
	}, nil
}

// GetExecution queries the state of a graph execution.
func (s *Server) GetExecution(ctx context.Context, req *gargiv1.GetExecutionRequest) (*gargiv1.GetExecutionResponse, error) {
	if req == nil || req.GetExecutionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "execution_id is required")
	}

	st, err := orchestrator.FetchStateWithFallback(ctx, s.store, s.temporalClient, req.GetExecutionId(), "")
	if err != nil {
		if engine.IsStoreNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "execution %q not found", req.GetExecutionId())
		}
		return nil, status.Errorf(codes.NotFound, "execution %q not found: %v", req.GetExecutionId(), err)
	}

	return toGetExecutionResponse(req.GetExecutionId(), st), nil
}

// GetNodeState queries the execution state of an individual node in the graph.
func (s *Server) GetNodeState(ctx context.Context, req *gargiv1.GetNodeStateRequest) (*gargiv1.GetNodeStateResponse, error) {
	if req == nil || req.GetExecutionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "execution_id is required")
	}
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	st, err := orchestrator.FetchStateWithFallback(ctx, s.store, s.temporalClient, req.GetExecutionId(), "")
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "execution %q not found: %v", req.GetExecutionId(), err)
	}

	ns := st.NodeState(engine.NodeID(req.GetNodeId()))
	if ns == nil {
		return nil, status.Errorf(codes.NotFound, "node %q not found in execution %q", req.GetNodeId(), req.GetExecutionId())
	}

	return &gargiv1.GetNodeStateResponse{
		ExecutionId: req.GetExecutionId(),
		NodeState:   toNodeStateDetail(ns),
	}, nil
}

// CancelExecution cancels an in-flight graph execution.
func (s *Server) CancelExecution(ctx context.Context, req *gargiv1.CancelExecutionRequest) (*gargiv1.CancelExecutionResponse, error) {
	if req == nil || req.GetExecutionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "execution_id is required")
	}

	if s.temporalClient != nil {
		if err := s.temporalClient.CancelWorkflow(ctx, req.GetExecutionId(), ""); err != nil {
			return nil, status.Errorf(codes.Internal, "cancelling workflow: %v", err)
		}
	}

	return &gargiv1.CancelExecutionResponse{
		ExecutionId: req.GetExecutionId(),
		Status:      gargiv1.ExecutionStatus_EXECUTION_STATUS_CANCELLED,
		CancelledAt: timestamppb.Now(),
	}, nil
}

// SignalNode delivers an external event or signal to a node.
func (s *Server) SignalNode(ctx context.Context, req *gargiv1.SignalNodeRequest) (*gargiv1.SignalNodeResponse, error) {
	if req == nil || req.GetExecutionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "execution_id is required")
	}
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	if req.GetSignalName() == "" {
		return nil, status.Error(codes.InvalidArgument, "signal_name is required")
	}

	var payloadMap map[string]any
	if req.GetPayload() != nil {
		payloadMap = map[string]any{"value": req.GetPayload().AsInterface()}
	}

	sig := orchestrator.ExternalSignalPayload{
		TargetNodeID: engine.NodeID(req.GetNodeId()),
		SignalName:   req.GetSignalName(),
		Payload:      payloadMap,
	}

	if s.temporalClient != nil {
		if err := orchestrator.SendNodeSignal(ctx, s.temporalClient, req.GetExecutionId(), "", sig); err != nil {
			return nil, status.Errorf(codes.Internal, "sending signal: %v", err)
		}
	}

	return &gargiv1.SignalNodeResponse{
		Accepted: true,
		Message:  "signal accepted for delivery",
	}, nil
}

// StreamExecutionUpdates streams execution updates to subscribers using the fan-out bridge.
func (s *Server) StreamExecutionUpdates(
	req *gargiv1.StreamExecutionUpdatesRequest,
	stream gargiv1.GargiService_StreamExecutionUpdatesServer,
) error {
	if req == nil || req.GetExecutionId() == "" {
		return status.Error(codes.InvalidArgument, "execution_id is required")
	}

	ctx := stream.Context()
	return s.bridge.Stream(ctx, req, stream)
}

// Helpers for model conversion.

func toGetExecutionResponse(execID string, st *engine.GraphState) *gargiv1.GetExecutionResponse {
	resp := &gargiv1.GetExecutionResponse{
		ExecutionId: execID,
		Status:      toPBExecutionStatus(st.Status()),
		Version:     st.Version(),
		NodeStates:  make(map[string]*gargiv1.NodeStateDetail),
		CreatedAt:   timestamppb.Now(),
		UpdatedAt:   timestamppb.Now(),
	}

	for _, ns := range st.NodeStates() {
		resp.NodeStates[string(ns.NodeID())] = toNodeStateDetail(ns)
	}

	return resp
}

func toNodeStateDetail(ns *engine.NodeState) *gargiv1.NodeStateDetail {
	if ns == nil {
		return nil
	}

	var outputVal *structpb.Value
	if outMap := ns.Output(); len(outMap) > 0 {
		outputVal, _ = structpb.NewValue(outMap)
	}

	return &gargiv1.NodeStateDetail{
		NodeId:       string(ns.NodeID()),
		Status:       toPBNodeStatus(ns.Status()),
		Output:       outputVal,
		ErrorMessage: ns.ErrorMessage(),
		Attempt:      uint32(ns.Version()),
		StartedAt:    timestamppb.Now(),
		CompletedAt:  timestamppb.Now(),
	}
}

func toPBExecutionStatus(s engine.Status) gargiv1.ExecutionStatus {
	switch s {
	case engine.StatusPending:
		return gargiv1.ExecutionStatus_EXECUTION_STATUS_PENDING
	case engine.StatusReady, engine.StatusRunning:
		return gargiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING
	case engine.StatusSucceeded:
		return gargiv1.ExecutionStatus_EXECUTION_STATUS_SUCCEEDED
	case engine.StatusFailed:
		return gargiv1.ExecutionStatus_EXECUTION_STATUS_FAILED
	default:
		return gargiv1.ExecutionStatus_EXECUTION_STATUS_UNSPECIFIED
	}
}

func toPBNodeStatus(s engine.Status) gargiv1.NodeStatus {
	switch s {
	case engine.StatusPending:
		return gargiv1.NodeStatus_NODE_STATUS_PENDING
	case engine.StatusReady:
		return gargiv1.NodeStatus_NODE_STATUS_READY
	case engine.StatusRunning:
		return gargiv1.NodeStatus_NODE_STATUS_RUNNING
	case engine.StatusSucceeded:
		return gargiv1.NodeStatus_NODE_STATUS_SUCCEEDED
	case engine.StatusFailed:
		return gargiv1.NodeStatus_NODE_STATUS_FAILED
	case engine.StatusSkipped:
		return gargiv1.NodeStatus_NODE_STATUS_SKIPPED
	case engine.StatusCompensating:
		return gargiv1.NodeStatus_NODE_STATUS_COMPENSATING
	case engine.StatusCompensated:
		return gargiv1.NodeStatus_NODE_STATUS_COMPENSATED
	default:
		return gargiv1.NodeStatus_NODE_STATUS_UNSPECIFIED
	}
}

func generateUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
