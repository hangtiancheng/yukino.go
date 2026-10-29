package dao

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"github.com/redis/go-redis/v9"
)

// Risk-offline evaluation parameters (tunable).
const (
	// RiskOfflineThreshold is the single-window hit threshold: exceeding it offlines the user.
	RiskOfflineThreshold = 5
	// RiskOfflineWindow is the evaluation window: count hits within the last N minutes.
	RiskOfflineWindow = 10 * time.Minute
)

// RiskOfflineJob is the offline evaluation job: count hits within the window; (user, subProduct) pairs reaching the threshold are written to the blacklist.
//
// Aligned with spec §5's batch-decision section: it only reads cg_risk_stats and writes no business tables.
// Dependencies are interface-injected (domain.RiskStatsRepository / domain.BlacklistWriter);
// it holds no gorm / redis.Client directly; when blacklist is nil, blacklist writes are skipped (evaluation is not interrupted).
type RiskOfflineJob struct {
	stats     domain.RiskStatsRepository
	blacklist domain.BlacklistWriter
}

// NewRiskOfflineJob creates the evaluation job.
func NewRiskOfflineJob(stats domain.RiskStatsRepository, blacklist domain.BlacklistWriter) *RiskOfflineJob {
	return &RiskOfflineJob{stats: stats, blacklist: blacklist}
}

// Run performs one evaluation: aggregate hits within the last RiskOfflineWindow; write the blacklist when the threshold is reached.
//
// A returned error only means the statistics query failed; a nil blacklist or one group's write failure does not interrupt the whole
// (the blacklist is a governance tool; write failures must not block the evaluation itself).
func (j *RiskOfflineJob) Run(ctx context.Context) error {
	if j.stats == nil {
		return nil // no statistics repository injected; skip (default assembly scenario)
	}
	windowStart := time.Now().Add(-RiskOfflineWindow).Unix()

	// Aggregate the hits of all (user, subProduct) pairs within the current window
	rows, err := j.stats.SumAllInWindow(ctx, windowStart)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.HitCount >= RiskOfflineThreshold {
			if j.blacklist != nil {
				if err := j.blacklist.SetBlacklisted(ctx, row.UserID, row.SubProduct); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// RunRiskOfflineJob is the one-shot evaluation entry (aligned with the old RunRiskOfflineJob signature for monitor compatibility).
//
// Equivalent to RiskOfflineJob.Run; when rdb is nil it degrades to not writing the blacklist (evaluation proceeds as usual).
func RunRiskOfflineJob(ctx context.Context, stats domain.RiskStatsRepository, rdb *redis.Client) error {
	job := NewRiskOfflineJob(stats, NewRedisBlacklistWriter(rdb))
	return job.Run(ctx)
}

// RedisBlacklistWriter is the Redis-based domain.BlacklistWriter implementation.
//
// It wraps risk_cache.SetRiskBlacklist: rdb nil skips silently (no Redis never blocks evaluation).
type RedisBlacklistWriter struct {
	rdb *redis.Client
}

// NewRedisBlacklistWriter creates the implementation. When rdb is nil, SetBlacklisted is a no-op.
func NewRedisBlacklistWriter(rdb *redis.Client) *RedisBlacklistWriter {
	return &RedisBlacklistWriter{rdb: rdb}
}

// Compile-time assertion: the implementation satisfies the domain.BlacklistWriter interface.
var _ domain.BlacklistWriter = (*RedisBlacklistWriter)(nil)

// SetBlacklisted writes the blacklist entry of userID under subProduct.
func (w *RedisBlacklistWriter) SetBlacklisted(ctx context.Context, userID int64, subProduct string) error {
	return SetRiskBlacklist(ctx, w.rdb, userID, subProduct)
}
