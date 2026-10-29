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

// TaskSubmitter is the minimal interface the Cron Poller uses to dispatch subtasks (decoupled from the concrete Submitter for testability).
type TaskSubmitter interface {
	Submit(ctx context.Context, run *model.CGTaskRun) (int64, error)
}

// CronTaskPoller is the cron multi-agent auto-advance poller (05-subagent.md §5.5 path B).
//
// Every CRON_POLL_INTERVAL it scans Running cg_cron_task rows:
//
//	the current agent finished (task_run status queried by the cron dispatch msg_id rules) → CurrentStepIndex+1 → dispatch the next agent
//	all agents done → MarkDone to close out
//
// It serves cron-task sessions only; the conversation path never triggers it.
type CronTaskPoller struct {
	cronRepo    repository.CronTaskRepository
	taskRunRepo repository.TaskRunRepository
	submitter   TaskSubmitter
	logger      *zap.Logger
}

// CronTaskPollerConfig holds the CronTaskPoller's dependencies.
type CronTaskPollerConfig struct {
	CronRepo    repository.CronTaskRepository
	TaskRunRepo repository.TaskRunRepository
	Submitter   TaskSubmitter
	Logger      *zap.Logger
}

// NewCronTaskPoller creates a CronTaskPoller.
func NewCronTaskPoller(cfg CronTaskPollerConfig) *CronTaskPoller {
	return &CronTaskPoller{
		cronRepo:    cfg.CronRepo,
		taskRunRepo: cfg.TaskRunRepo,
		submitter:   cfg.Submitter,
		logger:      cfg.Logger,
	}
}

// Start runs the polling loop, scanning every interval. It blocks until ctx is canceled.
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

// Scan sweeps all Running cron tasks once and tries to advance them.
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

// currentAgentMsgID computes the task_run lookup key (msg_id rule) of the current step.
//
// index==0: the first subtask is dispatched by the manager (msg_id = the manager message_id, i.e. cron.MsgID);
// index>0: dispatched by the Cron Poller (msg_id = "cron-{cronTaskID}-{index}").
// The two are asymmetric, so the Poller must look up the current agent's task_run by the respective rule.
func currentAgentMsgID(cron *model.CGCronTask, index int) string {
	if index == 0 {
		return cron.MsgID
	}
	return fmt.Sprintf("cron-%d-%d", cron.CronTaskID, index)
}

// AdvanceStep advances one cron task to the next agent. Returns whether advancement happened.
//
// Decision: parse plan_steps → take agents[current_step_index] as the current agent.
// The task_run of the current agent (index==0 looked up by the manager msg_id, index>0 by "cron-{id}-{idx}")
// exists and is Finished → advance index+1 and dispatch the next agent.
// The current agent's task_run missing or not Finished → wait (no advancement, avoiding duplicate dispatch).
// nextIndex out of bounds → MarkDone to close out.
func (p *CronTaskPoller) AdvanceStep(ctx context.Context, cron *model.CGCronTask) bool {
	plan, err := model.ParsePlanStepsString(cron.PlanSteps)
	if err != nil || plan == nil || len(plan.Agents) == 0 {
		p.logger.Error("cron poller: failed to parse plan_steps",
			zap.Int64("cronTaskID", cron.CronTaskID), zap.Error(err))
		_ = p.cronRepo.MarkFailed(ctx, cron.CronTaskID, "failed to parse plan_steps")
		return false
	}

	index := int(cron.CurrentStepIndex)
	if index >= len(plan.Agents) {
		// Out of bounds = everything done → close out
		_ = p.cronRepo.MarkDone(ctx, cron.CronTaskID)
		p.logger.Info("cron poller: cron task done",
			zap.Int64("cronTaskID", cron.CronTaskID))
		return false
	}

	// Whether the current agent finished: look up the associated task_run by the current index's msg_id rule
	curRun, err := p.taskRunRepo.GetByMsgID(ctx, currentAgentMsgID(cron, index))
	if err != nil {
		if errors.Is(err, repository.ErrTaskRunNotFound) {
			// The current agent's task_run is not persisted/dispatched yet → wait
			p.logger.Debug("cron poller: current step task_run not ready, waiting",
				zap.Int64("cronTaskID", cron.CronTaskID),
				zap.Int("index", index))
			return false
		}
		// A real DB error → log it and skip this round (retry next time)
		p.logger.Error("cron poller: failed to query the current step task_run",
			zap.Int64("cronTaskID", cron.CronTaskID),
			zap.Int("index", index),
			zap.Error(err))
		return false
	}
	if curRun.Status == model.TaskRunStatusFailed {
		// The current agent's task_run failed → the whole cron fails (stop waiting)
		p.logger.Warn("cron poller: current step failed, closing cron as Failed",
			zap.Int64("cronTaskID", cron.CronTaskID),
			zap.Int("index", index),
			zap.String("reason", curRun.Reason))
		_ = p.cronRepo.MarkFailed(ctx, cron.CronTaskID, "current agent task_run failed: "+curRun.Reason)
		return false
	}

	if curRun.Status != model.TaskRunStatusFinished {
		// Not finished yet (Pending/Running/Waiting) → wait
		p.logger.Debug("cron poller: current step not finished, waiting",
			zap.Int64("cronTaskID", cron.CronTaskID),
			zap.Int32("status", curRun.Status))
		return false
	}

	nextIndex := index + 1
	if nextIndex >= len(plan.Agents) {
		// The last agent finished → close out
		ok, _ := p.cronRepo.AdvanceStepIndex(ctx, cron.CronTaskID, cron.CurrentStepIndex, int32(nextIndex))
		if ok {
			_ = p.cronRepo.MarkDone(ctx, cron.CronTaskID)
		}
		return ok
	}

	// Dispatch the next agent (agents[nextIndex])
	nextAgent := plan.Agents[nextIndex]
	run := &model.CGTaskRun{
		UserID:        1, // simplification: cron sessions default to user=1
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

	// CAS-advance the index
	ok, err := p.cronRepo.AdvanceStepIndex(ctx, cron.CronTaskID, cron.CurrentStepIndex, int32(nextIndex))
	if err != nil {
		p.logger.Error("cron poller: advance index failed",
			zap.Int64("cronTaskID", cron.CronTaskID), zap.Error(err))
		return false
	}
	return ok
}
