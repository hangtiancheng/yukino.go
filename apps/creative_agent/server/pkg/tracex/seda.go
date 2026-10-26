package tracex

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"

	"go.opentelemetry.io/otel/trace"
)

// flowTraceKey 是 seda flow context 中存放 span context 的 key。
// con_reader 入口写入，supervisor_agent / 投递点读取。
const flowTraceKey = "otel_trace_context"

// InjectToFlow 把 ctx 的当前 span 序列化存入 flow context。
// 供 con_reader 入口调用，后续算子通过 ContextFromFlow 恢复。
func InjectToFlow(fc *seda.Context, ctx context.Context) {
	if fc == nil || ctx == nil {
		return
	}
	carrier := InjectCarrier(ctx)
	fc.Set(flowTraceKey, carrier)
}

// ContextFromFlow 从 flow context 恢复带 span 的 context.Context。
// flow context 无 span 时返回 context.Background()。
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

// SpanFromFlow 返回 flow context 里当前 span（无则 invalid）。
func SpanFromFlow(fc *seda.Context) trace.Span {
	return trace.SpanFromContext(ContextFromFlow(fc))
}

// TraceIDFromFlow 返回 flow context 里的 trace_id（无则空串）。
// 注意：invalid span context 的 TraceID().String() 是 32 位全零字符串而非空串，
// 这里显式用 IsValid() 判断，避免把零值 sentinel 落库。
func TraceIDFromFlow(fc *seda.Context) string {
	sc := trace.SpanFromContext(ContextFromFlow(fc)).SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
