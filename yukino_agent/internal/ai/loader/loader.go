// Package loader provides a file-based document loader for the knowledge indexing pipeline.
// It reads documents from the local filesystem and converts them into the Eino document format.
package loader

import (
	"context"

	"github.com/cloudwego/eino-ext/components/document/loader/file"
	"github.com/cloudwego/eino/components/document"
)

// NewFileLoader creates a file loader that can read documents from the local filesystem.
// Supported formats include plain text and Markdown files.
func NewFileLoader(ctx context.Context) (document.Loader, error) {
	return file.NewFileLoader(ctx, &file.FileLoaderConfig{})
}
