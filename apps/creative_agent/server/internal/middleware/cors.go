package middleware

import (
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// CORS returns a middleware allowing cross-origin requests (permissive configuration for development).
func CORS() yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		ctx.Set("Access-Control-Allow-Origin", "*")
		ctx.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		ctx.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if ctx.Request.Method == "OPTIONS" {
			ctx.SetStatus(204)
			return
		}
		next()
	}
}
