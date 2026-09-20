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

// Command chat runs an interactive test of the RAG-enhanced chat agent pipeline.
// It demonstrates multi-turn conversation with memory by sending two sequential
// queries and printing the agent's responses.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/agent/chat_pipeline"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/utility/log_callback"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/utility/mem"
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

	// First turn.
	firstQuestion := "Hello, what can you help me with?"
	if err := ask(ctx, runner, sessionID, firstQuestion); err != nil {
		log.Fatalf("first turn: %v", err)
	}

	// Second turn (uses conversation memory from the first).
	secondQuestion := "What time is it now?"
	if err := ask(ctx, runner, sessionID, secondQuestion); err != nil {
		log.Fatalf("second turn: %v", err)
	}
}

// ask sends a single question to the chat agent, prints the response,
// and stores the exchange in conversation memory.
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
