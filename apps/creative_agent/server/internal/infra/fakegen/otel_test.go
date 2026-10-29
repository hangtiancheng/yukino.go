package fakegen_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// setupGlobal sets the global provider and W3C propagator (both no-op by default)
// and restores the old values when the test ends, avoiding pollution of global state for other tests in the same process.
// Same as setupGlobal in pkg/tracex/carrier_test.go.
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

// globalTracer returns the global tracer (used with setupGlobal).
func globalTracer() trace.Tracer { return otel.Tracer("fakegen-test") }

// traceIDFromHeader parses the trace id out of a W3C traceparent header (dash-separated).
// Format: "00-<traceid>-<spanid>-<flags>".
func traceIDFromHeader(tp string) string {
	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		return ""
	}
	return parts[1]
}
