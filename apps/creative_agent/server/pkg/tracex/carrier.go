// Package tracex provides trace-propagation infrastructure: cross-process propagation, seda bridging, and logging helpers.
package tracex

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// TraceParentKey is the map key of the cross-process carrier (W3C traceparent).
const TraceParentKey = "traceparent"

// InjectCarrier extracts the current span from ctx and serializes it into a W3C traceparent text map.
// It returns an empty map when ctx has no valid span (no error).
func InjectCarrier(ctx context.Context) map[string]string {
	carrier := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, &mapCarrier{carrier})
	return carrier
}

// ContextFromCarrier deserializes the span context from the text map and returns a ctx carrying the parent span.
// It returns the original ctx when there is no valid traceparent (no panic, no error).
func ContextFromCarrier(ctx context.Context, m map[string]string) context.Context {
	if len(m) == 0 {
		return ctx
	}
	restored := otel.GetTextMapPropagator().Extract(ctx, &mapCarrier{m})
	if !trace.SpanFromContext(restored).SpanContext().IsValid() {
		return ctx
	}
	return restored
}

// mapCarrier implements propagation.TextMapCarrier.
type mapCarrier struct{ m map[string]string }

func (mc *mapCarrier) Get(key string) string { return mc.m[key] }
func (mc *mapCarrier) Set(key, value string) { mc.m[key] = value }
func (mc *mapCarrier) Keys() []string {
	keys := make([]string, 0, len(mc.m))
	for k := range mc.m {
		keys = append(keys, k)
	}
	return keys
}
