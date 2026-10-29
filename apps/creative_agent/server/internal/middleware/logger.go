package middleware

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.uber.org/zap"
)

// Logger returns a zap-based structured request-logging middleware.
// The log level is graded automatically by HTTP status code:
//
//	2xx → Info, 4xx → Warn, 5xx → Error
func Logger(logger *zap.Logger) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		start := time.Now()
		path := ctx.Request.URL.Path
		query := ctx.Request.URL.RawQuery

		// Derive a logger with trace_id/span_id from the request ctx (the OTel middleware has injected the span).
		logger := tracex.Logger(logger, ctx.Request.Context())

		next()

		latency := time.Since(start)
		statusCode := ctx.Status

		fields := []zap.Field{
			zap.Int("status", statusCode),
			zap.String("method", ctx.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", ClientIP(ctx.Request)),
			zap.Duration("latency", latency),
		}

		if requestID, ok := ctx.State[ContextRequestID].(string); ok {
			fields = append(fields, zap.String("request_id", requestID))
		}

		if statusCode >= 500 {
			logger.Error("request completed", fields...)
		} else if statusCode >= 400 {
			logger.Warn("request completed", fields...)
		} else {
			logger.Info("request completed", fields...)
		}
	}
}

// ClientIP extracts the client IP from the request: the first X-Forwarded-For entry takes priority, falling back to RemoteAddr.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
			return first
		}
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
