package dao

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"github.com/redis/go-redis/v9"
)

// RedisBlacklistChecker is the Redis-based domain.BlacklistChecker implementation.
//
// It wraps risk_cache.IsRiskBlacklisted: query errors pass (risk governance never blocks business);
// rdb nil also passes (blacklist checks without Redis are inactive).
type RedisBlacklistChecker struct {
	rdb *redis.Client
}

// NewRedisBlacklistChecker creates the implementation.
func NewRedisBlacklistChecker(rdb *redis.Client) *RedisBlacklistChecker {
	return &RedisBlacklistChecker{rdb: rdb}
}

// Compile-time assertion: the implementation satisfies the domain.BlacklistChecker interface.
var _ domain.BlacklistChecker = (*RedisBlacklistChecker)(nil)

// IsBlacklisted checks whether userID hits the blacklist under subProduct.
func (c *RedisBlacklistChecker) IsBlacklisted(ctx context.Context, userID int64, subProduct string) bool {
	return IsRiskBlacklisted(ctx, c.rdb, userID, subProduct)
}
