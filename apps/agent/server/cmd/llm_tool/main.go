package main

import (
	"context"
	"fmt"
	"github.com/hangtiancheng/yukino-code/apps/yukino/conversation"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/runtime"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"log"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	client, protocol, err := models.New(cfg, cfg.QuickChatModel, "You are a helpful assistant.")
	if err != nil {
		log.Fatal(err)
	}
	registry, err := tools.NewRegistry(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer registry.Dispose()
	conv := conversation.NewManager()
	conv.AddUserMessage("Tell me what tools you have available.")
	answer, err := runtime.Run(ctx, client, protocol, registry, conv, 25, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(answer)
}
