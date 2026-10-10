package chat_pipeline

import (
	"context"

	"github.com/cloudwego/eino/components/retriever"
	yukino_retriever "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/retriever"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func newRetriever(ctx context.Context, cfg *config.Config) (retriever.Retriever, error) {
	return yukino_retriever.NewMilvusRetriever(ctx, cfg)
}
