package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/middleware"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TestOTelTracing 验证 OTel 中间件把带 span 的 ctx 注入请求。
func TestOTelTracing(t *testing.T) {
	tp, exp := otelx.NewMemoryExporter()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	r := yukino.New()
	r.Use(middleware.RequestID())
	r.Use(middleware.OTelTracing("test-svc", tp))
	r.Get("/ping", func(c *yukino.Context, _ func()) {
		sc := trace.SpanContextFromContext(c.Request.Context())
		testx.True(t, sc.IsValid(), "请求 ctx 应带有效 span")
		testx.True(t, sc.IsSampled(), "span 应为 sampled（AlwaysSample）")
		c.SetStatus(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	testx.Equal(t, 200, w.Code)

	// RequestID 中间件生成 uuid，OTel 中间件应把它写入 span 属性（http.request_id）。
	testx.Eventually(t, func() bool {
		for _, s := range exp.GetSpans() {
			if s.Name() == "HTTP GET /ping" {
				for _, kv := range s.Attributes() {
					if kv.Key == "http.request_id" && kv.Value.AsString() != "" {
						return true
					}
				}
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "span 应带 http.request_id 属性")
}

// TestOTelTracingTraceParent 验证：入站请求带 traceparent 头时，
// 中间件 Extract 恢复远端父 span 并建 child span（同一 TraceID → 同一条 trace）。
func TestOTelTracingTraceParent(t *testing.T) {
	tp, exp := otelx.NewMemoryExporter()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// 生产路径：otelx.Init 启用时设置全局 W3C propagator，中间件经 otel.GetTextMapPropagator 读取。
	// NewMemoryExporter 故意不改全局（测试隔离），这里手动设置并 defer 恢复。
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)
	defer otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())

	r := yukino.New()
	r.Use(middleware.RequestID())
	r.Use(middleware.OTelTracing("test-svc", tp))
	r.Get("/ping", func(c *yukino.Context, _ func()) {
		sc := trace.SpanContextFromContext(c.Request.Context())
		testx.True(t, sc.IsValid(), "请求 ctx 应带有效 span")
		testx.True(t, sc.IsSampled(), "span 应为 sampled（AlwaysSample）")
		c.SetStatus(200)
	})

	// 构造一个已知 TraceID 的远端父 span ctx，用全局 W3C propagator 序列化为 traceparent 头。
	parentTraceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	parentSpanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	parentSC := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    parentTraceID,
		SpanID:     parentSpanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	parentCtx := trace.ContextWithRemoteSpanContext(context.Background(), parentSC)

	carrier := propagation.HeaderCarrier(http.Header{})
	propagator.Inject(parentCtx, carrier)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("traceparent", carrier.Get("traceparent"))
	r.ServeHTTP(w, req)
	testx.Equal(t, 200, w.Code)

	// child span 应继承父 TraceID（同一条 trace），并带 request_id 属性。
	testx.Eventually(t, func() bool {
		for _, s := range exp.GetSpans() {
			if s.Name() == "HTTP GET /ping" {
				if s.SpanContext().TraceID() != parentTraceID {
					return false
				}
				for _, kv := range s.Attributes() {
					if kv.Key == "http.request_id" && kv.Value.AsString() != "" {
						return true
					}
				}
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "child span 应继承父 TraceID 且带 request_id 属性")
}

// TestOTelTracingNilProvider 验证：TracerProvider 为 nil（OTel 未启用）时
// 中间件直接放行，不建 span、不 panic。
func TestOTelTracingNilProvider(t *testing.T) {
	r := yukino.New()
	r.Use(middleware.RequestID())
	r.Use(middleware.OTelTracing("test-svc", nil))
	r.Get("/ping", func(c *yukino.Context, _ func()) {
		sc := trace.SpanContextFromContext(c.Request.Context())
		testx.False(t, sc.IsValid(), "nil provider 时 ctx 不应带 span")
		c.SetStatus(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	testx.Equal(t, 200, w.Code)
}
