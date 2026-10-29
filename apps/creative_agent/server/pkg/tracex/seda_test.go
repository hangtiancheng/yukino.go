package tracex_test

import (
	"context"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// TestTraceIDFromFlowEmpty verifies that an empty string (not 32 zeros) is returned when the flow context has no trace.
func TestTraceIDFromFlowEmpty(t *testing.T) {
	fc := seda.NewContext()
	testx.Equal(t, "", tracex.TraceIDFromFlow(fc), "an empty string should be returned when there is no trace")
	testx.Equal(t, "", tracex.TraceIDFromFlow(nil), "a nil flow context should return an empty string")
}

// TestTraceIDFromFlowPopulated verifies that the trace_id can be retrieved after injecting a span.
func TestTraceIDFromFlowPopulated(t *testing.T) {
	setupGlobal(t)
	fc := seda.NewContext()

	ctx, span := otel.Tracer("test").Start(context.Background(), "parent")
	tracex.InjectToFlow(fc, ctx)

	testx.Equal(t, span.SpanContext().TraceID().String(), tracex.TraceIDFromFlow(fc),
		"the original trace_id should be retrieved from the flow context")

	// The span restored by ContextFromFlow should also be valid and in the same trace
	restored := tracex.ContextFromFlow(fc)
	testx.True(t, trace.SpanFromContext(restored).SpanContext().IsValid(), "the restored span should be valid")
	span.End()
}
