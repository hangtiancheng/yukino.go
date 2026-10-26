package middleware

import (
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// CORS 返回一个允许跨域请求的中间件（开发用宽松配置）。
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
