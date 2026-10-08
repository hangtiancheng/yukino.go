// Package embedder provides factory functions for creating text embedding models.
// The embeddings are used to vectorize documents for storage and retrieval in Milvus.
//
// Embeddings are produced over the OpenAI-compatible /v1/embeddings protocol via
// the eino-ext libs/acl/openai client, so any OpenAI-compatible endpoint works
// (OpenAI itself, Alibaba DashScope compatible-mode, a local vLLM/Ollama gateway,
// and so on) by pointing base_url at it.
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

// Embedder wraps the OpenAI-compatible embedding client. It implements
// embedding.Embedder so it can be used by Eino indexers and retrievers.
type Embedder struct {
	cli *openai.EmbeddingClient
}

// New creates a text embedding model for the configured OpenAI-compatible
// endpoint.
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

// EmbedStrings returns the embeddings for the given texts.
func (e *Embedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	return e.cli.EmbedStrings(ctx, texts, opts...)
}

// ProbeDimension returns the actual vector dimension produced by the configured
// embedding provider. It creates a temporary embedder, embeds a short probe
// string, and measures the output length. This is authoritative over any static
// config value — the model's real output determines the Milvus collection DIM.
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
