package loader

import (
	"context"

	"github.com/cloudwego/eino-ext/components/document/loader/file"
	"github.com/cloudwego/eino/components/document"
)

func NewFileLoader(ctx context.Context) (document.Loader, error) {
	return file.NewFileLoader(ctx, &file.FileLoaderConfig{})
}
