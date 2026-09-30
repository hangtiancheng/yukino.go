package middleware

import (
	"github.com/hangtiancheng/yukino.go/yukino_http"
)

func CORS() yukino_http.Middleware {
	return func(ctx *yukino_http.Context, next func()) {
		ctx.Set("Access-Control-Allow-Origin", "*")
		ctx.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		ctx.Set("Access-Control-Allow-Headers", "Origin, Content-Length, Content-Type, Authorization")

		if ctx.Method == "OPTIONS" {
			ctx.Status = 204
			return
		}

		next()
	}
}
