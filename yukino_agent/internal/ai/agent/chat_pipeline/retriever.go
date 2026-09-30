package chat_pipeline

import (
	"context"

	"github.com/cloudwego/eino/components/retriever"
	yukino_retriever "github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/retriever"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
)

// newRetriever creates a Redis-backed vector retriever for the chat pipeline.
// It searches the knowledge base for documents relevant to the user's query.
func newRetriever(ctx context.Context, cfg *config.Config) (retriever.Retriever, error) {
	return yukino_retriever.NewRedisRetriever(ctx, cfg)
}
