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

// TaskStepFrame 是 poll_hook 回流到前端的子任务进度帧（SSE task-step / task-done）。
type TaskStepFrame struct {
	Type       string `json:"type"`                 // task-step / task-done
	TaskRunID  int64  `json:"taskRunId,omitempty"`  // 所属 task_run
	TaskStepID int64  `json:"taskStepId,omitempty"` // 步骤 ID
	StepName   string `json:"stepName,omitempty"`   // 步骤名（agent 名 / tool 名）
	Status     int32  `json:"status"`               // cg_task_step.status
	Output     string `json:"output,omitempty"`     // step_output（终态时交付产物）
	Reason     string `json:"reason,omitempty"`     // 失败原因（task_run Failed 时）
	MsgID      string `json:"msgId,omitempty"`      // 关联管家 message_id（前端回放锚点）
}

// Poller 是子任务回流轮询器（对应真实系统 query_executor.go 的 TaskStepPollHook）。
//
// 与真实系统同构：子智能体只写 cg_task_run / cg_task_step 两张表，不主动推送。
// 对话 Worker 通过 poll 轮询这两张表，把增量 step 输出推成 SSE/chunk 回流前端。
// 跨进程完全靠 DB 解耦（05-subagent.md §5.4）。
type Poller struct {
	stepRepo    repository.TaskStepRepository
	taskRunRepo repository.TaskRunRepository
	logger      *zap.Logger

	// lastStepID 记录已推送的最大 task_step_id（游标语义，只推增量）。
	lastStepID atomic.Int64
}

// PollerConfig 持有 Poller 的依赖。
type PollerConfig struct {
	StepRepo    repository.TaskStepRepository
	TaskRunRepo repository.TaskRunRepository
	Logger      *zap.Logger
}

// NewPoller 创建子任务回流轮询器。
func NewPoller(cfg PollerConfig) *Poller {
	return &Poller{
		stepRepo:    cfg.StepRepo,
		taskRunRepo: cfg.TaskRunRepo,
		logger:      cfg.Logger,
	}
}

// PollOnce 轮询一次 cg_task_step：把增量步骤推给 onFrame，检查 task_run 终态。
// 返回是否已到终态（true=task_run Finished/Failed/Waiting，应停止轮询）。
//
// 游标语义：与 ③ 的 attach 同思想——只推 lastStepID 之后的增量，不重复推。
func (p *Poller) PollOnce(ctx context.Context, taskRunID int64, msgID string, onFrame func(TaskStepFrame)) bool {
	// 1. 拉增量步骤
	steps, err := p.stepRepo.GetByTaskRunID(ctx, taskRunID)
	if err != nil {
		p.logger.Error("poll: 拉 task_step 失败",
			zap.Int64("taskRunID", taskRunID), zap.Error(err))
		return false
	}

	last := p.lastStepID.Load()
	for _, s := range steps {
		if s.ID <= last {
			continue // 已推送过
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

	// 2. 查 task_run 终态
	run, err := p.taskRunRepo.GetByTaskRunID(ctx, taskRunID)
	if err != nil {
		return false
	}

	// 终态统一推 task-done。交付产物从「最近一条 Finished 顶层步骤」取——
	// 因为步骤按游标去重，Running→Finished 的同一 step 不会被重推，
	// 必须由 run 终态帧兜底携带 final_product（保证交付不丢）。
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
		// NeedWait=Yes：task_run 保持 Waiting，等产线回调。
		// 此时不推 task-done（终态未到），但中间产物已通过 task-step 回流。
		// 真实系统在此由产线 Callback 把 Waiting→Finished 后 poll 再收敛。
		return true
	default:
		return false // 还在跑，下次再查
	}
}

// newestFinishedStepID 返回 steps 中最近一条 Finished 顶层步骤的 TaskStepID（无则 0）。
func newestFinishedStepID(steps []model.CGTaskStep) int64 {
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].ParentStepID == 0 && steps[i].Status == model.TaskStepStatusFinished {
			return steps[i].TaskStepID
		}
	}
	return 0
}

// PollUntilDone 阻塞轮询直到 task_run 终态（或 ctx 取消）。
// onFrame 每轮收到增量帧。pollInterval 默认 500ms。
func (p *Poller) PollUntilDone(ctx context.Context, taskRunID int64, msgID string, onFrame func(TaskStepFrame), pollInterval time.Duration) {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// 先查一次，避免等待首个 tick
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

// FrameToJSON 将 TaskStepFrame 序列化为 JSON（供 chunkStore.Write / flowcontext.Push）。
func FrameToJSON(f TaskStepFrame) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// FrameToSSEFrame 将 TaskStepFrame 转为通用 SSEFrame（复用 provider.SSEFrame 的流式通道）。
func FrameToSSEFrame(f TaskStepFrame) interface{} {
	return f
}
