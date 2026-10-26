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
