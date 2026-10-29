package infrastructure

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/auth/domain"

	"github.com/redis/go-redis/v9"
)

// RedisAdapter is the *redis.Client-based domain.RedisPort implementation.
// It wraps only the primitives Login/Logout need.
type RedisAdapter struct {
	client *redis.Client
}

// NewRedisAdapter creates the implementation. Returns nil when client is nil (no-Redis degradation).
func NewRedisAdapter(client *redis.Client) domain.RedisPort {
	if client == nil {
		return nil
	}
	return &RedisAdapter{client: client}
}

// Compile-time assertion: the implementation satisfies the domain.RedisPort interface.
var _ domain.RedisPort = (*RedisAdapter)(nil)

// Get reads a key value; returns redis.Nil when the key does not exist.
func (a *RedisAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.client.Get(ctx, key).Result()
}

// Set writes a key value with a TTL.
func (a *RedisAdapter) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return a.client.Set(ctx, key, value, ttl).Err()
}

// Del deletes a key.
func (a *RedisAdapter) Del(ctx context.Context, key string) error {
	return a.client.Del(ctx, key).Err()
}

// Incr increments and returns the new value (starting from 1 when the key does not exist).
func (a *RedisAdapter) Incr(ctx context.Context, key string) (int64, error) {
	return a.client.Incr(ctx, key).Result()
}

// Expire sets the key's expiry.
func (a *RedisAdapter) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return a.client.Expire(ctx, key, ttl).Err()
}

// Exists checks whether keys exist, returning the count of existing keys.
func (a *RedisAdapter) Exists(ctx context.Context, key string) (int64, error) {
	return a.client.Exists(ctx, key).Result()
}
