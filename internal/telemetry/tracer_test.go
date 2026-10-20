package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/config"
	"go.opentelemetry.io/otel/codes"
)

func TestSetupTracer_Disabled(t *testing.T) {
	ctx := context.Background()
	cfg := config.TracingConfig{
		Enabled: false,
	}

	shutdown, err := SetupTracer(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error setting up disabled tracer: %v", err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			t.Errorf("error shutting down disabled tracer: %v", err)
		}
	}()

	tr := Tracer("test-disabled")
	_, span := tr.Start(ctx, "test-span")
	if span == nil {
		t.Fatal("expected non-nil span from disabled tracer")
	}
	span.End()
}

func TestSetupTracer_Enabled_Stdout(t *testing.T) {
	ctx := context.Background()
	cfg := config.TracingConfig{
		Enabled:      true,
		Endpoint:     "",
		Insecure:     true,
		SamplerRatio: 1.0,
		ServiceName:  "test-service",
	}

	shutdown, err := SetupTracer(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error setting up stdout tracer: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			t.Errorf("error shutting down tracer: %v", err)
		}
	}()

	tr := Tracer()
	_, span := tr.Start(ctx, "test-op")
	if !span.SpanContext().IsValid() {
		t.Errorf("expected valid span context when tracing enabled")
	}

	span.RecordError(errors.New("test error"))
	span.SetStatus(codes.Error, "error occurred")
	span.End()
}

func TestSetupTracer_Samplers(t *testing.T) {
	tests := []struct {
		name         string
		samplerRatio float64
	}{
		{"never_sample", 0.0},
		{"always_sample", 1.0},
		{"ratio_sample", 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := config.TracingConfig{
				Enabled:      true,
				SamplerRatio: tt.samplerRatio,
				ServiceName:  "sampler-test",
			}
			shutdown, err := SetupTracer(ctx, cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer func() {
				_ = shutdown(context.Background())
			}()

			tr := Tracer()
			_, span := tr.Start(ctx, "test-sampler")
			span.End()
		})
	}
}

func TestNewTemporalTracingInterceptor(t *testing.T) {
	tr := Tracer("temporal-test")
	interceptor, err := NewTemporalTracingInterceptor(tr)
	if err != nil {
		t.Fatalf("unexpected error creating temporal tracing interceptor: %v", err)
	}
	if interceptor == nil {
		t.Fatal("expected non-nil interceptor")
	}

	// Also test with nil tracer (defaults to global tracer provider)
	interceptorDefault, err := NewTemporalTracingInterceptor(nil)
	if err != nil {
		t.Fatalf("unexpected error creating default temporal tracing interceptor: %v", err)
	}
	if interceptorDefault == nil {
		t.Fatal("expected non-nil default interceptor")
	}
}

func TestTracer_DefaultName(t *testing.T) {
	tr := Tracer()
	if tr == nil {
		t.Fatal("expected non-nil tracer")
	}
	trNamed := Tracer("custom-scope")
	if trNamed == nil {
		t.Fatal("expected non-nil named tracer")
	}
}
