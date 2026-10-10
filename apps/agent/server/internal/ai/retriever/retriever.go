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
