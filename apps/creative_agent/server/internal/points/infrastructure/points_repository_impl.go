package infrastructure

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/domain"

	"gorm.io/gorm"
)

// pointsFlowRow is the persistence structure (gorm mapping cg_points_flow).
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

// TableName specifies the table name.
func (pointsFlowRow) TableName() string { return "cg_points_flow" }

// Migrate runs AutoMigrate for this context's tables (called at database.Init startup).
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&pointsFlowRow{})
}

// PointsRepositoryImpl is the gorm-based PointsRepository implementation.
type PointsRepositoryImpl struct {
	db *gorm.DB
}

// NewPointsRepositoryImpl creates the implementation.
func NewPointsRepositoryImpl(db *gorm.DB) *PointsRepositoryImpl {
	return &PointsRepositoryImpl{db: db}
}

// toRow converts the domain entity into the persistence structure.
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

// toDomain converts the persistence structure into the domain entity.
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

// GetBalance queries the user's current balance = total credits (refunds/returns) - total deductions (valid).
//
// ⚠️ Direction note: flow_type=1 is "deduct points" (user spending); flow_type=2 is "refund points" (user receiving back).
// The user-visible available balance = credits - spending = sum(flow_type=2) - sum(flow_type=1 AND status=1).
// Refunded deduction records are set status=0 and no longer count as spending (the refund itself already added back via flow_type=2).
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

// InsertConsume writes a deduction entry. The order_id UNIQUE index guarantees one order is never deducted twice.
func (r *PointsRepositoryImpl) InsertConsume(ctx context.Context, flow *domain.PointsFlow) (uint64, error) {
	row := toRow(flow)
	row.FlowType = domain.FlowTypeConsume
	row.Status = domain.FlowStatusValid
	if row.CreateTime == 0 {
		row.CreateTime = time.Now().Unix()
	}
	err := r.db.WithContext(ctx).Create(row).Error
	// Backfill the auto-increment ID
	flow.ID = row.ID
	return flow.ID, err
}

// InsertRefund writes a refund entry and sets the matching deduction entry's status to 0 (marked rolled back).
//
// Both steps run in one database transaction: insert the refund record first, then mark the original deduction rolled back;
// if the original deduction was already rolled back (affected rows=0), roll back and return ErrAlreadyRefunded.
func (r *PointsRepositoryImpl) InsertRefund(ctx context.Context, flow *domain.PointsFlow) (uint64, error) {
	row := toRow(flow)
	row.FlowType = domain.FlowTypeRefund
	row.Status = domain.FlowStatusValid
	if row.CreateTime == 0 {
		row.CreateTime = time.Now().Unix()
	}
	var id uint64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Insert the refund record
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		id = row.ID

		// 2. Mark the original deduction record rolled back
		res := tx.Model(&pointsFlowRow{}).
			Where("order_id = ? AND flow_type = ? AND status = ?", row.OrderID, domain.FlowTypeConsume, domain.FlowStatusValid).
			Update("status", domain.FlowStatusRolledBack)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// Already refunded: one order is never refunded twice
			return domain.ErrAlreadyRefunded
		}
		return nil
	})
	// Backfill the auto-increment ID
	flow.ID = row.ID
	return id, err
}

// RefundByOrderID refunds points by order ID (the idempotent version; business code uses this uniformly).
//
// It first checks whether it was already refunded; already-refunded returns nil directly (idempotent); otherwise it goes through InsertRefund.
func (r *PointsRepositoryImpl) RefundByOrderID(ctx context.Context, orderID string, userID int64, title string) error {
	var count int64
	err := r.db.WithContext(ctx).Model(&pointsFlowRow{}).
		Where("order_id = ? AND flow_type = ?", orderID, domain.FlowTypeRefund).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return nil // idempotent: already refunded
	}
	// The amount to refund = the order's original deduction amount (take the latest valid deduction record)
	var consume pointsFlowRow
	err = r.db.WithContext(ctx).
		Where("order_id = ? AND flow_type = ?", orderID, domain.FlowTypeConsume).
		Order("id DESC").
		First(&consume).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// No refundable deduction record (e.g. the deduction entry itself failed) — treat as refunded, return idempotently
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
		return nil // already refunded by another request under concurrency; return idempotently
	}
	return err
}

// ListByProject looks up all deduction/refund records of a work (reconciliation capability).
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
