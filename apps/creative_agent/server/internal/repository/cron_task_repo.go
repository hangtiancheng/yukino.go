package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrCronTaskNotFound indicates no matching cron-task record was found.
var ErrCronTaskNotFound = errors.New("cron task not found")

// CronTaskRepository encapsulates data access to the cg_cron_task table.
type CronTaskRepository interface {
	Insert(ctx context.Context, t *model.CGCronTask) error
	GetByCronTaskID(ctx context.Context, cronTaskID int64) (*model.CGCronTask, error)
	// ListRunning returns all cron tasks in Running status (the Poller's scan source).
	ListRunning(ctx context.Context) ([]*model.CGCronTask, error)
	// AdvanceStepIndex advances CurrentStepIndex with CAS: flips to next only when the current value equals expected.
	AdvanceStepIndex(ctx context.Context, cronTaskID int64, expected, next int32) (bool, error)
	MarkDone(ctx context.Context, cronTaskID int64) error
	MarkFailed(ctx context.Context, cronTaskID int64, reason string) error
}

type cronTaskRepository struct {
	db *gorm.DB
}

// NewCronTaskRepository creates a CronTaskRepository on top of gorm.DB.
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
