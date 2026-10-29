package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrTaskStepNotFound indicates no matching task_step record was found.
var ErrTaskStepNotFound = errors.New("task step not found")

// TaskStepRepository encapsulates data access to the cg_task_step table.
//
// Core semantics:
//   - Insert: creates a row for each sub-agent execution step (before the tool call)
//   - GetByTaskRunID: queries all steps under a task_run (used by the poll backflow)
//   - UpdateByTaskStepID: updates by business key (writes step_output / intermediate_result / status)
type TaskStepRepository interface {
	// Insert inserts one task_step record.
	Insert(ctx context.Context, s *model.CGTaskStep) error

	// GetByTaskStepID queries a record by its snowflake business key.
	GetByTaskStepID(ctx context.Context, taskStepID int64) (*model.CGTaskStep, error)

	// GetByTaskRunID queries all steps under a task_run (including sub-steps; the poll backflow's data source).
	GetByTaskRunID(ctx context.Context, taskRunID int64) ([]model.CGTaskStep, error)

	// UpdateByTaskStepID updates fields by business key.
	UpdateByTaskStepID(ctx context.Context, taskStepID int64, assigns map[string]any) error

	// GetNewestFinishedOutput queries the output of the newest Finished top-level step under a task_run
	// (the final delivered product in the NeedWait branch).
	GetNewestFinishedOutput(ctx context.Context, taskRunID int64) (string, error)
}

type taskStepRepository struct {
	db *gorm.DB
}

// NewTaskStepRepository creates a TaskStepRepository on top of gorm.DB.
func NewTaskStepRepository(db *gorm.DB) TaskStepRepository {
	return &taskStepRepository{db: db}
}

func (r *taskStepRepository) Insert(ctx context.Context, s *model.CGTaskStep) error {
	now := time.Now().UnixMilli()
	if s.CreateTime == 0 {
		s.CreateTime = now
	}
	if s.UpdateTime == 0 {
		s.UpdateTime = now
	}
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *taskStepRepository) GetByTaskStepID(ctx context.Context, taskStepID int64) (*model.CGTaskStep, error) {
	var s model.CGTaskStep
	err := r.db.WithContext(ctx).Where("task_step_id = ?", taskStepID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskStepNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *taskStepRepository) GetByTaskRunID(ctx context.Context, taskRunID int64) ([]model.CGTaskStep, error) {
	var steps []model.CGTaskStep
	err := r.db.WithContext(ctx).
		Where("task_run_id = ?", taskRunID).
		Order("id ASC").
		Find(&steps).Error
	if err != nil {
		return nil, err
	}
	return steps, nil
}

func (r *taskStepRepository) UpdateByTaskStepID(ctx context.Context, taskStepID int64, assigns map[string]any) error {
	assigns["update_time"] = time.Now().UnixMilli()
	return r.db.WithContext(ctx).Model(&model.CGTaskStep{}).
		Where("task_step_id = ?", taskStepID).
		Updates(assigns).Error
}

func (r *taskStepRepository) GetNewestFinishedOutput(ctx context.Context, taskRunID int64) (string, error) {
	var s model.CGTaskStep
	err := r.db.WithContext(ctx).
		Where("task_run_id = ? AND parent_step_id = 0 AND status = ?", taskRunID, model.TaskStepStatusFinished).
		Order("id DESC").
		First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return s.StepOutput, nil
}
