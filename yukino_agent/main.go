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
