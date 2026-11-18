package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"unicode/utf8"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/document"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/embedder"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
	yukino_milvus "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/milvus"
	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
)

type docRow struct {
	ID       string    `json:"id" milvus:"name:id"`
	Vector   []float32 `json:"vector" milvus:"name:vector"`
	Content  string    `json:"content" milvus:"name:content"`
	Metadata []byte    `json:"metadata" milvus:"name:metadata"`
}

func NewMilvusIndexer(ctx context.Context, cfg *config.Config) (*Indexer, error) {
	cli, dim, err := yukino_milvus.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	eb, err := embedder.New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return &Indexer{cli: cli, collection: cfg.Milvus.CollectionName, embedder: eb, dimension: dim}, nil
}

type Indexer struct {
	cli        milvusclient.Client
	collection string
	embedder   *embedder.Embedder
	dimension  int
}

func (i *Indexer) Store(ctx context.Context, docs []*document.Document) ([]string, error) {
	if len(docs) == 0 {
		return []string{}, nil
	}
	texts := make([]string, len(docs))
	ids := make([]string, len(docs))
	for n, doc := range docs {
		if doc == nil {
			return nil, fmt.Errorf("nil document")
		}
		texts[n] = doc.Content
		ids[n] = doc.ID
	}
	vectors, err := i.embedder.EmbedStrings(ctx, texts)
	if err != nil {
		return nil, err
	}
	for _, v := range vectors {
		if len(v) != i.dimension {
			return nil, fmt.Errorf("embedding dimension %d != %d", len(v), i.dimension)
		}
	}
	rows, err := documentToRows(ctx, docs, vectors)
	if err != nil {
		return nil, err
	}
	if _, err = i.cli.InsertRows(ctx, i.collection, "", rows); err != nil {
		return nil, err
	}
	if err = i.cli.Flush(ctx, i.collection, false); err != nil {
		return nil, err
	}
	return ids, nil
}

func documentToRows(ctx context.Context, docs []*document.Document, vectors [][]float64) ([]interface{}, error) {
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
