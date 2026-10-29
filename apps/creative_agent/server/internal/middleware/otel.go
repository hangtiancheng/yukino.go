package middleware

import (
	"strconv"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// OTelTracing creates an HTTP span for each request and injects the span-carrying ctx into ctx.Request.
// It must run after RequestID and before Logger so the Logger can read the trace fields.
//
// If an inbound request carries a traceparent header (propagated by upstream baseapi/fakegen/external services), it is Extracted first
// to restore the remote parent span, then a child span is created so caller → this service → callback forms a single trace in Jaeger.
// When tp is nil (OTel disabled) the request passes through without creating a span.
func OTelTracing(serviceName string, tp trace.TracerProvider) yukino.Middleware {
	if serviceName == "" {
		serviceName = "creative-agent"
	}
	return func(ctx *yukino.Context, next func()) {
		if tp == nil {
			// OTel disabled: pass through without creating a span or extracting.
			next()
			return
		}

		// Restore the W3C tracecontext from inbound HTTP headers (the traceparent propagated by upstream).
		// Without a traceparent header, Extract returns the original ctx, equivalent to creating a new root span.
		rctx := otel.GetTextMapPropagator().Extract(
			ctx.Request.Context(), propagation.HeaderCarrier(ctx.Request.Header))

		route := ctx.Path
		tracer := tp.Tracer(serviceName + "/http")
		rctx, span := tracer.Start(rctx, "HTTP "+ctx.Request.Method+" "+route)
		defer span.End()

		// Read request_id from the State injected by the RequestID middleware (a uuid is generated even when the client sends no X-Request-ID).
		// Recorded as a span attribute for easier Jaeger searching.
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
