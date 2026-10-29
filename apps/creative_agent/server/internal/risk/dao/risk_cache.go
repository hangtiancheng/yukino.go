package dao

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Risk-offline blacklist (written after RiskOfflineJob evaluation hits; checked before pre_create submission).
const (
	// RiskBlacklistKeyPrefix is the blacklist key prefix; the full key = risk:blacklist:{userID}:{subProduct}.
	RiskBlacklistKeyPrefix = "risk:blacklist:"
	// RiskBlacklistTTL is the blacklist default validity (evaluation job period > TTL; expiry recovers automatically).
	RiskBlacklistTTL = time.Hour
)

// RiskBlacklistKey builds the blacklist key.
func RiskBlacklistKey(userID int64, subProduct string) string {
	return fmt.Sprintf("%s%d:%s", RiskBlacklistKeyPrefix, userID, subProduct)
}

// SetRiskBlacklist writes the blacklist (idempotent overwrite; repeated evaluations do not write twice).
// When rdb is nil it returns silently (no Redis never blocks evaluation).
func SetRiskBlacklist(ctx context.Context, rdb *redis.Client, userID int64, subProduct string) error {
	if rdb == nil {
		return nil
	}
	return rdb.Set(ctx, RiskBlacklistKey(userID, subProduct), "1", RiskBlacklistTTL).Err()
}

// IsRiskBlacklisted checks whether the blacklist is hit. Returns false (pass) when rdb is nil or the query errors.
func IsRiskBlacklisted(ctx context.Context, rdb *redis.Client, userID int64, subProduct string) bool {
	if rdb == nil {
		return false
	}
	exists, err := rdb.Exists(ctx, RiskBlacklistKey(userID, subProduct)).Result()
	if err != nil {
		return false // pass on Redis errors; risk governance never blocks business
	}
	return exists > 0
}

// Risk cache: SHA-1 digests of audited texts are written to Redis so pre_create skips duplicate audits (stage 5).
//
// Design intent (§5.5): in retry scenarios the same text may be audited repeatedly. Texts that passed risk control have their hash cached
// (TTL 1h); at pre-create time filterCachedTextGroups skips audited content, avoiding duplicate submissions.
// Isolated by userID: the same text may have different context per user, avoiding false judgments.

const (
	// RiskCacheKeyPrefix is the cache key prefix; the full key = risk:passed:{userID}:{sha1(text)}.
	RiskCacheKeyPrefix = "risk:passed:"
	// RiskCacheTTL is the validity of audited-text cache entries.
	RiskCacheTTL = time.Hour
)

// RiskCacheKey builds the cache key of one text.
func RiskCacheKey(userID int64, text string) string {
	return fmt.Sprintf("%s%d:%s", RiskCacheKeyPrefix, userID, sha1Hash(text))
}

// sha1Hash computes the SHA-1 digest of a text (part of the cache key).
func sha1Hash(text string) string {
	h := sha1.Sum([]byte(text))
	return hex.EncodeToString(h[:])
}

// MarkRiskAuditPassed writes the cache after risk control passes: one TTL entry per text segment.
// When rdb is nil it returns nil directly (Redis unavailability never blocks business; the cache is a performance optimization).
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

// FilterCachedTextGroups filters cached text groups and returns the groups needing audit.
// Each element of the input textGroups is one text segment to audit; cached hits are skipped.
// When rdb is nil everything is returned (Redis unavailability submits as usual).
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
