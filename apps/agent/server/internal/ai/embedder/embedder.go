package embedder

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudwego/eino-ext/libs/acl/openai"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

type Embedder struct {
	cli *openai.EmbeddingClient
}

func New(ctx context.Context, cfg *config.Config) (embedding.Embedder, error) {
	encFmt := openai.EmbeddingEncodingFormatFloat
	ecfg := &openai.EmbeddingConfig{
		BaseURL:        cfg.EmbeddingModel.BaseURL,
		APIKey:         cfg.EmbeddingModel.APIKey,
		HTTPClient:     &http.Client{Timeout: 60 * time.Second},
		Model:          cfg.EmbeddingModel.Model,
		EncodingFormat: &encFmt,
	}

	cli, err := openai.NewEmbeddingClient(ctx, ecfg)
	if err != nil {
		return nil, err
	}
	return &Embedder{cli: cli}, nil
}

func (e *Embedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	return e.cli.EmbedStrings(ctx, texts, opts...)
}

func ProbeDimension(ctx context.Context, cfg *config.Config) (int, error) {
	eb, err := New(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("create probe embedder: %w", err)
	}
	vecs, err := eb.EmbedStrings(ctx, []string{"dimension probe"})
	if err != nil {
		return 0, fmt.Errorf("probe embedding: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return 0, fmt.Errorf("probe embedding returned empty vector")
	}
	return len(vecs[0]), nil
}
