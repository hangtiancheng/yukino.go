package app

import (
	"net/http"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

type App struct {
	engine *yukino_http.Application
	cfg    *config.Config
}

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

func (a *App) Engine() *yukino_http.Application {
	return a.engine
}

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
