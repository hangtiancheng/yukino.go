package domain

import "context"

// RiskStatsRow is a window-aggregation query result.
type RiskStatsRow struct {
	UserID     int64
	SubProduct string
	HitCount   int
}

// RiskStatsRepository is the risk-hit statistics repository interface.
type RiskStatsRepository interface {
	// UpsertIncrement increments hit_count +1 for (user, subProduct, riskType, windowStart) (idempotent via the UNIQUE key).
	UpsertIncrement(ctx context.Context, userID int64, subProduct, riskType string, windowStart int64) error
	// SumByUserAndSubProduct sums the accumulated hits within the window.
	SumByUserAndSubProduct(ctx context.Context, userID int64, subProduct string, windowStart int64) (int, error)
	// SumAllInWindow aggregates the hits of all (user, subProduct) pairs within the window.
	SumAllInWindow(ctx context.Context, windowStart int64) ([]RiskStatsRow, error)
	// MinWindowStart returns the oldest window start (tests/reconciliation).
	MinWindowStart(ctx context.Context) (int64, error)
}
