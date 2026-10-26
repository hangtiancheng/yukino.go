// Package tracex 提供 trace 穿透基础设施：跨进程透传、seda 桥接、日志 helper。
package tracex

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// TraceParentKey 是跨进程 carrier 的 map key（W3C traceparent）。
const TraceParentKey = "traceparent"

// InjectCarrier 从 ctx 提取当前 span，序列化为 W3C traceparent 文本映射。
// ctx 无有效 span 时返回空 map（不报错）。
func InjectCarrier(ctx context.Context) map[string]string {
	carrier := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, &mapCarrier{carrier})
	return carrier
}

// ContextFromCarrier 从文本映射反序列化 span context，返回带父 span 的 ctx。
// 无有效 traceparent 时返回原 ctx（不 panic、不报错）。
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

// mapCarrier 实现 propagation.TextMapCarrier。
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
