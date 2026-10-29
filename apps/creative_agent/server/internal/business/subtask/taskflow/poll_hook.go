package taskflow

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/sse"

	"go.uber.org/zap"
)

// TaskStepFrame is the subtask progress frame flowing back to the frontend via poll_hook (SSE task-step / task-done).
type TaskStepFrame struct {
	Type       string `json:"type"`                 // task-step / task-done
	TaskRunID  int64  `json:"taskRunId,omitempty"`  // owning task_run
	TaskStepID int64  `json:"taskStepId,omitempty"` // step ID
	StepName   string `json:"stepName,omitempty"`   // step name (agent name / tool name)
	Status     int32  `json:"status"`               // cg_task_step.status
	Output     string `json:"output,omitempty"`     // step_output (the delivered product at terminal state)
	Reason     string `json:"reason,omitempty"`     // failure reason (when task_run Failed)
	MsgID      string `json:"msgId,omitempty"`      // associated manager message_id (the frontend replay anchor)
}

// Poller is the subtask backflow poller (corresponding to the reference system's query_executor.go TaskStepPollHook).
//
// Isomorphic to the reference system: sub-agents only write the cg_task_run / cg_task_step tables and never push.
// The conversation Worker polls these two tables, pushing incremental step output as SSE/chunks back to the frontend.
// Cross-process decoupling relies entirely on the DB (05-subagent.md §5.4).
type Poller struct {
	stepRepo    repository.TaskStepRepository
	taskRunRepo repository.TaskRunRepository
	logger      *zap.Logger

	// lastStepID records the largest task_step_id pushed so far (cursor semantics; only increments are pushed).
	lastStepID atomic.Int64
}

// PollerConfig holds the Poller's dependencies.
type PollerConfig struct {
	StepRepo    repository.TaskStepRepository
	TaskRunRepo repository.TaskRunRepository
	Logger      *zap.Logger
}

// NewPoller creates the subtask backflow poller.
func NewPoller(cfg PollerConfig) *Poller {
	return &Poller{
		stepRepo:    cfg.StepRepo,
		taskRunRepo: cfg.TaskRunRepo,
		logger:      cfg.Logger,
	}
}

// PollOnce polls cg_task_step once: pushes incremental steps to onFrame and checks the task_run terminal state.
// Returns whether the terminal state was reached (true = task_run Finished/Failed/Waiting; polling should stop).
//
// Cursor semantics: same idea as ③'s attach — only increments after lastStepID are pushed, never duplicates.
func (p *Poller) PollOnce(ctx context.Context, taskRunID int64, msgID string, onFrame func(TaskStepFrame)) bool {
	// 1. Pull incremental steps
	steps, err := p.stepRepo.GetByTaskRunID(ctx, taskRunID)
	if err != nil {
		p.logger.Error("poll: failed to pull task_step",
			zap.Int64("taskRunID", taskRunID), zap.Error(err))
		return false
	}

	last := p.lastStepID.Load()
	for _, s := range steps {
		if s.ID <= last {
			continue // already pushed
		}
		frame := TaskStepFrame{
			Type:       sse.EventTaskStep,
			TaskRunID:  taskRunID,
			TaskStepID: s.TaskStepID,
			StepName:   s.StepName,
			Status:     s.Status,
			Output:     s.StepOutput,
			MsgID:      msgID,
		}
		onFrame(frame)
		if s.ID > last {
			last = s.ID
		}
	}
	p.lastStepID.Store(last)

	// 2. Check the task_run terminal state
	run, err := p.taskRunRepo.GetByTaskRunID(ctx, taskRunID)
	if err != nil {
		return false
	}

	// Terminal states uniformly push task-done. The delivered product is taken from the "newest Finished top-level step" —
	// because steps are deduped by cursor, the same step transitioning Running→Finished is not re-pushed,
	// so the run terminal frame must carry final_product as the fallback (guaranteeing delivery is never lost).
	switch run.Status {
	case model.TaskRunStatusFinished:
		finalOutput, _ := p.stepRepo.GetNewestFinishedOutput(ctx, taskRunID)
		onFrame(TaskStepFrame{
			Type:       sse.EventTaskDone,
			TaskRunID:  taskRunID,
			Status:     run.Status,
			Output:     finalOutput,
			StepName:   run.AgentID,
			TaskStepID: newestFinishedStepID(steps),
			MsgID:      msgID,
		})
		return true
	case model.TaskRunStatusFailed:
		onFrame(TaskStepFrame{
			Type:       sse.EventTaskDone,
			TaskRunID:  taskRunID,
			Status:     run.Status,
			Reason:     run.Reason,
			StepName:   run.AgentID,
			TaskStepID: newestFinishedStepID(steps),
			MsgID:      msgID,
		})
		return true
	case model.TaskRunStatusWaiting:
		// NeedWait=Yes: task_run stays Waiting for the pipeline callback.
		// No task-done is pushed now (terminal state not reached), but intermediate products already flowed back via task-step.
		// In the reference system the pipeline Callback flips Waiting→Finished and the poll then converges.
		return true
	default:
		return false // still running; check next time
	}
}

// newestFinishedStepID returns the TaskStepID of the newest Finished top-level step in steps (0 when none).
func newestFinishedStepID(steps []model.CGTaskStep) int64 {
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].ParentStepID == 0 && steps[i].Status == model.TaskStepStatusFinished {
			return steps[i].TaskStepID
		}
	}
	return 0
}

// PollUntilDone blocks polling until the task_run terminal state (or ctx cancellation).
// onFrame receives incremental frames each round. pollInterval defaults to 500ms.
func (p *Poller) PollUntilDone(ctx context.Context, taskRunID int64, msgID string, onFrame func(TaskStepFrame), pollInterval time.Duration) {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Poll once first to avoid waiting for the first tick
	if p.PollOnce(ctx, taskRunID, msgID, onFrame) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.PollOnce(ctx, taskRunID, msgID, onFrame) {
				return
			}
		}
	}
}

// FrameToJSON serializes a TaskStepFrame to JSON (for chunkStore.Write / flowcontext.Push).
func FrameToJSON(f TaskStepFrame) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// FrameToSSEFrame converts a TaskStepFrame into a generic SSEFrame (reusing provider.SSEFrame's streaming channel).
func FrameToSSEFrame(f TaskStepFrame) interface{} {
	return f
}
