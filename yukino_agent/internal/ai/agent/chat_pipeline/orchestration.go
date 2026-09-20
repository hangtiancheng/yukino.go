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

// Package chat_pipeline implements the RAG-enhanced chat agent pipeline.
// The pipeline combines vector retrieval, prompt templating, and a ReAct agent
// to produce context-aware responses with tool calling capabilities.
//
// Pipeline flow:
//
//	Input -> [InputToRag, InputToChat] (parallel)
//	        -> [RedisRetriever] -> [ChatTemplate] -> [ReactAgent] -> Output
package chat_pipeline

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
)

// BuildChatAgent constructs and compiles the chat agent computation graph.
// The graph retrieves relevant documents from Redis, merges them with conversation
// history via a chat template, and passes the result to a ReAct agent for response generation.
func BuildChatAgent(ctx context.Context, cfg *config.Config) (compose.Runnable[*UserMessage, *schema.Message], error) {
	const (
		InputToRag     = "InputToRag"
		ChatTemplate   = "ChatTemplate"
		ReactAgent     = "ReactAgent"
		RedisRetriever = "RedisRetriever"
		InputToChat    = "InputToChat"
	)

	g := compose.NewGraph[*UserMessage, *schema.Message]()

	_ = g.AddLambdaNode(InputToRag, compose.InvokableLambdaWithOption(newInputToRagLambda), compose.WithNodeName("UserMessageToRag"))

	chatTemplate, err := newChatTemplate(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_ = g.AddChatTemplateNode(ChatTemplate, chatTemplate)

	reactAgent, err := newReactAgentLambda(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_ = g.AddLambdaNode(ReactAgent, reactAgent, compose.WithNodeName("ReActAgent"))

	redisRetriever, err := newRetriever(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// Output key "documents" matches the {documents} placeholder in the chat template.
	_ = g.AddRetrieverNode(RedisRetriever, redisRetriever, compose.WithOutputKey("documents"))

	_ = g.AddLambdaNode(InputToChat, compose.InvokableLambdaWithOption(newInputToChatLambda), compose.WithNodeName("UserMessageToChat"))

	// Wire the graph edges.
	_ = g.AddEdge(compose.START, InputToRag)
	_ = g.AddEdge(compose.START, InputToChat)
	_ = g.AddEdge(ReactAgent, compose.END)
	_ = g.AddEdge(InputToRag, RedisRetriever)
	_ = g.AddEdge(RedisRetriever, ChatTemplate)
	_ = g.AddEdge(InputToChat, ChatTemplate)
	_ = g.AddEdge(ChatTemplate, ReactAgent)

	return g.Compile(ctx, compose.WithGraphName("ChatAgent"), compose.WithNodeTriggerMode(compose.AllPredecessor))
}
