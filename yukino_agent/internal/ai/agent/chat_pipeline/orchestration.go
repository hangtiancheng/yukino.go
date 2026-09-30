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
