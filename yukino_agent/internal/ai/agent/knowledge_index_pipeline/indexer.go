package knowledge_index_pipeline

import (
	"context"

	"github.com/cloudwego/eino/components/indexer"
	yukino_indexer "github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/indexer"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
)

// newIndexer creates a Redis-backed indexer that stores document chunks
// with their vector embeddings into the knowledge base.
func newIndexer(ctx context.Context, cfg *config.Config) (indexer.Indexer, error) {
	return yukino_indexer.NewRedisIndexer(ctx, cfg)
}
