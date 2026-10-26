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

// setupGlobal 设置全局 provider 与 W3C propagator（默认都是 no-op），
// 并在测试结束时恢复旧值，避免污染同进程其他测试的全局状态。
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

// TestCarrierRoundTrip 验证 span context → carrier → 恢复 往返一致。
func TestCarrierRoundTrip(t *testing.T) {
	setupGlobal(t)
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "parent")
	defer span.End()

	carrier := tracex.InjectCarrier(ctx)
	testx.NotEmpty(t, carrier["traceparent"], "应产出 traceparent")

	restored := tracex.ContextFromCarrier(context.Background(), carrier)
	restoredSpan := trace.SpanFromContext(restored)

	testx.Equal(t, span.SpanContext().TraceID().String(), restoredSpan.SpanContext().TraceID().String(),
		"恢复的 traceID 应一致")
	testx.Equal(t, span.SpanContext().SpanID().String(), restoredSpan.SpanContext().SpanID().String(),
		"恢复的 spanID 应一致")
}

// TestCarrierEmpty 验证空 carrier 返回原 ctx（不 panic）。
func TestCarrierEmpty(t *testing.T) {
	ctx := context.Background()
	restored := tracex.ContextFromCarrier(ctx, nil)
	testx.Equal(t, ctx, restored, "空 carrier 应返回原 ctx")
}

// TestContextFromCarrierGarbage 验证非法 traceparent 不 panic、返回原 ctx。
func TestContextFromCarrierGarbage(t *testing.T) {
	ctx := context.Background()
	restored := tracex.ContextFromCarrier(ctx, map[string]string{"traceparent": "garbage"})
	testx.Equal(t, ctx, restored)
}
