package infrastructure

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/auth/domain"

	"github.com/redis/go-redis/v9"
)

// RedisAdapter 基于 *redis.Client 的 domain.RedisPort 实现。
// 仅封装 Login/Logout 需要的原语。
type RedisAdapter struct {
	client *redis.Client
}

// NewRedisAdapter 创建实现。client 为 nil 时返回 nil（无 Redis 降级）。
func NewRedisAdapter(client *redis.Client) domain.RedisPort {
	if client == nil {
		return nil
	}
	return &RedisAdapter{client: client}
}

// 编译期断言：确保实现满足 domain.RedisPort 接口。
var _ domain.RedisPort = (*RedisAdapter)(nil)

// Get 读取键值，键不存在返回 redis.Nil。
func (a *RedisAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.client.Get(ctx, key).Result()
}

// Set 写入键值并设置 TTL。
func (a *RedisAdapter) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return a.client.Set(ctx, key, value, ttl).Err()
}

// Del 删除键。
func (a *RedisAdapter) Del(ctx context.Context, key string) error {
	return a.client.Del(ctx, key).Err()
}

// Incr 自增并返回新值（键不存在时从 1 开始）。
func (a *RedisAdapter) Incr(ctx context.Context, key string) (int64, error) {
	return a.client.Incr(ctx, key).Result()
}

// Expire 设置键的过期时间。
func (a *RedisAdapter) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return a.client.Expire(ctx, key, ttl).Err()
}

// Exists 键是否存在，返回存在的键个数。
func (a *RedisAdapter) Exists(ctx context.Context, key string) (int64, error) {
	return a.client.Exists(ctx, key).Result()
}
