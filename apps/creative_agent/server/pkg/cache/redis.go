package cache

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

// NewRedisClient 创建 Redis 客户端，并通过 Ping 验证连接。
// 同时注册 OTel 追踪 hook（使用全局 tracer provider）。
func NewRedisClient(cfg config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr(),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	// OTel 追踪（全局 provider 由 otelx.Init 设置；未设置时 redisotel 内部 fallback no-op）。
	if err := redisotel.InstrumentTracing(client); err != nil {
		_ = err // 追踪失败不阻断连接
	}

	return client, nil
}
