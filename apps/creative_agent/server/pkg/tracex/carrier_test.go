package tracex_test

import (
	"context"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// setupGlobal sets the global provider and W3C propagator (both no-op by default)
// and restores the old values when the test ends, avoiding pollution of global state for other tests in the same process.
func setupGlobal(t *testing.T) {
	t.Helper()
	tp, _ := otelx.NewMemoryExporter()
	oldTP := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() {
		otel.SetTracerProvider(oldTP)
		otel.SetTextMapPropagator(oldProp)
		_ = tp.Shutdown(context.Background())
	})
}

// TestCarrierRoundTrip verifies the span context → carrier → restore round trip is consistent.
func TestCarrierRoundTrip(t *testing.T) {
	setupGlobal(t)
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "parent")
	defer span.End()

	carrier := tracex.InjectCarrier(ctx)
	testx.NotEmpty(t, carrier["traceparent"], "a traceparent should be produced")

	restored := tracex.ContextFromCarrier(context.Background(), carrier)
	restoredSpan := trace.SpanFromContext(restored)

	testx.Equal(t, span.SpanContext().TraceID().String(), restoredSpan.SpanContext().TraceID().String(),
		"the restored traceID should match")
	testx.Equal(t, span.SpanContext().SpanID().String(), restoredSpan.SpanContext().SpanID().String(),
		"the restored spanID should match")
}

// TestCarrierEmpty verifies that an empty carrier returns the original ctx (no panic).
func TestCarrierEmpty(t *testing.T) {
	ctx := context.Background()
	restored := tracex.ContextFromCarrier(ctx, nil)
	testx.Equal(t, ctx, restored, "an empty carrier should return the original ctx")
}

// TestContextFromCarrierGarbage verifies that an invalid traceparent does not panic and returns the original ctx.
func TestContextFromCarrierGarbage(t *testing.T) {
	ctx := context.Background()
	restored := tracex.ContextFromCarrier(ctx, map[string]string{"traceparent": "garbage"})
	testx.Equal(t, ctx, restored)
}
