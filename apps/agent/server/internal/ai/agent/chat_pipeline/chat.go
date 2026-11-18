package chat_pipeline

import (
	"context"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino-code/apps/yukino/conversation"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/retriever"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/runtime"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func Run(ctx context.Context, cfg *config.Config, question string, history []conversation.Message, emit func(string)) (string, error) {
	rr, err := retriever.NewMilvusRetriever(ctx, cfg)
	if err != nil {
		return "", err
	}
	docs, err := rr.Retrieve(ctx, question)
	if err != nil {
		return "", err
	}
	var contextDocs strings.Builder
	for _, doc := range docs {
		contextDocs.WriteString(doc.Content)
		contextDocs.WriteString("\n")
	}
	system := strings.NewReplacer("{date}", time.Now().Format(time.DateTime), "{documents}", contextDocs.String()).Replace(buildSystemPrompt(cfg))
	client, protocol, err := models.New(cfg, cfg.QuickChatModel, system)
	if err != nil {
		return "", err
	}
	registry, err := tools.NewRegistry(ctx, cfg)
	if err != nil {
		return "", err
	}
	defer registry.Dispose()
	conv := conversation.NewManager()
	conv.AppendMessages(history)
	conv.AddUserMessage(question)
	return runtime.Run(ctx, client, protocol, registry, conv, 25, emit)
}
