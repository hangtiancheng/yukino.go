// Package otelx provides OpenTelemetry initialization and test helpers.
package otelx

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"
)

// Init initializes the global TracerProvider.
// When enabled=false it returns a no-op provider (zero side effects in tests/CI, no exporter started).
// A failed exporter build does not block startup: it logs a warning and degrades to no-op.
// When logger is nil, zap.L() is used.
func Init(cfg config.OTelConfig, logger *zap.Logger) (*sdktrace.TracerProvider, error) {
	if logger == nil {
		logger = zap.L()
	}
	if !cfg.Enabled {
		// An sdk provider without exporters: spans reach no exporter, equivalent to no-op,
		// but Shutdown semantics are preserved so callers handle it uniformly. Spans from Start are non-recording.
		return sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample())), nil
	}

	// Unified W3C tracecontext propagation (for HTTP headers / Redis stream propagation).
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	sampler := sdktrace.AlwaysSample()
	if cfg.SampleRate < 1.0 {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	exp, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithEndpoint(cfg.ExporterEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		// A failed exporter build does not block startup: degrade to no-op (no exporters) to keep the service available.
		logger.Warn("otelx: failed to create otlp exporter (trace disabled)",
			zap.String("endpoint", cfg.ExporterEndpoint), zap.Error(err))
		return sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample())), nil
	}

	svcName := cfg.ServiceName
	if svcName == "" {
		svcName = "yukino-creative-agent"
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(svcName),
		)),
	)

	// Set globally: the GORM plugin / redisotel / otelhttp read the global provider by default.
	otel.SetTracerProvider(tp)
	return tp, nil
}

// NewMemoryExporter creates an in-memory exporter + TracerProvider for tests.
// Call it inside tests to avoid depending on a real Jaeger. Note: it does not call otel.SetTracerProvider;
// the test decides whether to replace the global provider (to avoid overriding app.New's initialization).
func NewMemoryExporter() (*sdktrace.TracerProvider, *MemoryExporter) {
	exp := newMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(10*time.Millisecond)),
	)
	return tp, exp
}
