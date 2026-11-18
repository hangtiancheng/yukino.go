package main

import (
	"context"
	"fmt"
	"github.com/hangtiancheng/yukino-code/apps/yukino/conversation"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/agent/chat_pipeline"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"log"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatal(err)
	}
	var history []conversation.Message
	for _, question := range []string{"Hello, what can you help me with?", "What time is it now?"} {
		answer, err := chat_pipeline.Run(context.Background(), cfg, question, history, nil)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("Q:", question)
		fmt.Println("A:", answer)
		history = append(history, conversation.Message{Role: "user", Content: question}, conversation.Message{Role: "assistant", Content: answer})
	}
}
