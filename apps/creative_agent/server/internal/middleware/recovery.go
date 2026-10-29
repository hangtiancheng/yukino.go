package middleware

import (
	"fmt"
	"net/http"

	"github.com/getsentry/sentry-go"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.uber.org/zap"
)

// Recovery returns a panic-recovery middleware that catches all unhandled panics,
// returns a unified 500 JSON response, and logs the error stack via zap.
// When Sentry is initialized (non-empty DSN) the exception is reported synchronously; when the response has been taken over (e.g. an SSE stream) it only logs.
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
