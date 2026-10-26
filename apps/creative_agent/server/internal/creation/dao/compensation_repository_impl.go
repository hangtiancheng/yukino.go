package dao

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

	"gorm.io/gorm"
)

// compensationRow 持久化结构（gorm 映射 cg_audit_compensation）。
// 域模型 Compensation 为纯结构体（无 gorm tag），gorm tag 仅存在于本私有行结构。
type compensationRow struct {
	ID         uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	UserID     int64  `gorm:"column:user_id;not null"`
	ProjectID  string `gorm:"column:project_id;size:64;not null"`
	BizID      string `gorm:"column:biz_id;size:128;not null;index:idx_biz_type"`
	BizType    int    `gorm:"column:biz_type;not null;index:idx_biz_type"`
	Payload    string `gorm:"column:payload;type:text"` // 待重试的原始请求（JSON）
	Status     int    `gorm:"column:status;not null;default:0"`
	RetryCount int    `gorm:"column:retry_count;not null;default:0"`
	CreateTime int64  `gorm:"column:create_time;not null"`
	UpdateTime int64  `gorm:"column:update_time;not null"`
}

// TableName 指定表名。
func (compensationRow) TableName() string { return "cg_audit_compensation" }

// CompensationRepositoryImpl 基于 gorm 的 CompensationRepository 实现。
type CompensationRepositoryImpl struct {
	db *gorm.DB
}

// NewCompensationRepositoryImpl 创建实现。
func NewCompensationRepositoryImpl(db *gorm.DB) *CompensationRepositoryImpl {
	return &CompensationRepositoryImpl{db: db}
}

// 编译期断言：确保实现满足 domain.CompensationRepository 接口。
var _ domain.CompensationRepository = (*CompensationRepositoryImpl)(nil)

// toCompensationRow 将域实体转为持久化结构。
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

// toCompensationDomain 将持久化结构转为域实体。
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

// Insert 写一条补偿记录。create_time/update_time 缺省填当前时间，并回填自增 ID。
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
	// 回填自增 ID
	c.ID = row.ID
	return row.ID, nil
}

// GetByBizID 按 biz_id + biz_type 查补偿记录（幂等：同一笔提交失败不重复补偿），
// 取最新一条（id DESC），未找到返回 domain.ErrCompensationNotFound。
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

// GetPending 取待重试的补偿记录（status=0），按 id 升序，最多 limit 条（补偿 job 用）。
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

// MarkDone 标记补偿记录为已处理（重试成功或超上限放弃）。
func (r *CompensationRepositoryImpl) MarkDone(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&compensationRow{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": domain.CompensationStatusDone, "update_time": time.Now().Unix()}).Error
}

// IncrRetryCount 重试次数 +1（重试失败时调用）。
func (r *CompensationRepositoryImpl) IncrRetryCount(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&compensationRow{}).
		Where("id = ?", id).
		UpdateColumn("retry_count", gorm.Expr("retry_count + 1")).Error
}
