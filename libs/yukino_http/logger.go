package yukino_http

import (
	"log"
	"time"
)

func Logger() Middleware {
	return func(ctx *Context, next func()) {
		t := time.Now()
		next()
		log.Printf("[%d] %s in %v", ctx.Status, ctx.Request.RequestURI, time.Since(t))
	}
}
