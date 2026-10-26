package main

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/app"

	"go.uber.org/zap"
)

func main() {
	application, err := app.New(config.Load("config/config.json"))
	if err != nil {
		zap.L().Fatal("failed to initialize app", zap.Error(err))
	}

	if err := application.Run(); err != nil {
		zap.L().Fatal("failed to run server", zap.Error(err))
	}
}
