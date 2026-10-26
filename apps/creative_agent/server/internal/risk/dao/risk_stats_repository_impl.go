package dao

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// riskStatsRow 持久化结构（gorm 映射 cg_risk_stats）。
type riskStatsRow struct {
	ID          uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	UserID      int64  `gorm:"column:user_id;not null;uniqueIndex:idx_risk_stats"`
	SubProduct  string `gorm:"column:sub_product;size:128;not null;uniqueIndex:idx_risk_stats"`
	RiskType    string `gorm:"column:risk_type;size:64;not null;uniqueIndex:idx_risk_stats"`
	WindowStart int64  `gorm:"column:window_start;not null;uniqueIndex:idx_risk_stats"` // Unix 秒，分钟级分桶
	HitCount    int    `gorm:"column:hit_count;not null;default:0"`
	CreateTime  int64  `gorm:"column:create_time;not null"`
	UpdateTime  int64  `gorm:"column:update_time;not null"`
}

// TableName 指定表名。
func (riskStatsRow) TableName() string { return "cg_risk_stats" }

// RiskStatsRepositoryImpl 基于 gorm 的 RiskStatsRepository 实现。
type RiskStatsRepositoryImpl struct {
	db *gorm.DB
}

// NewRiskStatsRepositoryImpl 创建实现。
func NewRiskStatsRepositoryImpl(db *gorm.DB) *RiskStatsRepositoryImpl {
	return &RiskStatsRepositoryImpl{db: db}
}

// 编译期断言：确保实现满足 domain.RiskStatsRepository 接口。
var _ domain.RiskStatsRepository = (*RiskStatsRepositoryImpl)(nil)

// UpsertIncrement 对 (user, subProduct, riskType, windowStart) 累加 hit_count +1。
//
// ⚠️ 唯一键：UNIQUE(user_id, sub_product, risk_type, window_start)。
// 保证同一 (user, subProduct, riskType, windowStart) 只存在一行（不产生重复行）；
// 但 hit_count + 1 的累加语义不幂等——同一条记录跨批次被重复消费时仍会再次 +1。
// 批内去重由消费端 `id > lastInsertID` 下界负责（见 consumer），表结构只兜"不重复行"。
//
// ⚠️ 累加语义：ON DUPLICATE KEY UPDATE hit_count = hit_count + 1。
// 不能直接用 AssignmentColumns（它会生成 hit_count = excluded.hit_count，
// 重复时把 hit_count 覆盖成 1 而非累加），窗口内多次命中必须逐条 +1，
// 否则评估 job 的阈值判断（>= 5）永远不会命中。
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

// SumByUserAndSubProduct 统计 (userID, subProduct) 在 [windowStart, now] 窗口内累计命中次数。
// 评估 job 用：单窗口阈值判断"是否下线"。
func (r *RiskStatsRepositoryImpl) SumByUserAndSubProduct(ctx context.Context, userID int64, subProduct string, windowStart int64) (int, error) {
	var total int
	err := r.db.WithContext(ctx).Raw(
		"SELECT COALESCE(SUM(hit_count), 0) FROM cg_risk_stats WHERE user_id = ? AND sub_product = ? AND window_start >= ?",
		userID, subProduct, windowStart,
	).Scan(&total).Error
	return total, err
}

// MinWindowStart 返回当前最老的窗口起点（测试/对账用）。
func (r *RiskStatsRepositoryImpl) MinWindowStart(ctx context.Context) (int64, error) {
	var min int64
	err := r.db.WithContext(ctx).Raw(
		"SELECT COALESCE(MIN(window_start), 0) FROM cg_risk_stats",
	).Scan(&min).Error
	return min, err
}

// SumAllInWindow 统计 windowStart 之后所有 (user, subProduct) 的聚合命中数。
// 评估 job 全量扫描用。
func (r *RiskStatsRepositoryImpl) SumAllInWindow(ctx context.Context, windowStart int64) ([]domain.RiskStatsRow, error) {
	var rows []domain.RiskStatsRow
	err := r.db.WithContext(ctx).Raw(
		"SELECT user_id, sub_product, SUM(hit_count) AS hit_count FROM cg_risk_stats WHERE window_start >= ? GROUP BY user_id, sub_product",
		windowStart,
	).Scan(&rows).Error
	return rows, err
}
