package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"unicode/utf8"

	eino_milvus "github.com/cloudwego/eino-ext/components/indexer/milvus"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/embedder"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
	yukino_milvus "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/milvus"
)

type docRow struct {
	ID       string    `json:"id" milvus:"name:id"`
	Vector   []float32 `json:"vector" milvus:"name:vector"`
	Content  string    `json:"content" milvus:"name:content"`
	Metadata []byte    `json:"metadata" milvus:"name:metadata"`
}

func NewMilvusIndexer(ctx context.Context, cfg *config.Config) (indexer.Indexer, error) {
	cli, dim, err := yukino_milvus.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	eb, err := embedder.New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return eino_milvus.NewIndexer(ctx, &eino_milvus.IndexerConfig{
		Client:            cli,
		Collection:        cfg.Milvus.CollectionName,
		Fields:            yukino_milvus.Fields(dim),
		MetricType:        eino_milvus.COSINE,
		DocumentConverter: documentToRows,
		Embedding:         eb,
	})
}

func documentToRows(ctx context.Context, docs []*schema.Document, vectors [][]float64) ([]interface{}, error) {
	if len(docs) != len(vectors) {
		return nil, fmt.Errorf("docs/vectors length mismatch: %d != %d", len(docs), len(vectors))
	}

	rows := make([]interface{}, 0, len(docs))
	for i, doc := range docs {
		if doc.ID == "" {
			return nil, fmt.Errorf("doc id not set")
		}

		meta := make(map[string]any, len(doc.MetaData))
		for k, v := range doc.MetaData {
			meta[k] = v
		}
		if raw, _ := meta[consts.MilvusSourceKey].(string); raw != "" {
			meta[consts.MilvusSourceKey] = filepath.Base(raw)
		}
		metaJSON, err := json.Marshal(meta)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata of doc %s: %w", doc.ID, err)
		}

		content := truncateToRuneBoundary(doc.Content, consts.MaxContentLength)

		vec := make([]float32, len(vectors[i]))
		for j, v := range vectors[i] {
			vec[j] = float32(v)
		}

		rows = append(rows, &docRow{
			ID:       doc.ID,
			Vector:   vec,
			Content:  content,
			Metadata: metaJSON,
		})
	}
	return rows, nil
}

// truncateToRuneBoundary cuts s to at most maxBytes bytes without splitting a
// multi-byte UTF-8 rune. Knowledge chunks are mostly Chinese text, and a plain
// byte cut would leave an invalid UTF-8 sequence at the end of the truncated
// content stored in Milvus.
func truncateToRuneBoundary(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
