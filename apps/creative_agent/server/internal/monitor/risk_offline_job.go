package monitor

import (
	"context"
	"time"

	riskinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	riskdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RiskOfflineJobInterval 风控治理评估周期：每 5 分钟跑一次。
const RiskOfflineJobInterval = 5 * time.Minute

// RunRiskOfflineJobSafely 包一层 recover 的评估 job 执行。
//
// 对齐旧 creation.RunRiskOfflineJob：统计窗口内命中数，达阈值的 (user, subProduct)
// 写 Redis 黑名单；rdb 为 nil 时退化为不写黑名单（评估本身照常）。
func RunRiskOfflineJobSafely(ctx context.Context, stats riskdomain.RiskStatsRepository, rdb *redis.Client, logger *zap.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("risk offline job panic", zap.Any("panic", r))
		}
	}()
	if err := riskinfra.RunRiskOfflineJob(ctx, stats, rdb); err != nil {
		logger.Error("risk offline job failed", zap.Error(err))
	}
}
