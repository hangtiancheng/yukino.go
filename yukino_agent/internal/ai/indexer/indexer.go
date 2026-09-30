// Package indexer provides a Redis-backed document indexer for the knowledge base.
// Documents are split, embedded, and stored as Redis hashes with their vector embeddings.
package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	eino_redis "github.com/cloudwego/eino-ext/components/indexer/redis"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/embedder"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/consts"
	yukino_redis "github.com/hangtiancheng/yukino.go/yukino_agent/internal/utility/redis"
)

// NewRedisIndexer creates an indexer that stores document chunks with their vector
// embeddings into Redis. A custom DocumentToHashes is supplied so the stored
// hash fields align with the Next.js schema:
//   - "vector"   <- embedding of content (EmbedKey)
//   - "content"  <- raw chunk content
//   - "_source"  <- basename of the source file path (TAG, used for dedup)
//   - "metadata" <- JSON-serialized full metadata
//   - "created_at" <- RFC3339 timestamp
//
// Using basename for _source (instead of the Eino loader's full URI) keeps the
// dedup key stable across different working directories and matches the
// Next.js lib/ai/loader.ts behavior.
func NewRedisIndexer(ctx context.Context, cfg *config.Config) (indexer.Indexer, error) {
	client, err := yukino_redis.NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	eb, err := embedder.New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return eino_redis.NewIndexer(ctx, &eino_redis.IndexerConfig{
		Client:           client,
		KeyPrefix:        consts.RedisKeyPrefix,
		Embedding:        eb,
		BatchSize:        10,
		DocumentToHashes: documentToHashes,
	})
}

// documentToHashes maps an Eino document to the Redis hash fields stored by the
// indexer. The content field carries an EmbedKey so the Eino indexer vectorizes
// it and stores the embedding under the "vector" field.
func documentToHashes(ctx context.Context, doc *schema.Document) (*eino_redis.Hashes, error) {
	if doc.ID == "" {
		return nil, fmt.Errorf("doc id not set")
	}

	// Use basename for _source so dedup keys are stable and match Next.js.
	rawSource, _ := doc.MetaData[consts.RedisSourceField].(string)
	source := rawSource
	if rawSource != "" {
		source = filepath.Base(rawSource)
	}

	metaJSON, _ := json.Marshal(doc.MetaData)

	// Truncate content to MaxContentLength to match the Next.js indexer cap and
	// avoid oversized Redis values. Embedding is computed on the truncated text.
	content := doc.Content
	if len(content) > consts.MaxContentLength {
		content = content[:consts.MaxContentLength]
	}

	field2Value := map[string]eino_redis.FieldValue{
		consts.RedisContentField: {
			Value:    content,
			EmbedKey: consts.RedisVectorField, // embedding stored under "vector"
		},
		consts.RedisSourceField: {Value: source},
		"metadata":              {Value: string(metaJSON)},
		"created_at":            {Value: time.Now().UTC().Format(time.RFC3339)},
	}

	return &eino_redis.Hashes{
		Key:         doc.ID,
		Field2Value: field2Value,
	}, nil
}
