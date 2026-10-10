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

var (
	mu     sync.Mutex
	cached cachedClient
)

type cachedClient struct {
	key string
	cli milvuscli.Client
	dim int
}

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

func cacheKey(mc config.MilvusConfig) (string, error) {
	raw, err := json.Marshal(mc)
	if err != nil {
		return "", fmt.Errorf("marshal milvus config: %w", err)
	}
	return string(raw), nil
}

func bootstrap(ctx context.Context, cfg *config.Config) (milvuscli.Client, int, error) {
	defaultCli, err := milvuscli.NewClient(ctx, milvuscli.Config{
		Address:  cfg.Milvus.Addr,
		Username: cfg.Milvus.Username,
		Password: cfg.Milvus.Password,
		DBName:   "default",
	})
	if err != nil {
		return nil, 0, fmt.Errorf("connect to Milvus at %s: %w", cfg.Milvus.Addr, err)
	}

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

	cli, err := milvuscli.NewClient(ctx, milvuscli.Config{
		Address:  cfg.Milvus.Addr,
		Username: cfg.Milvus.Username,
		Password: cfg.Milvus.Password,
		DBName:   cfg.Milvus.DBName,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("connect to Milvus database %q: %w", cfg.Milvus.DBName, err)
	}

	dim, err := ensureCollection(ctx, cli, cfg)
	if err != nil {
		cli.Close()
		return nil, 0, err
	}
	return cli, dim, nil
}

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

func escapeMilvusString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
