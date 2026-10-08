// Package milvus provides a client factory for connecting to Milvus standalone.
// It bootstraps the vector knowledge base — database "agent", collection "biz" —
// storing embeddings as native FloatVector with COSINE similarity.
package milvus

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/embedder"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/logger"
	milvuscli "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// Client cache. Bootstrapping (two gRPC connections, an embedding dimension
// probe, collection provisioning and load) is expensive, so the connected
// client is cached per Milvus config — the chat handler rebuilds pipelines per
// request and must not pay the bootstrap cost each time.
var (
	mu     sync.Mutex
	cached cachedClient
)

type cachedClient struct {
	key string
	cli milvuscli.Client
	dim int
}

// NewClient returns a Milvus client connected to the configured database with
// the knowledge collection provisioned (created if missing, recreated when the
// stored vector dimension no longer matches the live embedding provider).
// The second return value is the vector dimension of the collection.
func NewClient(ctx context.Context, cfg *config.Config) (milvuscli.Client, int, error) {
	mu.Lock()
	defer mu.Unlock()

	key, err := cacheKey(cfg.Milvus)
	if err != nil {
		return nil, 0, err
	}
	if cached.cli != nil && cached.key == key {
		return cached.cli, cached.dim, nil
	}

	cli, dim, err := bootstrap(ctx, cfg)
	if err != nil {
		return nil, 0, err
	}
	if cached.cli != nil {
		cached.cli.Close()
	}
	cached = cachedClient{key: key, cli: cli, dim: dim}
	return cli, dim, nil
}

// cacheKey derives a deterministic cache key from the Milvus config.
func cacheKey(mc config.MilvusConfig) (string, error) {
	raw, err := json.Marshal(mc)
	if err != nil {
		return "", fmt.Errorf("marshal milvus config: %w", err)
	}
	return string(raw), nil
}

// bootstrap connects to the default database, ensures the target database
// exists, reconnects to it, and ensures the collection is ready and loaded.
func bootstrap(ctx context.Context, cfg *config.Config) (milvuscli.Client, int, error) {
	// 1. Connect to the default database first: the target database may not
	//    exist yet, and Milvus refuses connections to a missing database.
	defaultCli, err := milvuscli.NewClient(ctx, milvuscli.Config{
		Address:  cfg.Milvus.Addr,
		Username: cfg.Milvus.Username,
		Password: cfg.Milvus.Password,
		DBName:   "default",
	})
	if err != nil {
		return nil, 0, fmt.Errorf("connect to Milvus at %s: %w", cfg.Milvus.Addr, err)
	}

	// 2. Ensure the target database exists.
	if cfg.Milvus.DBName != "default" {
		databases, err := defaultCli.ListDatabases(ctx)
		if err != nil {
			defaultCli.Close()
			return nil, 0, fmt.Errorf("list Milvus databases: %w", err)
		}
		exists := false
		for _, db := range databases {
			if db.Name == cfg.Milvus.DBName {
				exists = true
				break
			}
		}
		if !exists {
			if err := defaultCli.CreateDatabase(ctx, cfg.Milvus.DBName); err != nil {
				defaultCli.Close()
				return nil, 0, fmt.Errorf("create Milvus database %q: %w", cfg.Milvus.DBName, err)
			}
			logger.L().Info("created milvus database", "db", cfg.Milvus.DBName)
		}
	}
	defaultCli.Close()

	// 3. Connect to the target database.
	cli, err := milvuscli.NewClient(ctx, milvuscli.Config{
		Address:  cfg.Milvus.Addr,
		Username: cfg.Milvus.Username,
		Password: cfg.Milvus.Password,
		DBName:   cfg.Milvus.DBName,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("connect to Milvus database %q: %w", cfg.Milvus.DBName, err)
	}

	// 4. Ensure the collection exists, is indexed, and is loaded.
	dim, err := ensureCollection(ctx, cli, cfg)
	if err != nil {
		cli.Close()
		return nil, 0, err
	}
	return cli, dim, nil
}

// ensureCollection provisions the knowledge collection. The vector dimension is
// probed from the live embedding provider (authoritative over any static
// config); if an existing collection carries a different dimension it is
// dropped and recreated so stale vectors can never be matched against the new
// embedding model. Returns the collection's vector dimension.
func ensureCollection(ctx context.Context, cli milvuscli.Client, cfg *config.Config) (int, error) {
	dim, err := embedder.ProbeDimension(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("probe embedding dimension: %w", err)
	}
	logger.L().Info("probed embedding dimension", "dim", dim)

	coll := cfg.Milvus.CollectionName
	has, err := cli.HasCollection(ctx, coll)
	if err != nil {
		return 0, fmt.Errorf("check collection %q: %w", coll, err)
	}

	if has {
		existing, err := cli.DescribeCollection(ctx, coll)
		if err != nil {
			return 0, fmt.Errorf("describe collection %q: %w", coll, err)
		}
		if storedDim, ok := vectorDim(existing.Schema); ok && storedDim != dim {
			logger.L().Warn("milvus collection dimension mismatch; dropping and recreating",
				"stored", storedDim, "probed", dim)
			if err := cli.DropCollection(ctx, coll); err != nil {
				return 0, fmt.Errorf("drop collection on dimension mismatch: %w", err)
			}
			has = false
		}
	}

	if !has {
		schema := entity.NewSchema().
			WithName(coll).
			WithDescription("Business knowledge collection")
		for _, f := range Fields(dim) {
			schema.WithField(f)
		}
		if err := cli.CreateCollection(ctx, schema, entity.DefaultShardNumber,
			milvuscli.WithConsistencyLevel(entity.ClBounded)); err != nil {
			return 0, fmt.Errorf("create collection %q: %w", coll, err)
		}

		idx, err := entity.NewIndexAUTOINDEX(entity.COSINE)
		if err != nil {
			return 0, fmt.Errorf("build vector index: %w", err)
		}
		if err := cli.CreateIndex(ctx, coll, consts.MilvusVectorField, idx, false); err != nil {
			return 0, fmt.Errorf("create vector index: %w", err)
		}
		logger.L().Info("created milvus collection", "collection", coll, "dim", dim)
	}

	// A collection must be loaded before search; LoadCollection is a no-op
	// error-wise when already loaded, so check the load state first.
	state, err := cli.GetLoadState(ctx, coll, nil)
	if err != nil {
		return 0, fmt.Errorf("get load state of %q: %w", coll, err)
	}
	if state != entity.LoadStateLoaded {
		if err := cli.LoadCollection(ctx, coll, false); err != nil {
			return 0, fmt.Errorf("load collection %q: %w", coll, err)
		}
	}
	return dim, nil
}

// Fields builds the collection schema fields for the given vector dimension.
// Shared by the bootstrap and the Eino indexer config so the schema check in
// milvus.NewIndexer always sees exactly the fields the collection was created
// with.
func Fields(dim int) []*entity.Field {
	return []*entity.Field{
		entity.NewField().
			WithName(consts.MilvusIDField).
			WithDescription("the unique id of the document").
			WithIsPrimaryKey(true).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(255),
		entity.NewField().
			WithName(consts.MilvusVectorField).
			WithDescription("the embedding vector of the document content").
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(dim)),
		entity.NewField().
			WithName(consts.MilvusContentField).
			WithDescription("the document content").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(consts.MaxContentLength),
		entity.NewField().
			WithName(consts.MilvusMetadataField).
			WithDescription("the document metadata").
			WithDataType(entity.FieldTypeJSON),
	}
}

// vectorDim extracts the dim type-param of the vector field from a collection
// schema. Returns ok=false when the field is missing or unparsable.
func vectorDim(s *entity.Schema) (int, bool) {
	if s == nil {
		return 0, false
	}
	for _, f := range s.Fields {
		if f.Name != consts.MilvusVectorField {
			continue
		}
		d, err := strconv.Atoi(f.TypeParams["dim"])
		if err != nil {
			return 0, false
		}
		return d, true
	}
	return 0, false
}

// DeleteBySource removes all rows whose metadata["_source"] equals source, so
// re-indexing a file replaces its previous chunks instead of duplicating them.
// Ids are queried first and then deleted in batches to keep expressions small.
func DeleteBySource(ctx context.Context, cli milvuscli.Client, collection, source string) error {
	if source == "" {
		return nil
	}
	expr := fmt.Sprintf(`%s["_source"] == "%s"`, consts.MilvusMetadataField, escapeMilvusString(source))
	resultSet, err := cli.Query(ctx, collection, []string{}, expr, []string{consts.MilvusIDField})
	if err != nil {
		return fmt.Errorf("query existing docs by source: %w", err)
	}

	var ids []string
	for _, column := range resultSet {
		if column.Name() != consts.MilvusIDField {
			continue
		}
		for i := 0; i < column.Len(); i++ {
			id, err := column.GetAsString(i)
			if err == nil {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}

	const batchSize = 500
	for start := 0; start < len(ids); start += batchSize {
		end := min(start+batchSize, len(ids))
		quoted := make([]string, 0, end-start)
		for _, id := range ids[start:end] {
			quoted = append(quoted, `"`+escapeMilvusString(id)+`"`)
		}
		deleteExpr := fmt.Sprintf(`%s in [%s]`, consts.MilvusIDField, strings.Join(quoted, ","))
		if err := cli.Delete(ctx, collection, "", deleteExpr); err != nil {
			return fmt.Errorf("delete docs by source: %w", err)
		}
	}
	logger.L().Info("deleted existing records", "count", len(ids), "source", source)
	return nil
}

// escapeMilvusString escapes a string for use inside a Milvus boolean
// expression double-quoted literal.
func escapeMilvusString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
