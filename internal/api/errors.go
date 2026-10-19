package api

import (
	"context"
	"errors"
	"strings"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToGRPCError maps domain and storage errors into standardized gRPC status errors.
// When passed through gRPC-Gateway, these gRPC codes automatically translate to
// standard HTTP response status codes:
//   - codes.InvalidArgument      -> 400 Bad Request
//   - codes.NotFound             -> 404 Not Found
//   - codes.Aborted              -> 409 Conflict
//   - codes.AlreadyExists        -> 409 Conflict
//   - codes.FailedPrecondition   -> 400 Bad Request (or 412 Precondition Failed)
//   - codes.Unavailable          -> 503 Service Unavailable
//   - codes.DeadlineExceeded     -> 504 Gateway Timeout
//   - codes.Canceled             -> 499 Client Closed Request
//   - codes.Internal             -> 500 Internal Server Error
func ToGRPCError(err error) error {
	if err == nil {
		return nil
	}

	// If already a gRPC status error, return as is
	if _, ok := status.FromError(err); ok {
		return err
	}

	// 1. Not Found errors (HTTP 404)
	if errors.Is(err, engine.ErrStoreNotFound) ||
		engine.IsStoreNotFound(err) ||
		errors.Is(err, engine.ErrUnknownNode) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "no temporal client provided for fallback") {
		return status.Error(codes.NotFound, err.Error())
	}

	// 2. Conflict / Optimistic Concurrency / Version Conflict (HTTP 409)
	if errors.Is(err, engine.ErrStoreConflict) ||
		engine.IsStoreConflict(err) ||
		errors.Is(err, engine.ErrVersionConflict) {
		return status.Error(codes.Aborted, err.Error())
	}

	// 3. Storage Backend Unavailable (HTTP 503)
	if errors.Is(err, engine.ErrStoreUnavailable) ||
		engine.IsStoreUnavailable(err) {
		return status.Error(codes.Unavailable, err.Error())
	}

	// 4. Invalid State Transition (HTTP 400 / FailedPrecondition)
	if errors.Is(err, engine.ErrInvalidTransition) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}

	// 5. Validation / Bad Request errors (HTTP 400)
	var valErrs *engine.ValidationErrors
	if errors.As(err, &valErrs) {
		return status.Error(codes.InvalidArgument, valErrs.Error())
	}

	if errors.Is(err, engine.ErrInvalidDefinition) ||
		errors.Is(err, engine.ErrInvalidMutation) {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// 6. Context cancellation / timeout
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, err.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, err.Error())
	}

	// 7. Default to Internal Server Error (HTTP 500)
	return status.Error(codes.Internal, err.Error())
}
