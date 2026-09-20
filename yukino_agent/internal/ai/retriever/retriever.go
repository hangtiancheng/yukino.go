// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
