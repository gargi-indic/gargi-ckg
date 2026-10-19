package api

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultTimeout is applied if the incoming context does not already have a deadline.
const DefaultTimeout = 30 * time.Second

// LoggingUnaryInterceptor logs unary RPC execution, latency, and status code.
func LoggingUnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		st, _ := status.FromError(err)
		code := st.Code()

		if logger != nil {
			lvl := slog.LevelInfo
			if code != codes.OK {
				lvl = slog.LevelWarn
			}
			logger.Log(ctx, lvl, "gRPC unary call",
				"method", info.FullMethod,
				"code", code.String(),
				"duration_ms", duration.Milliseconds(),
			)
		}
		return resp, err
	}
}

// LoggingStreamInterceptor logs stream RPC open and close.
func LoggingStreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		duration := time.Since(start)

		st, _ := status.FromError(err)
		code := st.Code()

		if logger != nil {
			lvl := slog.LevelInfo
			if code != codes.OK && code != codes.Canceled {
				lvl = slog.LevelWarn
			}
			logger.Log(ss.Context(), lvl, "gRPC stream call",
				"method", info.FullMethod,
				"code", code.String(),
				"duration_ms", duration.Milliseconds(),
			)
		}
		return err
	}
}

// RecoveryUnaryInterceptor recovers from panics in unary RPCs and returns codes.Internal.
func RecoveryUnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				if logger != nil {
					logger.Error("panic recovered in gRPC unary handler",
						"method", info.FullMethod,
						"panic", fmt.Sprintf("%v", r),
						"stack", stack,
					)
				}
				err = status.Errorf(codes.Internal, "internal server error: %v", r)
			}
		}()
		return handler(ctx, req)
	}
}

// RecoveryStreamInterceptor recovers from panics in stream RPCs and returns codes.Internal.
func RecoveryStreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				if logger != nil {
					logger.Error("panic recovered in gRPC stream handler",
						"method", info.FullMethod,
						"panic", fmt.Sprintf("%v", r),
						"stack", stack,
					)
				}
				err = status.Errorf(codes.Internal, "internal server error: %v", r)
			}
		}()
		return handler(srv, ss)
	}
}

// DeadlineUnaryInterceptor enforces a default deadline if none is present on incoming context.
func DeadlineUnaryInterceptor(defaultTimeout time.Duration) grpc.UnaryServerInterceptor {
	if defaultTimeout <= 0 {
		defaultTimeout = DefaultTimeout
	}
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
			defer cancel()
		}
		return handler(ctx, req)
	}
}
