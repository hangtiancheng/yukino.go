package dao

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"gorm.io/gorm"
)

// auditRecordRow is the persistence structure (gorm mapping cg_risk_audit_record).
type auditRecordRow struct {
	ID          uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	UserID      int64  `gorm:"column:user_id;not null;index:idx_risk_audit"`
	ProjectID   string `gorm:"column:project_id;size:64;not null;index:idx_risk_audit"`
	TaskStepID  int64  `gorm:"column:task_step_id"`
	TaskType    string `gorm:"column:task_type;size:128;not null;index:idx_risk_audit"`
	TaskInput   string `gorm:"column:task_input;type:text"`
	Status      string `gorm:"column:status;size:16;not null;default:auditing"`
	CallbackURL string `gorm:"column:callback_url;size:256;default:''"`
	CreateTime  int64  `gorm:"column:create_time;not null"`
	UpdateTime  int64  `gorm:"column:update_time;not null;default:0"`
}

// TableName specifies the table name.
func (auditRecordRow) TableName() string { return "cg_risk_audit_record" }

// Migrate runs AutoMigrate for this context's tables (called at database.Init startup).
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&auditRecordRow{}, &riskStatsRow{})
}

// AuditRecordRepositoryImpl is the gorm-based AuditRecordRepository implementation.
type AuditRecordRepositoryImpl struct {
	db *gorm.DB
}

// NewAuditRecordRepositoryImpl creates the implementation.
func NewAuditRecordRepositoryImpl(db *gorm.DB) *AuditRecordRepositoryImpl {
	return &AuditRecordRepositoryImpl{db: db}
}

// Compile-time assertion: the implementation satisfies the domain.AuditRecordRepository interface.
var _ domain.AuditRecordRepository = (*AuditRecordRepositoryImpl)(nil)

// Insert inserts an audit record. create_time/update_time default to now, and the auto-increment ID is backfilled.
func (r *AuditRecordRepositoryImpl) Insert(ctx context.Context, rec *domain.AuditRecord) (uint64, error) {
	now := time.Now().Unix()
	row := &auditRecordRow{
		UserID:      rec.UserID,
		ProjectID:   rec.ProjectID,
		TaskStepID:  rec.TaskStepID,
		TaskType:    rec.TaskType,
		TaskInput:   rec.TaskInput,
		Status:      rec.Status,
		CallbackURL: rec.CallbackURL,
	}
	if rec.CreateTime == 0 {
		row.CreateTime = now
	} else {
		row.CreateTime = rec.CreateTime
	}
	if rec.UpdateTime == 0 {
		row.UpdateTime = row.CreateTime
	} else {
		row.UpdateTime = rec.UpdateTime
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return 0, err
	}
	// Backfill the auto-increment ID
	rec.ID = row.ID
	return row.ID, nil
}

// Get queries an audit record by primary key; returns domain.ErrRecordNotFound when absent.
func (r *AuditRecordRepositoryImpl) Get(ctx context.Context, id uint64) (*domain.AuditRecord, error) {
	var row auditRecordRow
	if err := r.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRecordNotFound
		}
		return nil, err
	}
	return rowToAuditRecord(&row), nil
}

// Update updates the given fields by primary key, always refreshing update_time.
// Note: it writes into the caller's assigns map in place (adding the update_time key).
func (r *AuditRecordRepositoryImpl) Update(ctx context.Context, id uint64, assigns map[string]any) error {
	assigns["update_time"] = time.Now().Unix()
	return r.db.WithContext(ctx).Model(&auditRecordRow{}).
		Where("id = ?", id).Updates(assigns).Error
}

// MarkTerminal sets terminal with an optimistic lock: it updates only when status is within allowedFrom; returns whether the update succeeded.
//
// Caller contract: terminal transitions must pass allowedFrom explicitly (callback-replay idempotency depends on it);
// with an empty allowedFrom it updates unconditionally (no optimistic-lock protection), for non-terminal transitions only.
func (r *AuditRecordRepositoryImpl) MarkTerminal(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error) {
	query := r.db.WithContext(ctx).Model(&auditRecordRow{}).
		Where("id = ?", id)
	if len(allowedFrom) > 0 {
		query = query.Where("status IN ?", allowedFrom)
	}
	res := query.Updates(map[string]any{"status": status, "update_time": time.Now().Unix()})
	return res.RowsAffected == 1, res.Error
}

// rowToAuditRecord converts the persistence structure into the domain entity.
func rowToAuditRecord(row *auditRecordRow) *domain.AuditRecord {
	return &domain.AuditRecord{
		ID:          row.ID,
		UserID:      row.UserID,
		ProjectID:   row.ProjectID,
		TaskStepID:  row.TaskStepID,
		TaskType:    row.TaskType,
		TaskInput:   row.TaskInput,
		Status:      row.Status,
		CallbackURL: row.CallbackURL,
		CreateTime:  row.CreateTime,
		UpdateTime:  row.UpdateTime,
	}
}
