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

// TestTraceIDFromFlowEmpty 验证 flow context 无 trace 时返回空串（而非 32 位全零）。
func TestTraceIDFromFlowEmpty(t *testing.T) {
	fc := seda.NewContext()
	testx.Equal(t, "", tracex.TraceIDFromFlow(fc), "无 trace 时应返回空串")
	testx.Equal(t, "", tracex.TraceIDFromFlow(nil), "nil flow context 应返回空串")
}

// TestTraceIDFromFlowPopulated 验证注入 span 后能取回 trace_id。
func TestTraceIDFromFlowPopulated(t *testing.T) {
	setupGlobal(t)
	fc := seda.NewContext()

	ctx, span := otel.Tracer("test").Start(context.Background(), "parent")
	tracex.InjectToFlow(fc, ctx)

	testx.Equal(t, span.SpanContext().TraceID().String(), tracex.TraceIDFromFlow(fc),
		"应从 flow context 取回原 trace_id")

	// ContextFromFlow 恢复的 span 也应有效且同 trace
	restored := tracex.ContextFromFlow(fc)
	testx.True(t, trace.SpanFromContext(restored).SpanContext().IsValid(), "恢复的 span 应有效")
	span.End()
}
