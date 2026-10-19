package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/api"
	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/gargi-indic/gargi-ckg/internal/store/memory"
	gargiv1 "github.com/gargi-indic/gargi-ckg/pkg/api/v1"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestValidation_GraphDefinitions(t *testing.T) {
	memStore := memory.NewStore()
	tCfg := config.TemporalConfig{TaskQueue: "test-queue"}
	service := api.NewServer(nil, tCfg, memStore, nil)
	ctx := context.Background()

	t.Run("invalid_structure_missing_apiVersion", func(t *testing.T) {
		req := &gargiv1.SubmitExecutionRequest{
			GraphDefinition: `{
				"id": "missing-ver",
				"nodes": [{"id": "n1", "type": "passthrough"}]
			}`,
		}
		_, err := service.SubmitExecution(ctx, req)
		if err == nil {
			t.Fatal("expected error for missing apiVersion, got nil")
		}
		st := status.Convert(err)
		if st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v: %v", st.Code(), err)
		}
	})

	t.Run("invalid_topology_cyclic_graph", func(t *testing.T) {
		req := &gargiv1.SubmitExecutionRequest{
			GraphDefinition: `{
				"apiVersion": "graph/v1",
				"id": "cyclic-graph",
				"nodes": [
					{"id": "nodeA", "type": "passthrough"},
					{"id": "nodeB", "type": "passthrough"}
				],
				"edges": [
					{"from": "nodeA", "to": "nodeB"},
					{"from": "nodeB", "to": "nodeA"}
				]
			}`,
		}
		_, err := service.SubmitExecution(ctx, req)
		if err == nil {
			t.Fatal("expected error for cyclic graph, got nil")
		}
		st := status.Convert(err)
		if st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v: %v", st.Code(), err)
		}
		if !strings.Contains(st.Message(), "topology") && !strings.Contains(st.Message(), "cycle") {
			t.Logf("cycle validation error: %v", st.Message())
		}
	})

	t.Run("invalid_semantic_unregistered_node_type", func(t *testing.T) {
		req := &gargiv1.SubmitExecutionRequest{
			GraphDefinition: `{
				"apiVersion": "graph/v1",
				"id": "unknown-node-graph",
				"nodes": [
					{"id": "n1", "type": "nonexistent_custom_node_type_12345"}
				]
			}`,
		}
		_, err := service.SubmitExecution(ctx, req)
		if err == nil {
			t.Fatal("expected error for unregistered node type, got nil")
		}
		st := status.Convert(err)
		if st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v: %v", st.Code(), err)
		}
	})

	t.Run("valid_graph_passes_all_validation_layers", func(t *testing.T) {
		req := &gargiv1.SubmitExecutionRequest{
			GraphDefinition: `{
				"apiVersion": "graph/v1",
				"id": "valid-pipeline",
				"nodes": [
					{"id": "n1", "type": "passthrough"},
					{"id": "n2", "type": "passthrough"}
				],
				"edges": [
					{"from": "n1", "to": "n2"}
				]
			}`,
		}
		resp, err := service.SubmitExecution(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error for valid graph: %v", err)
		}
		if resp.GetExecutionId() == "" {
			t.Error("expected non-empty execution ID")
		}
	})
}

func TestIdempotency_SubmitExecution(t *testing.T) {
	memStore := memory.NewStore()
	tCfg := config.TemporalConfig{TaskQueue: "test-queue"}
	service := api.NewServer(nil, tCfg, memStore, nil)
	ctx := context.Background()

	validDef := `{
		"apiVersion": "graph/v1",
		"id": "idempotent-pipe",
		"nodes": [{"id": "n1", "type": "passthrough"}]
	}`

	t.Run("idempotency_via_ExecutionId_field", func(t *testing.T) {
		idempotencyKey := "idemp-key-field-123"
		req := &gargiv1.SubmitExecutionRequest{
			ExecutionId:     idempotencyKey,
			GraphDefinition: validDef,
		}

		resp1, err := service.SubmitExecution(ctx, req)
		if err != nil {
			t.Fatalf("first submission failed: %v", err)
		}
		if resp1.GetExecutionId() != idempotencyKey {
			t.Errorf("expected %s, got %s", idempotencyKey, resp1.GetExecutionId())
		}

		// Resubmit with same idempotency key
		resp2, err := service.SubmitExecution(ctx, req)
		if err != nil {
			t.Fatalf("idempotent resubmission failed: %v", err)
		}
		if resp2.GetExecutionId() != idempotencyKey {
			t.Errorf("expected same execution ID %s, got %s", idempotencyKey, resp2.GetExecutionId())
		}
	})

	t.Run("idempotency_via_metadata_header", func(t *testing.T) {
		idempotencyKey := "idemp-key-header-456"
		req := &gargiv1.SubmitExecutionRequest{
			GraphDefinition: validDef,
		}

		mdCtx := metadata.NewIncomingContext(ctx, metadata.Pairs(
			"idempotency-key", idempotencyKey,
		))

		resp1, err := service.SubmitExecution(mdCtx, req)
		if err != nil {
			t.Fatalf("first submission with header failed: %v", err)
		}
		if resp1.GetExecutionId() != idempotencyKey {
			t.Errorf("expected %s, got %s", idempotencyKey, resp1.GetExecutionId())
		}

		// Resubmit with same header
		resp2, err := service.SubmitExecution(mdCtx, req)
		if err != nil {
			t.Fatalf("idempotent resubmission with header failed: %v", err)
		}
		if resp2.GetExecutionId() != idempotencyKey {
			t.Errorf("expected same execution ID %s, got %s", idempotencyKey, resp2.GetExecutionId())
		}
	})
}

func TestErrorMapping_ToGRPCError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode codes.Code
	}{
		{
			name:         "ErrStoreNotFound maps to NotFound",
			err:          engine.ErrStoreNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name: "StoreNotFoundError maps to NotFound",
			err: &engine.StoreNotFoundError{
				ExecutionID: "exec-1",
				Entity:      "GraphState",
				Err:         engine.ErrStoreNotFound,
			},
			expectedCode: codes.NotFound,
		},
		{
			name:         "ErrUnknownNode maps to NotFound",
			err:          engine.ErrUnknownNode,
			expectedCode: codes.NotFound,
		},
		{
			name:         "ErrStoreConflict maps to Aborted (409 Conflict)",
			err:          engine.ErrStoreConflict,
			expectedCode: codes.Aborted,
		},
		{
			name: "StoreConflictError maps to Aborted (409 Conflict)",
			err: &engine.StoreConflictError{
				ExecutionID:     "exec-1",
				ExpectedVersion: 1,
				ActualVersion:   2,
				Err:             engine.ErrStoreConflict,
			},
			expectedCode: codes.Aborted,
		},
		{
			name:         "ErrVersionConflict maps to Aborted (409 Conflict)",
			err:          engine.ErrVersionConflict,
			expectedCode: codes.Aborted,
		},
		{
			name:         "ErrStoreUnavailable maps to Unavailable (503)",
			err:          engine.ErrStoreUnavailable,
			expectedCode: codes.Unavailable,
		},
		{
			name: "StoreUnavailableError maps to Unavailable (503)",
			err: &engine.StoreUnavailableError{
				Backend: "postgres",
			},
			expectedCode: codes.Unavailable,
		},
		{
			name:         "ErrInvalidTransition maps to FailedPrecondition",
			err:          engine.ErrInvalidTransition,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "ErrInvalidDefinition maps to InvalidArgument",
			err:          engine.ErrInvalidDefinition,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "ErrInvalidMutation maps to InvalidArgument",
			err:          engine.ErrInvalidMutation,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "context.Canceled maps to Canceled",
			err:          context.Canceled,
			expectedCode: codes.Canceled,
		},
		{
			name:         "context.DeadlineExceeded maps to DeadlineExceeded",
			err:          context.DeadlineExceeded,
			expectedCode: codes.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grpcErr := api.ToGRPCError(tt.err)
			st := status.Convert(grpcErr)
			if st.Code() != tt.expectedCode {
				t.Errorf("expected code %v, got %v (err: %v)", tt.expectedCode, st.Code(), grpcErr)
			}
		})
	}
}

func TestErrorMapping_HTTPStatusCode(t *testing.T) {
	// Verify CustomErrorHandler outputs expected HTTP status codes and JSON APIError
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/executions/notfound", nil)

	api.CustomErrorHandler(context.Background(), nil, nil, rec, req, engine.ErrStoreNotFound)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404, got %d", rec.Code)
	}

	var apiErr api.APIError
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("failed to decode APIError: %v", err)
	}
	if apiErr.Code != 404 || apiErr.Status != "NotFound" {
		t.Errorf("unexpected APIError payload: %+v", apiErr)
	}
}
