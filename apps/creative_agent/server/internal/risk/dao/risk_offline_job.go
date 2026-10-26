package dao

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"github.com/redis/go-redis/v9"
)

// 风控下线评估参数（可调）。
const (
	// RiskOfflineThreshold 单窗口命中阈值：超过则下线。
	RiskOfflineThreshold = 5
	// RiskOfflineWindow 评估窗口：统计最近 N 分钟内的命中。
	RiskOfflineWindow = 10 * time.Minute
)

// RiskOfflineJob 离线评估作业：统计窗口内命中数，达阈值的 (user, subProduct) 写黑名单。
//
// 对齐 spec §5 批量决策段：只读 cg_risk_stats，不写业务表。
// 依赖通过接口注入（domain.RiskStatsRepository / domain.BlacklistWriter），
// 不直接持有 gorm / redis.Client；blacklist 为 nil 时跳过写黑名单（评估不中断）。
type RiskOfflineJob struct {
	stats     domain.RiskStatsRepository
	blacklist domain.BlacklistWriter
}

// NewRiskOfflineJob 创建评估作业。
func NewRiskOfflineJob(stats domain.RiskStatsRepository, blacklist domain.BlacklistWriter) *RiskOfflineJob {
	return &RiskOfflineJob{stats: stats, blacklist: blacklist}
}

// Run 执行一次评估：统计最近 RiskOfflineWindow 内的命中聚合，达阈值写黑名单。
//
// 返回 error 仅表示统计查询失败；blacklist 为 nil 或单组写入失败不中断整体
// （黑名单是治理手段，写入失败不应阻断评估本身）。
func (j *RiskOfflineJob) Run(ctx context.Context) error {
	if j.stats == nil {
		return nil // 未注入统计仓储，跳过（装配缺省场景）
	}
	windowStart := time.Now().Add(-RiskOfflineWindow).Unix()

	// 全量聚合当前窗口内所有 (user, subProduct) 的命中数
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

// RunRiskOfflineJob 一次性的评估执行入口（对齐旧 RunRiskOfflineJob 签名，monitor 兼容）。
//
// 与 RiskOfflineJob.Run 等价；rdb 为 nil 时退化为不写黑名单（评估本身照常）。
func RunRiskOfflineJob(ctx context.Context, stats domain.RiskStatsRepository, rdb *redis.Client) error {
	job := NewRiskOfflineJob(stats, NewRedisBlacklistWriter(rdb))
	return job.Run(ctx)
}

// RedisBlacklistWriter 基于 Redis 的 domain.BlacklistWriter 实现。
//
// 包装 risk_cache.SetRiskBlacklist：rdb 为 nil 时静默跳过（无 Redis 不阻断评估）。
type RedisBlacklistWriter struct {
	rdb *redis.Client
}

// NewRedisBlacklistWriter 创建实现。rdb 为 nil 时 SetBlacklisted 为无操作。
func NewRedisBlacklistWriter(rdb *redis.Client) *RedisBlacklistWriter {
	return &RedisBlacklistWriter{rdb: rdb}
}

// 编译期断言：确保实现满足 domain.BlacklistWriter 接口。
var _ domain.BlacklistWriter = (*RedisBlacklistWriter)(nil)

// SetBlacklisted 写入 userID 在 subProduct 下的黑名单。
func (w *RedisBlacklistWriter) SetBlacklisted(ctx context.Context, userID int64, subProduct string) error {
	return SetRiskBlacklist(ctx, w.rdb, userID, subProduct)
}
