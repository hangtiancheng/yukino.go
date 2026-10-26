package dao

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"github.com/redis/go-redis/v9"
)

// RedisBlacklistChecker 基于 Redis 的 domain.BlacklistChecker 实现。
//
// 包装 risk_cache.IsRiskBlacklisted：查询出错时放行（不因风控治理阻断业务）；
// rdb 为 nil 时同样放行（未接入 Redis 的黑名单检查不生效）。
type RedisBlacklistChecker struct {
	rdb *redis.Client
}

// NewRedisBlacklistChecker 创建实现。
func NewRedisBlacklistChecker(rdb *redis.Client) *RedisBlacklistChecker {
	return &RedisBlacklistChecker{rdb: rdb}
}

// 编译期断言：确保实现满足 domain.BlacklistChecker 接口。
var _ domain.BlacklistChecker = (*RedisBlacklistChecker)(nil)

// IsBlacklisted 查询 userID 在 subProduct 下是否命中黑名单。
func (c *RedisBlacklistChecker) IsBlacklisted(ctx context.Context, userID int64, subProduct string) bool {
	return IsRiskBlacklisted(ctx, c.rdb, userID, subProduct)
}
