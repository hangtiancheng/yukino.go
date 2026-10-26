package domain

import "context"

// RiskStatsRow 窗口聚合查询结果。
type RiskStatsRow struct {
	UserID     int64
	SubProduct string
	HitCount   int
}

// RiskStatsRepository 风控命中统计仓储接口。
type RiskStatsRepository interface {
	// UpsertIncrement 对 (user, subProduct, riskType, windowStart) 累加 hit_count +1（UNIQUE 键幂等）。
	UpsertIncrement(ctx context.Context, userID int64, subProduct, riskType string, windowStart int64) error
	// SumByUserAndSubProduct 统计窗口内累计命中次数。
	SumByUserAndSubProduct(ctx context.Context, userID int64, subProduct string, windowStart int64) (int, error)
	// SumAllInWindow 聚合窗口内所有 (user, subProduct) 命中数。
	SumAllInWindow(ctx context.Context, windowStart int64) ([]RiskStatsRow, error)
	// MinWindowStart 返回最老窗口起点（测试/对账）。
	MinWindowStart(ctx context.Context) (int64, error)
}
