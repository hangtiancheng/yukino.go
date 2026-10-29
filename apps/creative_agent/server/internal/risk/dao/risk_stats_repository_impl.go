package dao

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// riskStatsRow is the persistence structure (gorm mapping cg_risk_stats).
type riskStatsRow struct {
	ID          uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	UserID      int64  `gorm:"column:user_id;not null;uniqueIndex:idx_risk_stats"`
	SubProduct  string `gorm:"column:sub_product;size:128;not null;uniqueIndex:idx_risk_stats"`
	RiskType    string `gorm:"column:risk_type;size:64;not null;uniqueIndex:idx_risk_stats"`
	WindowStart int64  `gorm:"column:window_start;not null;uniqueIndex:idx_risk_stats"` // Unix seconds, minute-level buckets
	HitCount    int    `gorm:"column:hit_count;not null;default:0"`
	CreateTime  int64  `gorm:"column:create_time;not null"`
	UpdateTime  int64  `gorm:"column:update_time;not null"`
}

// TableName specifies the table name.
func (riskStatsRow) TableName() string { return "cg_risk_stats" }

// RiskStatsRepositoryImpl is the gorm-based RiskStatsRepository implementation.
type RiskStatsRepositoryImpl struct {
	db *gorm.DB
}

// NewRiskStatsRepositoryImpl creates the implementation.
func NewRiskStatsRepositoryImpl(db *gorm.DB) *RiskStatsRepositoryImpl {
	return &RiskStatsRepositoryImpl{db: db}
}

// Compile-time assertion: the implementation satisfies the domain.RiskStatsRepository interface.
var _ domain.RiskStatsRepository = (*RiskStatsRepositoryImpl)(nil)

// UpsertIncrement increments hit_count +1 for (user, subProduct, riskType, windowStart).
//
// ⚠️ Unique key: UNIQUE(user_id, sub_product, risk_type, window_start).
// It guarantees one row per (user, subProduct, riskType, windowStart) (no duplicate rows);
// but the hit_count + 1 accumulation is not idempotent — the same record consumed again across batches will +1 again.
// In-batch dedup is the consumer's `id > lastInsertID` lower bound (see consumer); the schema only guarantees "no duplicate rows".
//
// ⚠️ Accumulation semantics: ON DUPLICATE KEY UPDATE hit_count = hit_count + 1.
// AssignmentColumns cannot be used directly (it generates hit_count = excluded.hit_count,
// overwriting hit_count to 1 on duplicates instead of accumulating); multiple hits within the window must each +1,
// otherwise the evaluation job's threshold check (>= 5) would never hit.
func (r *RiskStatsRepositoryImpl) UpsertIncrement(ctx context.Context, userID int64, subProduct, riskType string, windowStart int64) error {
	now := time.Now().Unix()
	row := &riskStatsRow{
		UserID:      userID,
		SubProduct:  subProduct,
		RiskType:    riskType,
		WindowStart: windowStart,
		HitCount:    1,
		CreateTime:  now,
		UpdateTime:  now,
	}
	// ON DUPLICATE KEY UPDATE hit_count = hit_count + 1, update_time = now
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "sub_product"}, {Name: "risk_type"}, {Name: "window_start"}},
		DoUpdates: clause.Assignments(map[string]any{
			"hit_count":   gorm.Expr("hit_count + ?", 1),
			"update_time": now,
		}),
	}).Create(row).Error
}

// SumByUserAndSubProduct sums the accumulated hits of (userID, subProduct) within the [windowStart, now] window.
// Used by the evaluation job: single-window threshold decision "whether to offline".
func (r *RiskStatsRepositoryImpl) SumByUserAndSubProduct(ctx context.Context, userID int64, subProduct string, windowStart int64) (int, error) {
	var total int
	err := r.db.WithContext(ctx).Raw(
		"SELECT COALESCE(SUM(hit_count), 0) FROM cg_risk_stats WHERE user_id = ? AND sub_product = ? AND window_start >= ?",
		userID, subProduct, windowStart,
	).Scan(&total).Error
	return total, err
}

// MinWindowStart returns the oldest window start (tests/reconciliation).
func (r *RiskStatsRepositoryImpl) MinWindowStart(ctx context.Context) (int64, error) {
	var min int64
	err := r.db.WithContext(ctx).Raw(
		"SELECT COALESCE(MIN(window_start), 0) FROM cg_risk_stats",
	).Scan(&min).Error
	return min, err
}

// SumAllInWindow aggregates the hits of all (user, subProduct) pairs after windowStart.
// Used by the evaluation job's full scan.
func (r *RiskStatsRepositoryImpl) SumAllInWindow(ctx context.Context, windowStart int64) ([]domain.RiskStatsRow, error) {
	var rows []domain.RiskStatsRow
	err := r.db.WithContext(ctx).Raw(
		"SELECT user_id, sub_product, SUM(hit_count) AS hit_count FROM cg_risk_stats WHERE window_start >= ? GROUP BY user_id, sub_product",
		windowStart,
	).Scan(&rows).Error
	return rows, err
}
