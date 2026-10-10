package main

import (
	"log"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/app"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/logger"
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
