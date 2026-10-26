package dao

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"gorm.io/gorm"
)

// auditRecordRow 持久化结构（gorm 映射 cg_risk_audit_record）。
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

// TableName 指定表名。
func (auditRecordRow) TableName() string { return "cg_risk_audit_record" }

// Migrate 对本 context 的表执行 AutoMigrate（database.Init 启动时调用）。
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&auditRecordRow{}, &riskStatsRow{})
}

// AuditRecordRepositoryImpl 基于 gorm 的 AuditRecordRepository 实现。
type AuditRecordRepositoryImpl struct {
	db *gorm.DB
}

// NewAuditRecordRepositoryImpl 创建实现。
func NewAuditRecordRepositoryImpl(db *gorm.DB) *AuditRecordRepositoryImpl {
	return &AuditRecordRepositoryImpl{db: db}
}

// 编译期断言：确保实现满足 domain.AuditRecordRepository 接口。
var _ domain.AuditRecordRepository = (*AuditRecordRepositoryImpl)(nil)

// Insert 插入审计记录。create_time/update_time 缺省填当前时间，并回填自增 ID。
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
	// 回填自增 ID
	rec.ID = row.ID
	return row.ID, nil
}

// Get 按主键查审计记录，未找到返回 domain.ErrRecordNotFound。
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

// Update 按主键更新指定字段，恒刷新 update_time。
// 注意：会就地写入调用方传入的 assigns map（补写 update_time 键）。
func (r *AuditRecordRepositoryImpl) Update(ctx context.Context, id uint64, assigns map[string]any) error {
	assigns["update_time"] = time.Now().Unix()
	return r.db.WithContext(ctx).Model(&auditRecordRow{}).
		Where("id = ?", id).Updates(assigns).Error
}

// MarkTerminal 乐观锁置终态：仅当 status 在 allowedFrom 内才更新，返回是否更新成功。
//
// 调用方约定：终态迁移必须显式传 allowedFrom（回调重放幂等依赖它）；
// 传空 allowedFrom 时无条件更新（无乐观锁保护），仅限非终态迁移场景使用。
func (r *AuditRecordRepositoryImpl) MarkTerminal(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error) {
	query := r.db.WithContext(ctx).Model(&auditRecordRow{}).
		Where("id = ?", id)
	if len(allowedFrom) > 0 {
		query = query.Where("status IN ?", allowedFrom)
	}
	res := query.Updates(map[string]any{"status": status, "update_time": time.Now().Unix()})
	return res.RowsAffected == 1, res.Error
}

// rowToAuditRecord 将持久化结构转为域实体。
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
