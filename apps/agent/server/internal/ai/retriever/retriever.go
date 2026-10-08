// Package retriever provides a Milvus-backed vector retriever for RAG (Retrieval-Augmented Generation).
// It runs an ANN search over the knowledge collection using embedding-based COSINE similarity.
package retriever

import (
	"context"
	"strings"

	eino_milvus "github.com/cloudwego/eino-ext/components/retriever/milvus"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/embedder"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
	yukino_milvus "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/milvus"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// NewMilvusRetriever creates a retriever that searches the Milvus knowledge
// collection using KNN vector similarity search. It returns the top-1 most
// relevant document for each query.
//
// The search runs with COSINE metric over native FloatVector fields and an
// AUTOINDEX search param (no radius/range_filter, which the Eino defaults
// would set from the collection dim and silently filter out every COSINE
// result). The stored metadata JSON is parsed back into each document's
// MetaData by the component's default converter, so downstream consumers
// (e.g. the query_internal_docs tool) see the original key/value pairs.
//
// An empty knowledge base degrades to an empty document list instead of an
// error (see emptyTolerantRetriever).
func NewMilvusRetriever(ctx context.Context, cfg *config.Config) (retriever.Retriever, error) {
	cli, _, err := yukino_milvus.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	eb, err := embedder.New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	sp, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil, err
	}

	r, err := eino_milvus.NewRetriever(ctx, &eino_milvus.RetrieverConfig{
		Client:      cli,
		Collection:  cfg.Milvus.CollectionName,
		VectorField: consts.MilvusVectorField,
		OutputFields: []string{
			consts.MilvusIDField,
			consts.MilvusContentField,
			consts.MilvusMetadataField,
		},
		MetricType:      entity.COSINE,
		Sp:              sp,
		TopK:            1,
		VectorConverter: floatVectorConverter,
		Embedding:       eb,
	})
	if err != nil {
		return nil, err
	}
	return &emptyTolerantRetriever{inner: r}, nil
}

// floatVectorConverter converts embeddings to entity.FloatVector, matching the
// FloatVector schema of the collection. The Eino component default packs float
// bytes into a BinaryVector, which does not match this collection's schema.
func floatVectorConverter(ctx context.Context, vectors [][]float64) ([]entity.Vector, error) {
	out := make([]entity.Vector, 0, len(vectors))
	for _, v := range vectors {
		fv := make(entity.FloatVector, len(v))
		for i, x := range v {
			fv[i] = float32(x)
		}
		out = append(out, fv)
	}
	return out, nil
}

// emptyTolerantRetriever maps the Eino Milvus retriever's "no results found"
// error — raised whenever the collection holds no rows yet — to an empty
// document list. Without it, chatting before the first document is indexed
// would fail the whole pipeline with a "search result has error".
type emptyTolerantRetriever struct {
	inner retriever.Retriever
}

func (r *emptyTolerantRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	docs, err := r.inner.Retrieve(ctx, query, opts...)
	if err != nil && strings.Contains(err.Error(), "no results found") {
		return []*schema.Document{}, nil
	}
	return docs, err
}
