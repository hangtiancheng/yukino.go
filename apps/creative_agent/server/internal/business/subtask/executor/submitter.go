package subtaskexecutor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	"go.uber.org/zap"
)

// TaskPayload 是子任务 stream 的消息体。
// 只携带定位信息（task_run_id 业务键 + msg_id 去重键），执行上下文从 DB 读。
// 由 subtaskexecutor.Submitter 投递、taskflow.Consumer 消费，故跨包导出。
type TaskPayload struct {
	TaskRunID int64  `json:"task_run_id"`
	MsgID     string `json:"msg_id"`
	UserID    int64  `json:"user_id"`
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	Query     string `json:"query"`
	// TraceParent 是 W3C traceparent，跨进程透传 trace。
	TraceParent string `json:"traceparent,omitempty"`
}

// Submitter 是子任务发射器（对应真实系统的 AsyncSubmitter）。
//
// 两个可靠性要点（见 mini-async-task-impl.md §四-a）：
//  1. 先建 task_run(Pending) 再投递——投递可失败，但"待执行"已持久化
//  2. 投递失败不阻断主流程——标记 Failed 后照常返回 id，管家不因一个子任务投递失败而整体挂掉
type Submitter struct {
	taskRunRepo repository.TaskRunRepository
	streamQ     infra.StreamQueue
	logger      *zap.Logger
}

// SubmitterConfig 持有 Submitter 的依赖。
type SubmitterConfig struct {
	TaskRunRepo repository.TaskRunRepository
	StreamQ     infra.StreamQueue
	Logger      *zap.Logger
}

// NewSubmitter 创建 Submitter。
func NewSubmitter(cfg SubmitterConfig) *Submitter {
	return &Submitter{
		taskRunRepo: cfg.TaskRunRepo,
		streamQ:     cfg.StreamQ,
		logger:      cfg.Logger,
	}
}

// Submit 先建 task_run(Pending) 再投递。
// 返回 taskRunID：只要 task_run 建成即返回 id + nil（投递失败不阻断，标记 Failed）。
// 仅当建 task_run 本身失败才返回 error。
func (s *Submitter) Submit(ctx context.Context, run *model.CGTaskRun) (int64, error) {
	// 1. 先落库（可靠性第一道）
	if run.TaskRunID == 0 {
		run.TaskRunID = model.NextID()
	}
	if err := s.taskRunRepo.Insert(ctx, run); err != nil {
		return 0, fmt.Errorf("submitter: 建 task_run 失败: %w", err)
	}

	// 2. 再投递（真实版带 3 次重试；此处简化为一次，失败即标记 Failed）
	guid := fmt.Sprintf("task-%d", run.TaskRunID)
	body, _ := json.Marshal(TaskPayload{
		TaskRunID:   run.TaskRunID,
		MsgID:       run.MsgID, // 真实 messageID（此前是 "task-{id}" guid）
		UserID:      run.UserID,
		SessionID:   run.SessionID,
		AgentID:     run.AgentID,
		Query:       run.OriginalQuery,
		TraceParent: tracex.InjectCarrier(ctx)["traceparent"],
	})
	if err := s.streamQ.Push(ctx, guid, body); err != nil {
		// 投递失败不阻断主流程，但要把 task_run 标记为 Failed（否则永远卡在 Pending）
		s.logger.Error("submitter: 投递失败，标记 task_run Failed",
			zap.Int64("taskRunID", run.TaskRunID),
			zap.Error(err))
		_ = s.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFailed, "submitter: 投递失败: "+err.Error())
		return run.TaskRunID, nil
	}

	return run.TaskRunID, nil
}
