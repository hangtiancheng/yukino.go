package dao

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

	"gorm.io/gorm"
)

// callbackRecordRow 持久化结构（gorm 映射 cg_task_callback_record）。
// 域模型 CallbackRecord 为纯结构体（无 gorm tag），gorm tag 仅存在于本私有行结构。
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

// TableName 指定表名。
func (callbackRecordRow) TableName() string { return "cg_task_callback_record" }

// CallbackRecordRepositoryImpl 基于 gorm 的 CallbackRecordRepository 实现。
type CallbackRecordRepositoryImpl struct {
	db *gorm.DB
}

// NewCallbackRecordRepositoryImpl 创建实现。
func NewCallbackRecordRepositoryImpl(db *gorm.DB) *CallbackRecordRepositoryImpl {
	return &CallbackRecordRepositoryImpl{db: db}
}

// 编译期断言：确保实现满足 domain.CallbackRecordRepository 接口。
var _ domain.CallbackRecordRepository = (*CallbackRecordRepositoryImpl)(nil)

// toCallbackRecordRow 将域实体转为持久化结构。
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

// toCallbackRecordDomain 将持久化结构转为域实体。
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

// Insert 插入回调记录。create_time/update_time 缺省填当前时间，并回填自增 ID。
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
	// 回填自增 ID
	rec.ID = row.ID
	return row.ID, nil
}

// Get 按主键查回调记录，未找到返回 domain.ErrCallbackRecordNotFound。
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

// GetByProjectAndType 按 project + task_type 查回调记录（幂等 / 对账用），
// 取最新一条（id DESC），未找到返回 domain.ErrCallbackRecordNotFound。
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

// Update 按主键更新回调记录的指定字段，恒刷新 update_time。
// ⚠️ 注意：入参 assigns map 会被原地写入 update_time 键，调用方勿复用同一 map 跨多次调用。
func (r *CallbackRecordRepositoryImpl) Update(ctx context.Context, id uint64, assigns map[string]any) error {
	assigns["update_time"] = time.Now().Unix()
	return r.db.WithContext(ctx).Model(&callbackRecordRow{}).
		Where("id = ?", id).Updates(assigns).Error
}

// MarkStatusIf 乐观锁：仅当当前 status 在 allowedFrom 内才置新 status，返回是否更新成功。
//
// 调用方约定：状态迁移必须显式传 allowedFrom（回调重放幂等依赖它）；
// 传空 allowedFrom 时无条件更新（无乐观锁保护），仅限非终态迁移场景使用。
func (r *CallbackRecordRepositoryImpl) MarkStatusIf(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error) {
	query := r.db.WithContext(ctx).Model(&callbackRecordRow{}).
		Where("id = ?", id)
	if len(allowedFrom) > 0 {
		query = query.Where("status IN ?", allowedFrom)
	}
	res := query.Updates(map[string]any{"status": status, "update_time": time.Now().Unix()})
	return res.RowsAffected == 1, res.Error
}
