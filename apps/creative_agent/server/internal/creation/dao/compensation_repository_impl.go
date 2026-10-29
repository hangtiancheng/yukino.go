package dao

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

	"gorm.io/gorm"
)

// compensationRow is the persistence structure (gorm mapping cg_audit_compensation).
// The domain model Compensation is a plain struct (no gorm tags); gorm tags live only in this private row struct.
type compensationRow struct {
	ID         uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	UserID     int64  `gorm:"column:user_id;not null"`
	ProjectID  string `gorm:"column:project_id;size:64;not null"`
	BizID      string `gorm:"column:biz_id;size:128;not null;index:idx_biz_type"`
	BizType    int    `gorm:"column:biz_type;not null;index:idx_biz_type"`
	Payload    string `gorm:"column:payload;type:text"` // the original request to retry (JSON)
	Status     int    `gorm:"column:status;not null;default:0"`
	RetryCount int    `gorm:"column:retry_count;not null;default:0"`
	CreateTime int64  `gorm:"column:create_time;not null"`
	UpdateTime int64  `gorm:"column:update_time;not null"`
}

// TableName specifies the table name.
func (compensationRow) TableName() string { return "cg_audit_compensation" }

// CompensationRepositoryImpl is the gorm-based CompensationRepository implementation.
type CompensationRepositoryImpl struct {
	db *gorm.DB
}

// NewCompensationRepositoryImpl creates the implementation.
func NewCompensationRepositoryImpl(db *gorm.DB) *CompensationRepositoryImpl {
	return &CompensationRepositoryImpl{db: db}
}

// Compile-time assertion: the implementation satisfies the domain.CompensationRepository interface.
var _ domain.CompensationRepository = (*CompensationRepositoryImpl)(nil)

// toCompensationRow converts the domain entity into the persistence structure.
func toCompensationRow(f *domain.Compensation) *compensationRow {
	return &compensationRow{
		ID:         f.ID,
		UserID:     f.UserID,
		ProjectID:  f.ProjectID,
		BizID:      f.BizID,
		BizType:    f.BizType,
		Payload:    f.Payload,
		Status:     f.Status,
		RetryCount: f.RetryCount,
		CreateTime: f.CreateTime,
		UpdateTime: f.UpdateTime,
	}
}

// toCompensationDomain converts the persistence structure into the domain entity.
func toCompensationDomain(r *compensationRow) *domain.Compensation {
	return &domain.Compensation{
		ID:         r.ID,
		UserID:     r.UserID,
		ProjectID:  r.ProjectID,
		BizID:      r.BizID,
		BizType:    r.BizType,
		Payload:    r.Payload,
		Status:     r.Status,
		RetryCount: r.RetryCount,
		CreateTime: r.CreateTime,
		UpdateTime: r.UpdateTime,
	}
}

// Insert writes one compensation record. create_time/update_time default to now, and the auto-increment ID is backfilled.
func (r *CompensationRepositoryImpl) Insert(ctx context.Context, c *domain.Compensation) (uint64, error) {
	now := time.Now().Unix()
	row := toCompensationRow(c)
	if row.CreateTime == 0 {
		row.CreateTime = now
	}
	row.UpdateTime = now
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return 0, err
	}
	// Backfill the auto-increment ID
	c.ID = row.ID
	return row.ID, nil
}

// GetByBizID queries a compensation record by biz_id + biz_type (idempotent: one failed submission is not compensated twice),
// taking the newest row (id DESC); returns domain.ErrCompensationNotFound when absent.
func (r *CompensationRepositoryImpl) GetByBizID(ctx context.Context, bizID string, bizType int) (*domain.Compensation, error) {
	var row compensationRow
	err := r.db.WithContext(ctx).
		Where("biz_id = ? AND biz_type = ?", bizID, bizType).
		Order("id DESC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrCompensationNotFound
	}
	if err != nil {
		return nil, err
	}
	return toCompensationDomain(&row), nil
}

// GetPending fetches compensation records awaiting retry (status=0), ascending by id, at most limit rows (for the compensation job).
func (r *CompensationRepositoryImpl) GetPending(ctx context.Context, limit int) ([]*domain.Compensation, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []*compensationRow
	err := r.db.WithContext(ctx).
		Where("status = ?", domain.CompensationStatusPending).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	list := make([]*domain.Compensation, 0, len(rows))
	for _, row := range rows {
		list = append(list, toCompensationDomain(row))
	}
	return list, nil
}

// MarkDone marks a compensation record processed (retry succeeded or abandoned beyond the cap).
func (r *CompensationRepositoryImpl) MarkDone(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&compensationRow{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": domain.CompensationStatusDone, "update_time": time.Now().Unix()}).Error
}

// IncrRetryCount increments the retry count (called on retry failure).
func (r *CompensationRepositoryImpl) IncrRetryCount(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&compensationRow{}).
		Where("id = ?", id).
		UpdateColumn("retry_count", gorm.Expr("retry_count + 1")).Error
}
