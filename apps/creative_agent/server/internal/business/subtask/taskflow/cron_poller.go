package taskflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"

	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

// TaskSubmitter 是 Cron Poller 投递子任务的最小接口（解耦具体 Submitter 便于测试）。
type TaskSubmitter interface {
	Submit(ctx context.Context, run *model.CGTaskRun) (int64, error)
}

// CronTaskPoller 是 Cron 多 agent 自动推进轮询器（05-subagent.md §5.5 路径 B）。
//
// 每 CRON_POLL_INTERVAL 扫 Running 的 cg_cron_task：
//
//	当前 agent 已完成（按 cron 投递的 msg_id 规则查 task_run 状态）→ CurrentStepIndex+1 → 投递下一个 agent
//	全部 agent 跑完 → MarkDone 收口
//
// 只服务定时任务会话，对话路径不触发。
type CronTaskPoller struct {
	cronRepo    repository.CronTaskRepository
	taskRunRepo repository.TaskRunRepository
	submitter   TaskSubmitter
	logger      *zap.Logger
}

// CronTaskPollerConfig 持有 CronTaskPoller 的依赖。
type CronTaskPollerConfig struct {
	CronRepo    repository.CronTaskRepository
	TaskRunRepo repository.TaskRunRepository
	Submitter   TaskSubmitter
	Logger      *zap.Logger
}

// NewCronTaskPoller 创建 CronTaskPoller。
func NewCronTaskPoller(cfg CronTaskPollerConfig) *CronTaskPoller {
	return &CronTaskPoller{
		cronRepo:    cfg.CronRepo,
		taskRunRepo: cfg.TaskRunRepo,
		submitter:   cfg.Submitter,
		logger:      cfg.Logger,
	}
}

// Start 启动轮询循环，每 interval 扫一次。阻塞直到 ctx 取消。
func (p *CronTaskPoller) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	p.logger.Info("cron task poller started")
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("cron task poller stopped")
			return
		case <-ticker.C:
			p.Scan(ctx)
		}
	}
}

// Scan 扫一轮所有 Running 的 cron 任务并尝试推进。
func (p *CronTaskPoller) Scan(ctx context.Context) {
	ctx, span := otel.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/executor").Start(ctx, "cron.poll")
	defer span.End()

	tasks, err := p.cronRepo.ListRunning(ctx)
	if err != nil {
		p.logger.Error("cron poller: list running failed", zap.Error(err))
		return
	}
	for _, t := range tasks {
		if p.AdvanceStep(ctx, t) {
			p.logger.Info("cron poller: advanced cron task",
				zap.Int64("cronTaskID", t.CronTaskID))
		}
	}
}

// currentAgentMsgID 计算当前步骤的 task_run 查询键（msg_id 规则）。
//
// index==0：首个子任务由管家投递（msg_id = 管家 message_id，即 cron.MsgID），
// index>0：由 Cron Poller 投递（msg_id = "cron-{cronTaskID}-{index}"）。
// 两者不对称，Poller 必须分别按规则查找当前 agent 的 task_run。
func currentAgentMsgID(cron *model.CGCronTask, index int) string {
	if index == 0 {
		return cron.MsgID
	}
	return fmt.Sprintf("cron-%d-%d", cron.CronTaskID, index)
}

// AdvanceStep 推进一个 cron 任务到下一个 agent。返回是否发生了推进。
//
// 判定：解析 plan_steps → 取 agents[current_step_index] 作为当前 agent。
// 当前 agent 对应的 task_run（index==0 按管家 msg_id 查，index>0 按 "cron-{id}-{idx}" 查）
// 若存在且已 Finished → 推进 index+1 并投递下一个 agent。
// 若当前 agent 的 task_run 不存在或未 Finished → 等待（不推进，避免重复投递）。
// 若 nextIndex 已越界 → MarkDone 收口。
func (p *CronTaskPoller) AdvanceStep(ctx context.Context, cron *model.CGCronTask) bool {
	plan, err := model.ParsePlanStepsString(cron.PlanSteps)
	if err != nil || plan == nil || len(plan.Agents) == 0 {
		p.logger.Error("cron poller: plan_steps 解析失败",
			zap.Int64("cronTaskID", cron.CronTaskID), zap.Error(err))
		_ = p.cronRepo.MarkFailed(ctx, cron.CronTaskID, "plan_steps 解析失败")
		return false
	}

	index := int(cron.CurrentStepIndex)
	if index >= len(plan.Agents) {
		// 越界 = 全部跑完 → 收口
		_ = p.cronRepo.MarkDone(ctx, cron.CronTaskID)
		p.logger.Info("cron poller: cron task done",
			zap.Int64("cronTaskID", cron.CronTaskID))
		return false
	}

	// 当前 agent 是否已完成：按当前 index 的 msg_id 规则查关联 task_run
	curRun, err := p.taskRunRepo.GetByMsgID(ctx, currentAgentMsgID(cron, index))
	if err != nil {
		if errors.Is(err, repository.ErrTaskRunNotFound) {
			// 当前 agent 的 task_run 还没落库/还没被投递 → 等待
			p.logger.Debug("cron poller: 当前步骤 task_run 未就绪，等待",
				zap.Int64("cronTaskID", cron.CronTaskID),
				zap.Int("index", index))
			return false
		}
		// 真实 DB 错误 → 记录，本轮跳过（下次再试）
		p.logger.Error("cron poller: 查询当前步骤 task_run 失败",
			zap.Int64("cronTaskID", cron.CronTaskID),
			zap.Int("index", index),
			zap.Error(err))
		return false
	}
	if curRun.Status == model.TaskRunStatusFailed {
		// 当前 agent 的 task_run 已失败 → cron 整体失败（不再等待）
		p.logger.Warn("cron poller: 当前步骤失败，cron 收口 Failed",
			zap.Int64("cronTaskID", cron.CronTaskID),
			zap.Int("index", index),
			zap.String("reason", curRun.Reason))
		_ = p.cronRepo.MarkFailed(ctx, cron.CronTaskID, "当前 agent task_run 失败: "+curRun.Reason)
		return false
	}

	if curRun.Status != model.TaskRunStatusFinished {
		// 还没执行完（Pending/Running/Waiting）→ 等待
		p.logger.Debug("cron poller: 当前步骤未完成，等待",
			zap.Int64("cronTaskID", cron.CronTaskID),
			zap.Int32("status", curRun.Status))
		return false
	}

	nextIndex := index + 1
	if nextIndex >= len(plan.Agents) {
		// 最后一个 agent 已完成 → 收口
		ok, _ := p.cronRepo.AdvanceStepIndex(ctx, cron.CronTaskID, cron.CurrentStepIndex, int32(nextIndex))
		if ok {
			_ = p.cronRepo.MarkDone(ctx, cron.CronTaskID)
		}
		return ok
	}

	// 投递下一个 agent（agents[nextIndex]）
	nextAgent := plan.Agents[nextIndex]
	run := &model.CGTaskRun{
		UserID:        1, // 简化：cron 会话默认 user=1
		AgentID:       nextAgent.AgentID,
		SessionID:     cron.SessionID,
		OriginalQuery: nextAgent.Input,
		MsgID:         fmt.Sprintf("cron-%d-%d", cron.CronTaskID, nextIndex),
		Status:        model.TaskRunStatusPending,
	}
	if err := run.SetPlanSteps(&model.PlanSteps{Agents: []model.PlanAgent{nextAgent}}); err != nil {
		p.logger.Error("cron poller: set plan steps failed",
			zap.Int64("cronTaskID", cron.CronTaskID), zap.Error(err))
		return false
	}
	if _, err := p.submitter.Submit(ctx, run); err != nil {
		p.logger.Error("cron poller: submit next agent failed",
			zap.Int64("cronTaskID", cron.CronTaskID), zap.Error(err))
		return false
	}

	// CAS 推进索引
	ok, err := p.cronRepo.AdvanceStepIndex(ctx, cron.CronTaskID, cron.CurrentStepIndex, int32(nextIndex))
	if err != nil {
		p.logger.Error("cron poller: advance index failed",
			zap.Int64("cronTaskID", cron.CronTaskID), zap.Error(err))
		return false
	}
	return ok
}
