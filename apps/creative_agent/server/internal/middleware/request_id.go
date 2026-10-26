package middleware

import (
	"github.com/google/uuid"
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

const (
	headerRequestID  = "X-Request-ID"
	ContextRequestID = "request_id"
)

// RequestID 为每个请求生成唯一标识，注入到 ctx.State 和响应头中。
// 如果客户端已通过 X-Request-ID 头提供了 ID，则复用该值。
func RequestID() yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		requestID := ctx.Get(headerRequestID)
		if requestID == "" {
			requestID = uuid.New().String()
		}
		ctx.State[ContextRequestID] = requestID
		ctx.Set(headerRequestID, requestID)
		next()
	}
}
