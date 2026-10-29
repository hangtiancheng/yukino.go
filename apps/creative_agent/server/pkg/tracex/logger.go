package tracex

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Logger returns a copy of the logger with trace_id/span_id fields.
// It returns the original logger when ctx has no valid span (no panic, no error).
// Call sites: derive once at each module entry (handleConversation / handleTask / HTTP handler),
// then reuse it throughout the function — existing log statements automatically carry the trace fields.
func Logger(l *zap.Logger, ctx context.Context) *zap.Logger {
	if l == nil || ctx == nil {
		return l
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return l
	}
	return l.With(
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	)
}
