package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrTaskStepNotFound 表示未找到对应 task_step 记录。
var ErrTaskStepNotFound = errors.New("task step not found")

// TaskStepRepository 封装 cg_task_step 表的数据访问。
//
// 核心语义：
//   - Insert: 子智能体执行每步时建行（工具调用前）
//   - GetByTaskRunID: 查指定 task_run 下所有步骤（poll 回流用）
//   - UpdateByTaskStepID: 按业务键更新（写 step_output / intermediate_result / 状态）
type TaskStepRepository interface {
	// Insert 插入一条 task_step 记录。
	Insert(ctx context.Context, s *model.CGTaskStep) error

	// GetByTaskStepID 按雪花业务键查询记录。
	GetByTaskStepID(ctx context.Context, taskStepID int64) (*model.CGTaskStep, error)

	// GetByTaskRunID 查询指定 task_run 下所有步骤（含子步骤，poll 回流的数据源）。
	GetByTaskRunID(ctx context.Context, taskRunID int64) ([]model.CGTaskStep, error)

	// UpdateByTaskStepID 按业务键更新字段。
	UpdateByTaskStepID(ctx context.Context, taskStepID int64, assigns map[string]any) error

	// GetNewestFinishedOutput 查询指定 task_run 下最近一条 Finished 顶层步骤的输出
	// （NeedWait 分支下最终交付产物）。
	GetNewestFinishedOutput(ctx context.Context, taskRunID int64) (string, error)
}

type taskStepRepository struct {
	db *gorm.DB
}

// NewTaskStepRepository 基于 gorm.DB 创建 TaskStepRepository。
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
