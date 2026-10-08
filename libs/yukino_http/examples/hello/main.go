package main

import (
	"net/http"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

func main() {
	app := yukino.Default()
	app.Get("/", func(ctx *yukino.Context, next func()) {
		ctx.Status = http.StatusOK
		ctx.String("Hello World\n")
	})
	app.Get("/panic", func(ctx *yukino.Context, next func()) {
		names := []string{"yukino"}
		ctx.Status = http.StatusOK
		ctx.String("%s", names[100])
	})
	app.Get("/events", func(ctx *yukino.Context, next func()) {
		sse := ctx.SSE()
		sse.Event("greeting", "hello")
		sse.Data("world")
		sse.Done()
	})

	app.Listen(":9999")
}
