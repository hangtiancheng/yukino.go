// Yukino Chatbot is an AI-powered intelligent operations assistant.
// It provides chat-based interaction with RAG (Retrieval-Augmented Generation),
// knowledge base indexing, and automated alert analysis capabilities.
//
// The application uses the yukino_http framework for HTTP serving and the
// Eino framework for AI pipeline orchestration.
package main

import (
	"log"

	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/app"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/utility/logger"
)

func main() {
	logger.Init()

	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	application := app.New(cfg)

	logger.L().Info("yukino_agent listening", "addr", cfg.ServerAddr)
	if err := application.Engine().Listen(cfg.ServerAddr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
