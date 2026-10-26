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

// Logger 返回一个基于 zap 的结构化请求日志中间件。
// 日志级别按 HTTP 状态码自动分级：
//
//	2xx → Info, 4xx → Warn, 5xx → Error
func Logger(logger *zap.Logger) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		start := time.Now()
		path := ctx.Request.URL.Path
		query := ctx.Request.URL.RawQuery

		// 从请求 ctx（OTel 中间件已注入 span）派生带 trace_id/span_id 的 logger。
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

// ClientIP 从请求中提取客户端 IP：优先 X-Forwarded-For 首段，回退 RemoteAddr。
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
