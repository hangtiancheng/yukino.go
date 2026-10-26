package middleware

import (
	"fmt"
	"net/http"

	"github.com/getsentry/sentry-go"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.uber.org/zap"
)

// Recovery 返回一个 panic 恢复中间件，catch 所有未处理的 panic
// 并返回统一的 500 JSON 响应，同时通过 zap 记录错误堆栈。
// Sentry 已初始化（DSN 非空）时同步上报异常；响应已接管（如 SSE 流）时只记日志。
func Recovery(logger *zap.Logger) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		defer func() {
			if err := recover(); err != nil {
				if err == http.ErrAbortHandler {
					panic(err)
				}
				logger.Error("panic recovered",
					zap.Any("error", err),
					zap.String("path", ctx.Request.URL.Path),
					zap.String("method", ctx.Request.Method),
				)
				if hub := sentry.CurrentHub(); hub.Client() != nil {
					if e, ok := err.(error); ok {
						hub.CaptureException(e)
					} else {
						hub.CaptureMessage(fmt.Sprintf("%v", err))
					}
				}
				response.Fail(ctx, http.StatusInternalServerError, response.CodeInternal, "internal server error")
			}
		}()
		next()
	}
}
