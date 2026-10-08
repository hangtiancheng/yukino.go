// Package app provides the HTTP application layer for the Yukino Chatbot.
// It wires up routes, middleware, and request handlers using the yukino_http framework.
package app

import (
	"net/http"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// App encapsulates the HTTP application and its dependencies.
type App struct {
	engine *yukino_http.Application
	cfg    *config.Config
}

// New creates a new App with all routes and middleware configured.
func New(cfg *config.Config) *App {
	engine := yukino_http.Default()
	engine.Use(corsMiddleware)

	a := &App{engine: engine, cfg: cfg}

	api := engine.Router("/api")
	api.Post("/chat", a.handleChat)
	api.Post("/chat_stream", a.handleChatStream)
	api.Post("/upload", a.handleFileUpload)
	api.Post("/ai_ops", a.handleAIOps)
	api.Post("/log", a.handleSentryLog)
	api.Get("/metrics", a.handleMetrics)

	return a
}

// Engine returns the underlying yukino_http Application for starting the server.
func (a *App) Engine() *yukino_http.Application {
	return a.engine
}

// corsMiddleware adds CORS headers to all responses and handles preflight requests.
// Methods/headers are restricted to GET+POST+OPTIONS / Content-Type, the only
// verbs and header the client uses.
func corsMiddleware(ctx *yukino_http.Context, next func()) {
	ctx.Set("Access-Control-Allow-Origin", "*")
	ctx.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	ctx.Set("Access-Control-Allow-Headers", "Content-Type")
	if ctx.Method == http.MethodOptions {
		ctx.Status = http.StatusNoContent
		return
	}
	next()
}
