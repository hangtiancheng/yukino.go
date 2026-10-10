package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/agent/chat_pipeline"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/log_callback"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/mem"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	sessionID := "cli-test"

	runner, err := chat_pipeline.BuildChatAgent(ctx, cfg)
	if err != nil {
		log.Fatalf("build chat agent: %v", err)
	}

	firstQuestion := "Hello, what can you help me with?"
	if err := ask(ctx, runner, sessionID, firstQuestion); err != nil {
		log.Fatalf("first turn: %v", err)
	}

	secondQuestion := "What time is it now?"
	if err := ask(ctx, runner, sessionID, secondQuestion); err != nil {
		log.Fatalf("second turn: %v", err)
	}
}

func ask(ctx context.Context, runner compose.Runnable[*chat_pipeline.UserMessage, *schema.Message], sessionID, question string) error {
	userMsg := &chat_pipeline.UserMessage{
		ID:      sessionID,
		Query:   question,
		History: mem.Get(sessionID).All(),
	}

	out, err := runner.Invoke(ctx, userMsg, compose.WithCallbacks(log_callback.NewHandler(nil)))
	if err != nil {
		return err
	}

	fmt.Println("Q:", question)
	fmt.Println("A:", out.Content)
	fmt.Println("----------------")

	mem.Get(sessionID).Append(schema.UserMessage(question))
	mem.Get(sessionID).Append(schema.AssistantMessage(out.Content, nil))
	return nil
}
