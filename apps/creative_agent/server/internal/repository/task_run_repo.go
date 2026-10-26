package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrTaskRunNotFound 表示未找到对应 task_run 记录。
var ErrTaskRunNotFound = errors.New("task run not found")

// ErrTaskRunConflict 表示 CAS 抢占冲突（已被其他 executor 抢占）。
var ErrTaskRunConflict = errors.New("task run conflict: already taken by another executor")

// TaskRunRepository 封装 cg_task_run 表的数据访问。
//
// 核心语义：
//   - Insert: 可靠性第一道——Submitter 先建 task_run(Pending) 再投递
//   - UpdateCAS: CAS 抢占（WHERE task_run_id=? AND status=expected），防多实例并发执行同一步骤
//   - UpdateByTaskRunID: 非 CAS 更新（终态、NeedWait 分流等）
type TaskRunRepository interface {
	// Insert 插入一条 task_run 记录。
	Insert(ctx context.Context, r *model.CGTaskRun) error

	// GetByTaskRunID 按雪花业务键查询记录。
	GetByTaskRunID(ctx context.Context, taskRunID int64) (*model.CGTaskRun, error)

	// GetByMsgID 按管家 message_id 查询最新一条 task_run（快路径回流定位用）。
	GetByMsgID(ctx context.Context, msgID string) (*model.CGTaskRun, error)

	// GetWaitingBySession 查询指定 session + user 下最新一条 Waiting 状态的 run（回调推进定位用）。
	GetWaitingBySession(ctx context.Context, sessionID string, userID int64) (*model.CGTaskRun, error)

	// UpdateCAS 仅当 status 等于 expectedStatus 时更新字段（CAS 语义）。
	// RowsAffected==0 表示状态已变化（已被抢占或已终态）。
	UpdateCAS(ctx context.Context, taskRunID int64, expectedStatus int32, assigns map[string]any) (bool, error)

	// UpdateByTaskRunID 无条件按业务键更新字段（不校验当前状态）。
	UpdateByTaskRunID(ctx context.Context, taskRunID int64, assigns map[string]any) error

	// MarkTerminal 将 task_run 标记为终态（Finished 或 Failed），并设置 end_time。
	MarkTerminal(ctx context.Context, taskRunID int64, status int32, reason string) error
}

type taskRunRepository struct {
	db *gorm.DB
}

// NewTaskRunRepository 基于 gorm.DB 创建 TaskRunRepository。
func NewTaskRunRepository(db *gorm.DB) TaskRunRepository {
	return &taskRunRepository{db: db}
}

func (r *taskRunRepository) Insert(ctx context.Context, run *model.CGTaskRun) error {
	now := time.Now().UnixMilli()
	if run.CreateTime == 0 {
		run.CreateTime = now
	}
	if run.UpdateTime == 0 {
		run.UpdateTime = now
	}
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *taskRunRepository) GetByTaskRunID(ctx context.Context, taskRunID int64) (*model.CGTaskRun, error) {
	var run model.CGTaskRun
	err := r.db.WithContext(ctx).Where("task_run_id = ?", taskRunID).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskRunNotFound
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *taskRunRepository) GetByMsgID(ctx context.Context, msgID string) (*model.CGTaskRun, error) {
	var run model.CGTaskRun
	err := r.db.WithContext(ctx).Where("msg_id = ?", msgID).Order("id DESC").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskRunNotFound
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *taskRunRepository) GetWaitingBySession(ctx context.Context, sessionID string, userID int64) (*model.CGTaskRun, error) {
	var run model.CGTaskRun
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND user_id = ? AND status = ?", sessionID, userID, model.TaskRunStatusWaiting).
		Order("id DESC").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskRunNotFound
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *taskRunRepository) UpdateCAS(ctx context.Context, taskRunID int64, expectedStatus int32, assigns map[string]any) (bool, error) {
	assigns["update_time"] = time.Now().UnixMilli()
	res := r.db.WithContext(ctx).
		Model(&model.CGTaskRun{}).
		Where("task_run_id = ? AND status = ?", taskRunID, expectedStatus).
		Updates(assigns)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *taskRunRepository) UpdateByTaskRunID(ctx context.Context, taskRunID int64, assigns map[string]any) error {
	assigns["update_time"] = time.Now().UnixMilli()
	return r.db.WithContext(ctx).Model(&model.CGTaskRun{}).
		Where("task_run_id = ?", taskRunID).
		Updates(assigns).Error
}

func (r *taskRunRepository) MarkTerminal(ctx context.Context, taskRunID int64, status int32, reason string) error {
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).Model(&model.CGTaskRun{}).
		Where("task_run_id = ?", taskRunID).
		Updates(map[string]any{
			"status":      status,
			"reason":      reason,
			"end_time":    now,
			"update_time": now,
		}).Error
}
