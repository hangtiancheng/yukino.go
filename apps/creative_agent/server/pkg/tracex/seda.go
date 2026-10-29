package tracex

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"

	"go.opentelemetry.io/otel/trace"
)

// flowTraceKey is the key storing the span context in the seda flow context.
// Written at the con_reader entry; read by supervisor_agent / dispatch points.
const flowTraceKey = "otel_trace_context"

// InjectToFlow serializes the current span of ctx and stores it in the flow context.
// Called at the con_reader entry; later operators restore it via ContextFromFlow.
func InjectToFlow(fc *seda.Context, ctx context.Context) {
	if fc == nil || ctx == nil {
		return
	}
	carrier := InjectCarrier(ctx)
	fc.Set(flowTraceKey, carrier)
}

// ContextFromFlow restores a span-carrying context.Context from the flow context.
// It returns context.Background() when the flow context has no span.
func ContextFromFlow(fc *seda.Context) context.Context {
	if fc == nil {
		return context.Background()
	}
	raw, ok := fc.Get(flowTraceKey).(map[string]string)
	if !ok || len(raw) == 0 {
		return context.Background()
	}
	return ContextFromCarrier(context.Background(), raw)
}

// SpanFromFlow returns the current span in the flow context (invalid when absent).
func SpanFromFlow(fc *seda.Context) trace.Span {
	return trace.SpanFromContext(ContextFromFlow(fc))
}

// TraceIDFromFlow returns the trace_id in the flow context (empty string when absent).
// Note: TraceID().String() of an invalid span context is a 32-char all-zero string, not an empty string,
// so IsValid() is checked explicitly here to avoid persisting the zero-value sentinel.
func TraceIDFromFlow(fc *seda.Context) string {
	sc := trace.SpanFromContext(ContextFromFlow(fc)).SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
