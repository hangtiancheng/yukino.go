package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrTaskRunNotFound indicates no matching task_run record was found.
var ErrTaskRunNotFound = errors.New("task run not found")

// ErrTaskRunConflict indicates a CAS claim conflict (already claimed by another executor).
var ErrTaskRunConflict = errors.New("task run conflict: already taken by another executor")

// TaskRunRepository encapsulates data access to the cg_task_run table.
//
// Core semantics:
//   - Insert: the first reliability guard — the Submitter creates task_run(Pending) before dispatching
//   - UpdateCAS: CAS claim (WHERE task_run_id=? AND status=expected), preventing multiple instances from executing the same step concurrently
//   - UpdateByTaskRunID: non-CAS update (terminal states, NeedWait branching, etc.)
type TaskRunRepository interface {
	// Insert inserts one task_run record.
	Insert(ctx context.Context, r *model.CGTaskRun) error

	// GetByTaskRunID queries a record by its snowflake business key.
	GetByTaskRunID(ctx context.Context, taskRunID int64) (*model.CGTaskRun, error)

	// GetByMsgID queries the latest task_run by manager message_id (used by the fast-path backflow for locating).
	GetByMsgID(ctx context.Context, msgID string) (*model.CGTaskRun, error)

	// GetWaitingBySession queries the latest Waiting run for the given session + user (used by callback advancement for locating).
	GetWaitingBySession(ctx context.Context, sessionID string, userID int64) (*model.CGTaskRun, error)

	// UpdateCAS updates fields only when status equals expectedStatus (CAS semantics).
	// RowsAffected==0 means the status has changed (already claimed or terminal).
	UpdateCAS(ctx context.Context, taskRunID int64, expectedStatus int32, assigns map[string]any) (bool, error)

	// UpdateByTaskRunID updates fields by business key unconditionally (no current-status check).
	UpdateByTaskRunID(ctx context.Context, taskRunID int64, assigns map[string]any) error

	// MarkTerminal marks the task_run terminal (Finished or Failed) and sets end_time.
	MarkTerminal(ctx context.Context, taskRunID int64, status int32, reason string) error
}

type taskRunRepository struct {
	db *gorm.DB
}

// NewTaskRunRepository creates a TaskRunRepository on top of gorm.DB.
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
