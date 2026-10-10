package knowledge_index_pipeline

import (
	"context"

	"github.com/cloudwego/eino-ext/components/document/loader/file"
	"github.com/cloudwego/eino/components/document"
)

func newLoader(ctx context.Context) (document.Loader, error) {
	return file.NewFileLoader(ctx, &file.FileLoaderConfig{})
}
