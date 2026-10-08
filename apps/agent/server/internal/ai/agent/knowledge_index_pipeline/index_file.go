package knowledge_index_pipeline

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/compose"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/loader"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/log_callback"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/logger"
	yukino_milvus "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/milvus"
)

// IndexFile indexes a single document file into the Milvus knowledge base.
// Before indexing, it removes any existing documents that share the same
// "_source" metadata value, ensuring re-indexing a file does not produce
// duplicate entries.
//
// This function is shared between the HTTP file-upload handler and the
// batch knowledge-indexing CLI to avoid logic duplication.
func IndexFile(ctx context.Context, cfg *config.Config, path string) error {
	runner, err := BuildKnowledgeIndexing(ctx, cfg)
	if err != nil {
		return err
	}

	// Load the document to obtain its metadata for deduplication.
	ldr, err := loader.NewFileLoader(ctx)
	if err != nil {
		return err
	}
	docs, err := ldr.Load(ctx, document.Source{URI: path})
	if err != nil {
		return err
	}

	// Connect to Milvus and remove existing documents with the same source.
	cli, _, err := yukino_milvus.NewClient(ctx, cfg)
	if err != nil {
		return err
	}

	source := ""
	if len(docs) > 0 {
		source, _ = docs[0].MetaData[consts.MilvusSourceKey].(string)
	}
	if source != "" {
		// Use basename to match the _source value written by the indexer
		// (see indexer.documentToRows) so dedup keys are stable across
		// different working directories.
		source = filepath.Base(source)
	}
	if err := yukino_milvus.DeleteBySource(ctx, cli, cfg.Milvus.CollectionName, source); err != nil {
		logger.L().Warn("delete existing data failed", "err", err)
	}

	// Index the new document through the pipeline.
	ids, err := runner.Invoke(ctx, document.Source{URI: path}, compose.WithCallbacks(log_callback.NewHandler(nil)))
	if err != nil {
		return fmt.Errorf("invoke index graph: %w", err)
	}
	logger.L().Info("indexing file done", "path", path, "parts", len(ids))
	return nil
}
