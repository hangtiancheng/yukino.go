package knowledge_index_pipeline

import (
	"context"

	"github.com/cloudwego/eino/components/indexer"
	yukino_indexer "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/indexer"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

// newIndexer creates a Milvus-backed indexer that stores document chunks
// with their vector embeddings into the knowledge base.
func newIndexer(ctx context.Context, cfg *config.Config) (indexer.Indexer, error) {
	return yukino_indexer.NewMilvusIndexer(ctx, cfg)
}
