package monitor

import (
	"context"
	"time"

	riskinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	riskdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RiskOfflineJobInterval is the risk-governance evaluation interval: run every 5 minutes.
const RiskOfflineJobInterval = 5 * time.Minute

// RunRiskOfflineJobSafely runs the evaluation job wrapped in a recover layer.
//
// Aligned with the old creation.RunRiskOfflineJob: count hits within the window; (user, subProduct) pairs reaching the threshold
// are written to the Redis blacklist; when rdb is nil it degrades to not writing the blacklist (evaluation itself proceeds as usual).
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
