// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Command llm_tool tests tool binding and tool-calling capabilities of the
// configured LLM. It creates a chat model, binds the available tools (MCP log
// tool and current-time tool), and asks the model to describe which tools it
// has access to.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/tools"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()

	// Create the chat model using the quick (fast-response) configuration.
	// This honors the configured model_provider (openai or anthropic).
	chatModel, err := models.NewQuickChatModel(ctx, cfg)
	if err != nil {
		log.Fatalf("create chat model: %v", err)
	}

	// Gather tool definitions to bind to the chat model.
	toolList, err := tools.GetLogMcpTool(ctx, cfg.MCP_URL)
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

	// Bind tools to the chat model, obtaining a new instance with the tools bound.
	chatModel, err = chatModel.WithTools(toolInfos)
	if err != nil {
		log.Fatalf("bind tools: %v", err)
	}

	// Build and compile a simple chain that passes messages to the chat model.
	chain := compose.NewChain[[]*schema.Message, *schema.Message]()
	chain.AppendChatModel(chatModel, compose.WithNodeName("chat_model"))

	agent, err := chain.Compile(ctx)
	if err != nil {
		log.Fatalf("compile chain: %v", err)
	}

	// Run a sample query asking the model about its available tools.
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
