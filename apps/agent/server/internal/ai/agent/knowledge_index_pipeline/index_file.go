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

func IndexFile(ctx context.Context, cfg *config.Config, path string) error {
	runner, err := BuildKnowledgeIndexing(ctx, cfg)
	if err != nil {
		return err
	}

	ldr, err := loader.NewFileLoader(ctx)
	if err != nil {
		return err
	}
	docs, err := ldr.Load(ctx, document.Source{URI: path})
	if err != nil {
		return err
	}

	cli, _, err := yukino_milvus.NewClient(ctx, cfg)
	if err != nil {
		return err
	}

	source := ""
	if len(docs) > 0 {
		source, _ = docs[0].MetaData[consts.MilvusSourceKey].(string)
	}
	if source != "" {
		source = filepath.Base(source)
	}
	if err := yukino_milvus.DeleteBySource(ctx, cli, cfg.Milvus.CollectionName, source); err != nil {
		logger.L().Warn("delete existing data failed", "err", err)
	}

	ids, err := runner.Invoke(ctx, document.Source{URI: path}, compose.WithCallbacks(log_callback.NewHandler(nil)))
	if err != nil {
		return fmt.Errorf("invoke index graph: %w", err)
	}
	logger.L().Info("indexing file done", "path", path, "parts", len(ids))
	return nil
}
