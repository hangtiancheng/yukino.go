package tracex

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Logger 返回带 trace_id/span_id 字段的 logger 副本。
// ctx 无有效 span 时返回原 logger（不 panic、不报错）。
// 调用点：各模块入口（handleConversation / handleTask / HTTP handler）派生一次，
// 之后整条函数内复用——既有日志语句自动带 trace 字段。
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
