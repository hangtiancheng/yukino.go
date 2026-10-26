package dao

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// 风险下线黑名单（RiskOfflineJob 评估命中后写入，pre_create 送审前检查）。
const (
	// RiskBlacklistKeyPrefix 黑名单 key 前缀，完整 key = risk:blacklist:{userID}:{subProduct}。
	RiskBlacklistKeyPrefix = "risk:blacklist:"
	// RiskBlacklistTTL 黑名单默认有效期（评估 job 周期 > TTL，过期自动恢复）。
	RiskBlacklistTTL = time.Hour
)

// RiskBlacklistKey 构造黑名单 key。
func RiskBlacklistKey(userID int64, subProduct string) string {
	return fmt.Sprintf("%s%d:%s", RiskBlacklistKeyPrefix, userID, subProduct)
}

// SetRiskBlacklist 写入黑名单（幂等覆盖，重复评估不重复写）。
// rdb 为 nil 时静默返回（无 Redis 不阻断评估）。
func SetRiskBlacklist(ctx context.Context, rdb *redis.Client, userID int64, subProduct string) error {
	if rdb == nil {
		return nil
	}
	return rdb.Set(ctx, RiskBlacklistKey(userID, subProduct), "1", RiskBlacklistTTL).Err()
}

// IsRiskBlacklisted 查询是否命中黑名单。rdb 为 nil 或查询出错时返回 false（放行）。
func IsRiskBlacklisted(ctx context.Context, rdb *redis.Client, userID int64, subProduct string) bool {
	if rdb == nil {
		return false
	}
	exists, err := rdb.Exists(ctx, RiskBlacklistKey(userID, subProduct)).Result()
	if err != nil {
		return false // Redis 异常放行，不因风控治理阻断业务
	}
	return exists > 0
}

// 风控缓存：已审文本的 SHA-1 摘要写 Redis，pre_create 时跳过重复审核（阶段 5）。
//
// 设计意图（§5.5）：重试场景下同一段文本可能被重复审核。通过风控的文本哈希写缓存
// （TTL 1h），预创建时 filterCachedTextGroups 跳过已审内容，避免重复送审。
// 按 userID 隔离：同一段文本不同用户语境可能不同，避免误判。

const (
	// RiskCacheKeyPrefix 缓存 key 前缀，完整 key = risk:passed:{userID}:{sha1(text)}。
	RiskCacheKeyPrefix = "risk:passed:"
	// RiskCacheTTL 已审文本缓存有效期。
	RiskCacheTTL = time.Hour
)

// RiskCacheKey 构造单个文本的缓存 key。
func RiskCacheKey(userID int64, text string) string {
	return fmt.Sprintf("%s%d:%s", RiskCacheKeyPrefix, userID, sha1Hash(text))
}

// sha1Hash 对文本做 SHA-1 摘要（缓存 key 的一部分）。
func sha1Hash(text string) string {
	h := sha1.Sum([]byte(text))
	return hex.EncodeToString(h[:])
}

// MarkRiskAuditPassed 风控通过后写缓存：为每段文本写一条 TTL 缓存。
// rdb 为 nil 时直接返回 nil（Redis 不可用不阻断业务，缓存是性能优化）。
func MarkRiskAuditPassed(ctx context.Context, rdb *redis.Client, userID int64, texts []string) error {
	if rdb == nil || len(texts) == 0 {
		return nil
	}
	pipe := rdb.Pipeline()
	for _, text := range texts {
		if text == "" {
			continue
		}
		pipe.Set(ctx, RiskCacheKey(userID, text), "1", RiskCacheTTL)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// FilterCachedTextGroups 过滤已缓存的文本组，返回需要送审的组。
// 输入 textGroups 的每个元素是一段待审文本；已命中缓存的跳过。
// rdb 为 nil 时全部返回（Redis 不可用则照常送审）。
func FilterCachedTextGroups(ctx context.Context, rdb *redis.Client, userID int64, textGroups []string) []string {
	if rdb == nil || len(textGroups) == 0 {
		return textGroups
	}
	var needAudit []string
	for _, text := range textGroups {
		if text == "" {
			continue
		}
		exists, err := rdb.Exists(ctx, RiskCacheKey(userID, text)).Result()
		if err != nil || exists == 0 {
			needAudit = append(needAudit, text)
		}
	}
	return needAudit
}
