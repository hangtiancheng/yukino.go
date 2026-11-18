package embedder

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type Embedder struct {
	client openai.Client
	model  string
}

func New(ctx context.Context, cfg *config.Config) (*Embedder, error) {
	if cfg.EmbeddingModel.Provider != "openai" {
		return nil, fmt.Errorf("unsupported embedding provider %q", cfg.EmbeddingModel.Provider)
	}
	opts := []option.RequestOption{option.WithAPIKey(cfg.EmbeddingModel.APIKey), option.WithHTTPClient(&http.Client{Timeout: 60 * time.Second})}
	if cfg.EmbeddingModel.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.EmbeddingModel.BaseURL))
	}
	return &Embedder{client: openai.NewClient(opts...), model: cfg.EmbeddingModel.Model}, nil
}
func (e *Embedder) EmbedStrings(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	result, err := e.client.Embeddings.New(ctx, openai.EmbeddingNewParams{Model: openai.EmbeddingModel(e.model), Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts}, EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat})
	if err != nil {
		return nil, err
	}
	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("embedding count %d != input count %d", len(result.Data), len(texts))
	}
	vectors := make([][]float64, len(texts))
	for _, item := range result.Data {
		i := int(item.Index)
		if i < 0 || i >= len(vectors) || len(vectors[i]) > 0 || len(item.Embedding) == 0 {
			return nil, fmt.Errorf("invalid embedding at index %d", i)
		}
		vectors[i] = item.Embedding
	}
	return vectors, nil
}
func ProbeDimension(ctx context.Context, cfg *config.Config) (int, error) {
	eb, err := New(ctx, cfg)
	if err != nil {
		return 0, err
	}
	vecs, err := eb.EmbedStrings(ctx, []string{"dimension probe"})
	if err != nil {
		return 0, err
	}
	return len(vecs[0]), nil
}
