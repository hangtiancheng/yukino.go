package domain

import (
	"context"
	"time"
)

// RedisPort 抽象登录失败计数与 token 黑名单所需的 Redis 原语。
//
// 仅暴露 Login/Logout 需要的操作；应用层依赖本接口而非 *redis.Client，
// 便于测试 mock 与无 Redis 降级。实现见 infrastructure.RedisAdapter。
type RedisPort interface {
	// Get 读取键值，键不存在返回 redis.Nil。
	Get(ctx context.Context, key string) (string, error)
	// Set 写入键值并设置 TTL。
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	// Del 删除键。
	Del(ctx context.Context, key string) error
	// Incr 自增并返回新值（键不存在时从 1 开始）。
	Incr(ctx context.Context, key string) (int64, error)
	// Expire 设置键的过期时间。
	Expire(ctx context.Context, key string, ttl time.Duration) error
	// Exists 键是否存在，返回存在的键个数。
	Exists(ctx context.Context, key string) (int64, error)
}
