package chat_pipeline

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

// newChatModel creates the LLM chat model instance used by the ReAct agent.
// It uses the quick chat model configuration for fast response times.
func newChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	return models.NewQuickChatModel(ctx, cfg)
}
