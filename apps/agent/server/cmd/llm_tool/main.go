package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()

	chatModel, err := models.NewQuickChatModel(ctx, cfg)
	if err != nil {
		log.Fatalf("create chat model: %v", err)
	}

	toolList, err := tools.GetLogMcpTool(ctx, cfg.MCP)
	if err != nil {
		log.Fatalf("get mcp tools: %v", err)
	}
	timeTool, err := tools.NewGetCurrentTimeTool()
	if err != nil {
		log.Fatalf("create get_current_time tool: %v", err)
	}
	toolList = append(toolList, timeTool)

	toolInfos := make([]*schema.ToolInfo, 0, len(toolList))
	for _, t := range toolList {
		info, err := t.Info(ctx)
		if err != nil {
			log.Fatalf("get tool info: %v", err)
		}
		toolInfos = append(toolInfos, info)
	}

	chatModel, err = chatModel.WithTools(toolInfos)
	if err != nil {
		log.Fatalf("bind tools: %v", err)
	}

	chain := compose.NewChain[[]*schema.Message, *schema.Message]()
	chain.AppendChatModel(chatModel, compose.WithNodeName("chat_model"))

	agent, err := chain.Compile(ctx)
	if err != nil {
		log.Fatalf("compile chain: %v", err)
	}

	resp, err := agent.Invoke(ctx, []*schema.Message{
		{
			Role:    schema.User,
			Content: "Tell me what tools you have available.",
		},
	})
	if err != nil {
		log.Fatalf("invoke: %v", err)
	}

	fmt.Println(resp.Content)
}
