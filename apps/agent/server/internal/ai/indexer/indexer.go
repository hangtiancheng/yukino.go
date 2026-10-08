// Package indexer provides a Milvus-backed document indexer for the knowledge base.
// Documents are split, embedded, and stored as rows in the Milvus collection with
// their float vector embeddings.
package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	eino_milvus "github.com/cloudwego/eino-ext/components/indexer/milvus"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/embedder"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
	yukino_milvus "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/milvus"
)

// docRow is the row-based record inserted into Milvus. The milvus struct tags
// bind each Go field to its collection field (id / vector / content / metadata).
type docRow struct {
	ID       string    `json:"id" milvus:"name:id"`
	Vector   []float32 `json:"vector" milvus:"name:vector"`
	Content  string    `json:"content" milvus:"name:content"`
	Metadata []byte    `json:"metadata" milvus:"name:metadata"`
}

// NewMilvusIndexer creates an indexer that stores document chunks with their
// float vector embeddings into the Milvus knowledge collection (database
// "agent", collection "biz" by default; auto-provisioned on first connect).
//
// A custom DocumentConverter is supplied so the stored rows:
//   - carry the embedding as a native FloatVector (COSINE metric)
//   - normalize metadata["_source"] to the file basename, keeping the dedup
//     key stable across working directories
//   - truncate content to MaxContentLength, matching the collection schema
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

// documentToRows maps Eino documents plus their embeddings to Milvus rows.
func documentToRows(ctx context.Context, docs []*schema.Document, vectors [][]float64) ([]interface{}, error) {
	if len(docs) != len(vectors) {
		return nil, fmt.Errorf("docs/vectors length mismatch: %d != %d", len(docs), len(vectors))
	}

	rows := make([]interface{}, 0, len(docs))
	for i, doc := range docs {
		if doc.ID == "" {
			return nil, fmt.Errorf("doc id not set")
		}

		// Normalize _source to the basename so dedup keys are stable across
		// different working directories.
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

		content := doc.Content
		if len(content) > consts.MaxContentLength {
			content = content[:consts.MaxContentLength]
		}

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
