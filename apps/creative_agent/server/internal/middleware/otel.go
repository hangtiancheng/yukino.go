package middleware

import (
	"strconv"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OTelTracing 为每个请求创建 HTTP span，并把带 span 的 ctx 注入 ctx.Request。
// 顺序必须放在 RequestID 之后、Logger 之前，让 Logger 能读到 trace 字段。
func OTelTracing(serviceName string, tp trace.TracerProvider) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		rctx := ctx.Request.Context()
		route := ctx.Path
		tracer := tp.Tracer("creative-agent/http")
		rctx, span := tracer.Start(rctx, "HTTP "+ctx.Request.Method+" "+route)
		defer span.End()

		// 从 RequestID 中间件注入的 State 取 request_id（客户端未带 X-Request-ID 时也会生成 uuid），
		// 作为 span 属性便于 Jaeger 搜索。
		if rid, ok := ctx.State[ContextRequestID].(string); ok && rid != "" {
			span.SetAttributes(attribute.String("http.request_id", rid))
		}
		span.SetAttributes(
			attribute.String("http.method", ctx.Request.Method),
			attribute.String("http.route", route),
		)

		ctx.Request = ctx.Request.WithContext(rctx)

		next()

		status := ctx.Status
		span.SetAttributes(attribute.Int("http.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, "http status "+strconv.Itoa(status))
		}
	}
}
