// Package retriever provides a Redis-backed vector retriever for RAG (Retrieval-Augmented Generation).
// It queries the RediSearch index for documents similar to the input query using embedding-based KNN search.
package retriever

import (
	"context"

	eino_redis "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/embedder"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/consts"
	yukino_redis "github.com/hangtiancheng/yukino.go/yukino_agent/internal/utility/redis"
)

// NewRedisRetriever creates a retriever that searches the Redis knowledge base using
// KNN vector similarity search. It returns the top-1 most relevant document for each query.
//
// ReturnFields includes "metadata" so downstream consumers (e.g. the
// query_internal_docs tool) can access the stored metadata. The relevance score
// is NOT returned because the Eino redis retriever hardcodes WithScores=false;
// exposing it would require a custom DocumentConverter or a fork — tracked as a
// known limitation (review Q-5).
func NewRedisRetriever(ctx context.Context, cfg *config.Config) (retriever.Retriever, error) {
	client, err := yukino_redis.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	eb, err := embedder.New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return eino_redis.NewRetriever(ctx, &eino_redis.RetrieverConfig{
		Client:       client,
		Index:        consts.RedisIndexName,
		VectorField:  consts.RedisVectorField, // "vector"
		ReturnFields: []string{consts.RedisContentField, "metadata"},
		TopK:         1,
		Embedding:    eb,
	})
}
