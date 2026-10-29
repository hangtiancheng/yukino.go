package cache

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

// NewRedisClient creates the Redis client and verifies the connection with Ping.
// It also registers OTel tracing hooks (using the global tracer provider).
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

	// OTel tracing (the global provider is set by otelx.Init; when unset, redisotel falls back to no-op internally).
	if err := redisotel.InstrumentTracing(client); err != nil {
		_ = err // tracing failure does not block the connection
	}

	return client, nil
}
