package dao

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

	"gorm.io/gorm"
)

// callbackRecordRow is the persistence structure (gorm mapping cg_task_callback_record).
// The domain model CallbackRecord is a plain struct (no gorm tags); gorm tags live only in this private row struct.
type callbackRecordRow struct {
	ID               uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	UserID           int64  `gorm:"column:user_id;not null"`
	ProjectID        string `gorm:"column:project_id;size:64;not null;index:idx_project"`
	TaskStepID       int64  `gorm:"column:task_step_id"`
	TaskType         string `gorm:"column:task_type;size:128;not null;index:idx_project"`
	TaskInput        string `gorm:"column:task_input;type:text"`
	TaskOutput       string `gorm:"column:task_output;type:text"`
	Status           string `gorm:"column:status;size:16;not null;default:running"`
	ProductionLineID string `gorm:"column:production_line_id;size:128;default:''"`
	CallbackTime     int64  `gorm:"column:callback_time"`
	CreateTime       int64  `gorm:"column:create_time;not null"`
	UpdateTime       int64  `gorm:"column:update_time;not null;default:0"`
}

// TableName specifies the table name.
func (callbackRecordRow) TableName() string { return "cg_task_callback_record" }

// CallbackRecordRepositoryImpl is the gorm-based CallbackRecordRepository implementation.
type CallbackRecordRepositoryImpl struct {
	db *gorm.DB
}

// NewCallbackRecordRepositoryImpl creates the implementation.
func NewCallbackRecordRepositoryImpl(db *gorm.DB) *CallbackRecordRepositoryImpl {
	return &CallbackRecordRepositoryImpl{db: db}
}

// Compile-time assertion: the implementation satisfies the domain.CallbackRecordRepository interface.
var _ domain.CallbackRecordRepository = (*CallbackRecordRepositoryImpl)(nil)

// toCallbackRecordRow converts the domain entity into the persistence structure.
func toCallbackRecordRow(f *domain.CallbackRecord) *callbackRecordRow {
	return &callbackRecordRow{
		ID:               f.ID,
		UserID:           f.UserID,
		ProjectID:        f.ProjectID,
		TaskStepID:       f.TaskStepID,
		TaskType:         f.TaskType,
		TaskInput:        f.TaskInput,
		TaskOutput:       f.TaskOutput,
		Status:           f.Status,
		ProductionLineID: f.ProductionLineID,
		CallbackTime:     f.CallbackTime,
		CreateTime:       f.CreateTime,
		UpdateTime:       f.UpdateTime,
	}
}

// toCallbackRecordDomain converts the persistence structure into the domain entity.
func toCallbackRecordDomain(r *callbackRecordRow) *domain.CallbackRecord {
	return &domain.CallbackRecord{
		ID:               r.ID,
		UserID:           r.UserID,
		ProjectID:        r.ProjectID,
		TaskStepID:       r.TaskStepID,
		TaskType:         r.TaskType,
		TaskInput:        r.TaskInput,
		TaskOutput:       r.TaskOutput,
		Status:           r.Status,
		ProductionLineID: r.ProductionLineID,
		CallbackTime:     r.CallbackTime,
		CreateTime:       r.CreateTime,
		UpdateTime:       r.UpdateTime,
	}
}

// Insert inserts a callback record. create_time/update_time default to now, and the auto-increment ID is backfilled.
func (r *CallbackRecordRepositoryImpl) Insert(ctx context.Context, rec *domain.CallbackRecord) (uint64, error) {
	now := time.Now().Unix()
	row := toCallbackRecordRow(rec)
	if row.CreateTime == 0 {
		row.CreateTime = now
	}
	if row.UpdateTime == 0 {
		row.UpdateTime = row.CreateTime
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return 0, err
	}
	// Backfill the auto-increment ID
	rec.ID = row.ID
	return row.ID, nil
}

// Get queries a callback record by primary key; returns domain.ErrCallbackRecordNotFound when absent.
func (r *CallbackRecordRepositoryImpl) Get(ctx context.Context, id uint64) (*domain.CallbackRecord, error) {
	var row callbackRecordRow
	err := r.db.WithContext(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrCallbackRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return toCallbackRecordDomain(&row), nil
}

// GetByProjectAndType queries a callback record by project + task_type (for idempotency / reconciliation),
// taking the newest row (id DESC); returns domain.ErrCallbackRecordNotFound when absent.
func (r *CallbackRecordRepositoryImpl) GetByProjectAndType(ctx context.Context, projectID, taskType string) (*domain.CallbackRecord, error) {
	var row callbackRecordRow
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND task_type = ?", projectID, taskType).
		Order("id DESC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrCallbackRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return toCallbackRecordDomain(&row), nil
}

// Update updates the given fields of a callback record by primary key, always refreshing update_time.
// ⚠️ Note: the assigns map argument gets the update_time key written in place; callers must not reuse the same map across calls.
func (r *CallbackRecordRepositoryImpl) Update(ctx context.Context, id uint64, assigns map[string]any) error {
	assigns["update_time"] = time.Now().Unix()
	return r.db.WithContext(ctx).Model(&callbackRecordRow{}).
		Where("id = ?", id).Updates(assigns).Error
}

// MarkStatusIf optimistic lock: sets the new status only when the current status is within allowedFrom; returns whether the update succeeded.
//
// Caller contract: state transitions must pass allowedFrom explicitly (callback-replay idempotency depends on it);
// with an empty allowedFrom it updates unconditionally (no optimistic-lock protection), for non-terminal transitions only.
func (r *CallbackRecordRepositoryImpl) MarkStatusIf(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error) {
	query := r.db.WithContext(ctx).Model(&callbackRecordRow{}).
		Where("id = ?", id)
	if len(allowedFrom) > 0 {
		query = query.Where("status IN ?", allowedFrom)
	}
	res := query.Updates(map[string]any{"status": status, "update_time": time.Now().Unix()})
	return res.RowsAffected == 1, res.Error
}
