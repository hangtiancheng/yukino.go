package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/obsx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/telemetry"
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

// CORSMiddleware allows the vite dev server (and any configured origin) to
// call the API cross-origin.
func CORSMiddleware(allowAll bool, allowedOrigins []string) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		origin := ctx.Get("Origin")
		allow := ""
		switch {
		case allowAll && origin != "":
			allow = origin
		case origin != "":
			for _, o := range allowedOrigins {
				if o == origin || o == "*" {
					allow = origin
					break
				}
			}
		}
		if allow != "" {
			ctx.Set("Access-Control-Allow-Origin", allow)
			ctx.Set("Vary", "Origin")
			ctx.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			ctx.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key, X-Trace-Id, traceparent, tracestate")
			ctx.Set("Access-Control-Max-Age", "600")
		}
		if ctx.Method == http.MethodOptions {
			ctx.SetStatus(http.StatusNoContent)
			return
		}
		next()
	}
}

func TokenMiddleware(token string, internal bool) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		if token != "" {
			expected := sha256.Sum256([]byte("Bearer " + token))
			received := sha256.Sum256([]byte(ctx.Get("Authorization")))
			if subtle.ConstantTimeCompare(expected[:], received[:]) != 1 {
				ctx.Set("WWW-Authenticate", "Bearer")
				ctx.Throw(http.StatusUnauthorized, "authentication required")
				return
			}
		} else if internal {
			host, _, _ := net.SplitHostPort(ctx.Request.RemoteAddr)
			if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
				ctx.Throw(http.StatusForbidden, "internal endpoint requires a token or loopback connection")
				return
			}
		}
		next()
	}
}

func BodyLimitMiddleware() yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		if ctx.Request.ContentLength > 1<<20 {
			ctx.Throw(http.StatusRequestEntityTooLarge, "request body is too large")
			return
		}
		if ctx.Request.Body != nil {
			ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 1<<20)
		}
		ctx.Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(ctx.Path, "/api/") {
			ctx.Set("Cache-Control", "no-store")
		}
		next()
	}
}

// TraceMiddleware extracts the W3C traceparent header, opens a server span
// and stamps the trace id onto the response and request context.
func TraceMiddleware() yukino.Middleware {
	tracer := telemetry.Tracer("http")
	return func(ctx *yukino.Context, next func()) {
		carrier := propagation.HeaderCarrier(ctx.Request.Header)
		reqCtx := otel.GetTextMapPropagator().Extract(ctx.Request.Context(), carrier)

		reqCtx, span := tracer.Start(reqCtx, ctx.Method+" "+ctx.Path)
		defer span.End()

		ctx.Request = ctx.Request.WithContext(reqCtx)
		traceID := telemetry.TraceIDFrom(reqCtx)
		ctx.State["trace_id"] = traceID
		if traceID != "" {
			ctx.Set("X-Trace-Id", traceID)
		}

		next()

		span.SetAttributes(
			attribute.Int("http.response_status_code", ctx.Status),
			attribute.String("http.route_path", ctx.Path),
		)
	}
}

// SentryRecoverMiddleware converts panics into 500 responses and reports them
// to sentry with request context.
func SentryRecoverMiddleware() yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("request panicked", "path", ctx.Path, "method", ctx.Method, "panic", r)
				obsx.CapturePanic(ctx.Request.Context(), r, map[string]string{
					"path":   ctx.Path,
					"method": ctx.Method,
				})
				ctx.SetStatus(http.StatusInternalServerError)
				ctx.JSON(yukino.H{"message": "internal server error", "data": nil})
			}
		}()
		next()
	}
}

// AccessLogMiddleware records a structured line per request.
func AccessLogMiddleware() yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		start := time.Now()
		next()
		slog.Info("http",
			"method", ctx.Method,
			"path", ctx.Path,
			"status", ctx.Status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}
