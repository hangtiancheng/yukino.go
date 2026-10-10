package knowledge_index_pipeline

import (
	"context"

	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/markdown"
	"github.com/cloudwego/eino/components/document"
	"github.com/google/uuid"
)

func newDocumentTransformer(ctx context.Context) (document.Transformer, error) {
	return markdown.NewHeaderSplitter(ctx, &markdown.HeaderConfig{
		Headers: map[string]string{
			"#": "title",
		},
		TrimHeaders: false,
		IDGenerator: func(ctx context.Context, originalID string, splitIndex int) string {
			return uuid.New().String()
		},
	})
}
