package knowledge_index_pipeline

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/indexer"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/loader"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/logger"
	milvus "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/milvus"
)

func IndexFile(ctx context.Context, cfg *config.Config, path string) error {
	docs, err := loader.LoadMarkdown(ctx, path)
	if err != nil {
		return err
	}
	ix, err := indexer.NewMilvusIndexer(ctx, cfg)
	if err != nil {
		return err
	}
	cli, _, err := milvus.NewClient(ctx, cfg)
	if err != nil {
		return err
	}
	if err = milvus.DeleteBySource(ctx, cli, cfg.Milvus.CollectionName, filepath.Base(path)); err != nil {
		return fmt.Errorf("delete previous document: %w", err)
	}
	ids, err := ix.Store(ctx, docs)
	if err != nil {
		return fmt.Errorf("index document: %w", err)
	}
	logger.L().Info("indexing file done", "path", path, "parts", len(ids))
	return nil
}
