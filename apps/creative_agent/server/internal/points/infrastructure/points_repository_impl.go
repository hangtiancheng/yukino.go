package infrastructure

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/domain"

	"gorm.io/gorm"
)

// pointsFlowRow 持久化结构（gorm 映射 cg_points_flow）。
type pointsFlowRow struct {
	ID         uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	UserID     int64  `gorm:"column:user_id;not null;index:idx_user"`
	ProjectID  string `gorm:"column:project_id;size:64;not null"`
	OrderID    string `gorm:"column:order_id;size:128;not null;uniqueIndex:idx_order_type"`
	FlowType   int    `gorm:"column:flow_type;not null;uniqueIndex:idx_order_type"`
	Amount     int    `gorm:"column:amount;not null"`
	SceneDesc  string `gorm:"column:scene_desc;size:128;default:''"`
	Status     int    `gorm:"column:status;not null;default:1"`
	CreateTime int64  `gorm:"column:create_time;not null"`
}

// TableName 指定表名。
func (pointsFlowRow) TableName() string { return "cg_points_flow" }

// Migrate 对本 context 的表执行 AutoMigrate（database.Init 启动时调用）。
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&pointsFlowRow{})
}

// PointsRepositoryImpl 基于 gorm 的 PointsRepository 实现。
type PointsRepositoryImpl struct {
	db *gorm.DB
}

// NewPointsRepositoryImpl 创建实现。
func NewPointsRepositoryImpl(db *gorm.DB) *PointsRepositoryImpl {
	return &PointsRepositoryImpl{db: db}
}

// toRow 将域实体转为持久化结构。
func toRow(f *domain.PointsFlow) *pointsFlowRow {
	return &pointsFlowRow{
		ID:         f.ID,
		UserID:     f.UserID,
		ProjectID:  f.ProjectID,
		OrderID:    f.OrderID,
		FlowType:   f.FlowType,
		Amount:     f.Amount,
		SceneDesc:  f.SceneDesc,
		Status:     f.Status,
		CreateTime: f.CreateTime,
	}
}

// toDomain 将持久化结构转为域实体。
func toDomain(r *pointsFlowRow) *domain.PointsFlow {
	return &domain.PointsFlow{
		ID:         r.ID,
		UserID:     r.UserID,
		ProjectID:  r.ProjectID,
		OrderID:    r.OrderID,
		FlowType:   r.FlowType,
		Amount:     r.Amount,
		SceneDesc:  r.SceneDesc,
		Status:     r.Status,
		CreateTime: r.CreateTime,
	}
}

// GetBalance 查询用户当前余额 = 总入账（退/返还） - 总扣分（有效）。
//
// ⚠️ 方向说明：flow_type=1 是"扣积分"（用户支出），flow_type=2 是"退积分"（用户收回）。
// 用户视角的可用余额 = 入账 - 支出 = sum(flow_type=2) - sum(flow_type=1 AND status=1)。
// 已退款的扣分记录被置 status=0，不再计入支出（退款本身已通过 flow_type=2 加回余额）。
func (r *PointsRepositoryImpl) GetBalance(ctx context.Context, userID int64) (int, error) {
	// SELECT COALESCE(SUM(CASE WHEN flow_type=2 THEN amount ELSE 0 END), 0)
	//      - COALESCE(SUM(CASE WHEN flow_type=1 AND status=1 THEN amount ELSE 0 END), 0)
	// FROM cg_points_flow WHERE user_id = ?
	var balance int
	err := r.db.WithContext(ctx).Raw(
		"SELECT COALESCE(SUM(CASE WHEN flow_type = ? THEN amount ELSE 0 END), 0) - "+
			"COALESCE(SUM(CASE WHEN flow_type = ? AND status = ? THEN amount ELSE 0 END), 0) "+
			"FROM cg_points_flow WHERE user_id = ?",
		domain.FlowTypeRefund, domain.FlowTypeConsume, domain.FlowStatusValid, userID).
		Scan(&balance).Error
	return balance, err
}

// InsertConsume 写扣积分流水。order_id UNIQUE 索引保证同一个订单不能重复扣。
func (r *PointsRepositoryImpl) InsertConsume(ctx context.Context, flow *domain.PointsFlow) (uint64, error) {
	row := toRow(flow)
	row.FlowType = domain.FlowTypeConsume
	row.Status = domain.FlowStatusValid
	if row.CreateTime == 0 {
		row.CreateTime = time.Now().Unix()
	}
	err := r.db.WithContext(ctx).Create(row).Error
	// 回填自增 ID
	flow.ID = row.ID
	return flow.ID, err
}

// InsertRefund 写退积分流水，同时将对应扣积分流水的 status 置为 0（标记已回退）。
//
// 两步在同一数据库事务内：先插退积分记录，再标记原扣分记录为已回退；
// 若原扣分记录已回退过（affected rows=0）则回滚并返回 ErrAlreadyRefunded。
func (r *PointsRepositoryImpl) InsertRefund(ctx context.Context, flow *domain.PointsFlow) (uint64, error) {
	row := toRow(flow)
	row.FlowType = domain.FlowTypeRefund
	row.Status = domain.FlowStatusValid
	if row.CreateTime == 0 {
		row.CreateTime = time.Now().Unix()
	}
	var id uint64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 插退积分记录
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		id = row.ID

		// 2. 标记原扣分记录为已回退
		res := tx.Model(&pointsFlowRow{}).
			Where("order_id = ? AND flow_type = ? AND status = ?", row.OrderID, domain.FlowTypeConsume, domain.FlowStatusValid).
			Update("status", domain.FlowStatusRolledBack)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 已退过：不允许同一笔订单重复退积分
			return domain.ErrAlreadyRefunded
		}
		return nil
	})
	// 回填自增 ID
	flow.ID = row.ID
	return id, err
}

// RefundByOrderID 根据订单号退积分（幂等版本，业务代码统一用这个）。
//
// 先查是否已退过，已退直接返回 nil（幂等）；否则走 InsertRefund。
func (r *PointsRepositoryImpl) RefundByOrderID(ctx context.Context, orderID string, userID int64, title string) error {
	var count int64
	err := r.db.WithContext(ctx).Model(&pointsFlowRow{}).
		Where("order_id = ? AND flow_type = ?", orderID, domain.FlowTypeRefund).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return nil // 幂等：已退过
	}
	// 需要退还的金额 = 该订单原扣分金额（拿最新一条有效扣分记录）
	var consume pointsFlowRow
	err = r.db.WithContext(ctx).
		Where("order_id = ? AND flow_type = ?", orderID, domain.FlowTypeConsume).
		Order("id DESC").
		First(&consume).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 没有可退的扣分记录（比如扣分流水本身就失败）—— 视为已退，幂等返回
		return nil
	}
	if err != nil {
		return err
	}
	_, err = r.InsertRefund(ctx, &domain.PointsFlow{
		UserID:    consume.UserID,
		ProjectID: consume.ProjectID,
		OrderID:   orderID,
		Amount:    consume.Amount,
		SceneDesc: title,
	})
	if errors.Is(err, domain.ErrAlreadyRefunded) {
		return nil // 并发下已被其他请求退过，幂等返回
	}
	return err
}

// ListByProject 反查某作品的所有扣退记录（对账能力）。
func (r *PointsRepositoryImpl) ListByProject(ctx context.Context, projectID string) ([]*domain.PointsFlow, error) {
	var rows []*pointsFlowRow
	err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	list := make([]*domain.PointsFlow, 0, len(rows))
	for _, row := range rows {
		list = append(list, toDomain(row))
	}
	return list, nil
}
