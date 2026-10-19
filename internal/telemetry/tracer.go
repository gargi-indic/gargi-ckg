// Package telemetry provides OpenTelemetry distributed tracing setup and utilities.
package telemetry

import (
	"context"
	"fmt"
	"io"

	"github.com/gargi-indic/gargi-ckg/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
)

const DefaultTracerName = "gargi-ckg"

// Tracer returns a named or default OpenTelemetry tracer from the global TracerProvider.
func Tracer(name ...string) trace.Tracer {
	tracerName := DefaultTracerName
	if len(name) > 0 && name[0] != "" {
		tracerName = name[0]
	}
	return otel.GetTracerProvider().Tracer(tracerName)
}

// SetupTracer initializes the OpenTelemetry TracerProvider according to TracingConfig.
// It configures W3C trace context propagation and returns a shutdown function.
func SetupTracer(ctx context.Context, cfg config.TracingConfig) (func(context.Context) error, error) {
	// Always configure W3C standard traceparent & baggage propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Enabled {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	var exp sdktrace.SpanExporter
	var err error

	if cfg.Endpoint != "" {
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exp, err = otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
		}
	} else {
		// If enabled but no remote endpoint is provided, fallback to stdout trace (discarding output unless testing)
		exp, err = stdouttrace.New(stdouttrace.WithWriter(io.Discard))
		if err != nil {
			return nil, fmt.Errorf("creating fallback stdout trace exporter: %w", err)
		}
	}

	svcName := cfg.ServiceName
	if svcName == "" {
		svcName = DefaultTracerName
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(svcName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("building OpenTelemetry resource: %w", err)
	}

	var sampler sdktrace.Sampler
	if cfg.SamplerRatio <= 0 {
		sampler = sdktrace.NeverSample()
	} else if cfg.SamplerRatio >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else {
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplerRatio))
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)

	shutdown := func(shutdownCtx context.Context) error {
		return tp.Shutdown(shutdownCtx)
	}

	return shutdown, nil
}

// NewTemporalTracingInterceptor creates a Temporal interceptor for OpenTelemetry tracing.
func NewTemporalTracingInterceptor(tracer trace.Tracer) (interceptor.Interceptor, error) {
	opts := temporalotel.TracerOptions{
		Tracer: tracer,
	}
	return temporalotel.NewTracingInterceptor(opts)
}
