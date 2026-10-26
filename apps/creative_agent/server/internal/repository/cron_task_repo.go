package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrCronTaskNotFound 表示未找到对应 cron 任务记录。
var ErrCronTaskNotFound = errors.New("cron task not found")

// CronTaskRepository 封装 cg_cron_task 表的数据访问。
type CronTaskRepository interface {
	Insert(ctx context.Context, t *model.CGCronTask) error
	GetByCronTaskID(ctx context.Context, cronTaskID int64) (*model.CGCronTask, error)
	// ListRunning 返回所有 Running 状态的 cron 任务（Poller 扫描源）。
	ListRunning(ctx context.Context) ([]*model.CGCronTask, error)
	// AdvanceStepIndex CAS 推进 CurrentStepIndex：仅当当前值等于 expected 时翻到 next。
	AdvanceStepIndex(ctx context.Context, cronTaskID int64, expected, next int32) (bool, error)
	MarkDone(ctx context.Context, cronTaskID int64) error
	MarkFailed(ctx context.Context, cronTaskID int64, reason string) error
}

type cronTaskRepository struct {
	db *gorm.DB
}

// NewCronTaskRepository 基于 gorm.DB 创建 CronTaskRepository。
func NewCronTaskRepository(db *gorm.DB) CronTaskRepository {
	return &cronTaskRepository{db: db}
}

func (r *cronTaskRepository) Insert(ctx context.Context, t *model.CGCronTask) error {
	if t.CreateTime == 0 {
		t.CreateTime = time.Now().UnixMilli()
	}
	t.UpdateTime = time.Now().UnixMilli()
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *cronTaskRepository) GetByCronTaskID(ctx context.Context, cronTaskID int64) (*model.CGCronTask, error) {
	var t model.CGCronTask
	err := r.db.WithContext(ctx).Where("cron_task_id = ?", cronTaskID).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCronTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *cronTaskRepository) ListRunning(ctx context.Context) ([]*model.CGCronTask, error) {
	var tasks []*model.CGCronTask
	err := r.db.WithContext(ctx).Where("status = ?", model.CronTaskStatusRunning).Find(&tasks).Error
	return tasks, err
}

func (r *cronTaskRepository) AdvanceStepIndex(ctx context.Context, cronTaskID int64, expected, next int32) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.CGCronTask{}).
		Where("cron_task_id = ? AND current_step_index = ?", cronTaskID, expected).
		Updates(map[string]any{"current_step_index": next, "update_time": time.Now().UnixMilli()})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *cronTaskRepository) MarkDone(ctx context.Context, cronTaskID int64) error {
	return r.db.WithContext(ctx).Model(&model.CGCronTask{}).
		Where("cron_task_id = ?", cronTaskID).
		Updates(map[string]any{"status": model.CronTaskStatusDone, "update_time": time.Now().UnixMilli()}).Error
}

func (r *cronTaskRepository) MarkFailed(ctx context.Context, cronTaskID int64, reason string) error {
	return r.db.WithContext(ctx).Model(&model.CGCronTask{}).
		Where("cron_task_id = ?", cronTaskID).
		Updates(map[string]any{"status": model.CronTaskStatusFailed, "reason": reason, "update_time": time.Now().UnixMilli()}).Error
}
