package chat_pipeline

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func BuildChatAgent(ctx context.Context, cfg *config.Config) (compose.Runnable[*UserMessage, *schema.Message], error) {
	const (
		InputToRag      = "InputToRag"
		ChatTemplate    = "ChatTemplate"
		ReactAgent      = "ReactAgent"
		MilvusRetriever = "MilvusRetriever"
		InputToChat     = "InputToChat"
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

	milvusRetriever, err := newRetriever(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_ = g.AddRetrieverNode(MilvusRetriever, milvusRetriever, compose.WithOutputKey("documents"))

	_ = g.AddLambdaNode(InputToChat, compose.InvokableLambdaWithOption(newInputToChatLambda), compose.WithNodeName("UserMessageToChat"))

	_ = g.AddEdge(compose.START, InputToRag)
	_ = g.AddEdge(compose.START, InputToChat)
	_ = g.AddEdge(ReactAgent, compose.END)
	_ = g.AddEdge(InputToRag, MilvusRetriever)
	_ = g.AddEdge(MilvusRetriever, ChatTemplate)
	_ = g.AddEdge(InputToChat, ChatTemplate)
	_ = g.AddEdge(ChatTemplate, ReactAgent)

	return g.Compile(ctx, compose.WithGraphName("ChatAgent"), compose.WithNodeTriggerMode(compose.AllPredecessor))
}
