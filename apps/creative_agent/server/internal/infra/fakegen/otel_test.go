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

// setupGlobal 设置全局 provider 与 W3C propagator（默认都是 no-op），
// 并在测试结束时恢复旧值，避免污染同进程其他测试的全局状态。
// 与 pkg/tracex/carrier_test.go 的 setupGlobal 同款。
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

// globalTracer 返回全局 tracer（配合 setupGlobal 使用）。
func globalTracer() trace.Tracer { return otel.Tracer("fakegen-test") }

// traceIDFromHeader 从 W3C traceparent 头解出 trace id（0 个连字符的分隔）。
// 形如 "00-<traceid>-<spanid>-<flags>"。
func traceIDFromHeader(tp string) string {
	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		return ""
	}
	return parts[1]
}
