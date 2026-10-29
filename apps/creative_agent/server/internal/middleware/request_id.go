package middleware

import (
	"github.com/google/uuid"
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

const (
	headerRequestID  = "X-Request-ID"
	ContextRequestID = "request_id"
)

// RequestID generates a unique identifier for each request and injects it into ctx.State and the response headers.
// If the client already provided an ID via the X-Request-ID header, that value is reused.
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
