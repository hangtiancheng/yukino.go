package retriever

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/document"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/embedder"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
	milvus "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/milvus"
	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

type Retriever struct {
	cli        milvusclient.Client
	collection string
	embedder   *embedder.Embedder
	dimension  int
}

func NewMilvusRetriever(ctx context.Context, cfg *config.Config) (*Retriever, error) {
	cli, dim, err := milvus.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	eb, err := embedder.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Retriever{cli: cli, collection: cfg.Milvus.CollectionName, embedder: eb, dimension: dim}, nil
}
func (r *Retriever) Retrieve(ctx context.Context, query string) ([]*document.Document, error) {
	vectors, err := r.embedder.EmbedStrings(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors[0]) != r.dimension {
		return nil, fmt.Errorf("embedding dimension %d != %d", len(vectors[0]), r.dimension)
	}
	vs, err := floatVectorConverter(ctx, vectors)
	if err != nil {
		return nil, err
	}
	sp, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil, err
	}
	results, err := r.cli.Search(ctx, r.collection, nil, "", []string{consts.MilvusContentField, consts.MilvusMetadataField}, vs, consts.MilvusVectorField, entity.COSINE, 1, sp)
	if err != nil {
		return nil, err
	}
	return searchDocuments(results)
}
func searchDocuments(results []milvusclient.SearchResult) ([]*document.Document, error) {
	docs := make([]*document.Document, 0)
	for _, result := range results {
		if result.Err != nil {
			return nil, result.Err
		}
		for n := 0; n < result.ResultCount; n++ {
			id, err := result.IDs.GetAsString(n)
			if err != nil {
				return nil, err
			}
			doc := &document.Document{ID: id, MetaData: map[string]any{}}
			for _, field := range result.Fields {
				switch field.Name() {
				case consts.MilvusContentField:
					doc.Content, err = field.GetAsString(n)
				case consts.MilvusMetadataField:
					var value any
					value, err = field.Get(n)
					if err == nil {
						raw, ok := value.([]byte)
						if !ok {
							return nil, fmt.Errorf("unexpected metadata type %T", value)
						}
						err = json.Unmarshal(raw, &doc.MetaData)
					}
				}
				if err != nil {
					return nil, err
				}
			}
			docs = append(docs, doc)
		}
	}
	return docs, nil
}
func floatVectorConverter(ctx context.Context, vectors [][]float64) ([]entity.Vector, error) {
	out := make([]entity.Vector, len(vectors))
	for n, v := range vectors {
		fv := make(entity.FloatVector, len(v))
		for j, x := range v {
			fv[j] = float32(x)
		}
		out[n] = fv
	}
	return out, nil
}
