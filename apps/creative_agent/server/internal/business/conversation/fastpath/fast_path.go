package fastpath

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/executor"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"

	"go.uber.org/zap"
)

// FastTaskRequest is the context needed by CheckFastTask.
// Assembled by the shell (sync DoImpl / async Worker) from the request/flow context.
type FastTaskRequest struct {
	SessionID string
	UserID    int64
	AgentID   string
	Query     string
	// RequestType is the request type: "" (normal) / "retry" (retry the last failure) / "cron" (scheduled task).
	RequestType string
	// Resume marks an HIL breakpoint-resume request (carried after the frontend clicks "Continue").
	Resume bool
	// MsgID is the associated message_id (for cron/task_run association).
	MsgID string
	// OriginMsgID is the previous-step message ID echoed by the frontend after card confirmation.
	// Dispatching the next agent uses it to trace the preceding script (aligned with the reference system's OriginMsgID chain).
	OriginMsgID string
}

// FastPath is the unified fast-path entry (Phase 4 4.1/4.2).
//
// Mirrors the reference system's op.Call chain of "return on hit" short-circuits (03-supervisor.md §3.2):
// HIL resume → Retry → CanFastTask → (no hit runs ReAct).
// The sync DoImpl and async Worker share the same decision logic (twin structure).
type FastPath struct {
	Memory      *memory.IntentMemory
	Submitter   *subtaskexecutor.Submitter // dispatch-class fast paths return handled=false when nil
	TaskRunRepo repository.TaskRunRepository
	StepRepo    repository.TaskStepRepository // used to fetch the preceding script (step_output)
	Logger      *zap.Logger
}

// FastPathConfig holds the FastPath's dependencies.
type FastPathConfig struct {
	Memory      *memory.IntentMemory
	Submitter   *subtaskexecutor.Submitter
	TaskRunRepo repository.TaskRunRepository
	StepRepo    repository.TaskStepRepository
	Logger      *zap.Logger
}

// NewFastPath creates a FastPath.
func NewFastPath(cfg FastPathConfig) *FastPath {
	return &FastPath{
		Memory:      cfg.Memory,
		Submitter:   cfg.Submitter,
		TaskRunRepo: cfg.TaskRunRepo,
		StepRepo:    cfg.StepRepo,
		Logger:      cfg.Logger,
	}
}

// CheckFastTask decides and executes the fast path. handled=true means a hit;
// the caller should return directly (no ReAct).
func (f *FastPath) CheckFastTask(ctx context.Context, req *FastTaskRequest) (bool, error) {
	// Branch 1: HIL resume — restore the ReAct context from the checkpoint (no dispatch).
	// Must come first: interrupts happen at CanFastTask stages like params/plan/script_confirmed;
	// if CanFastTask intercepted the resume request first, it would wrongly dispatch a new agent instead of resuming.
	if req.Resume {
		cp := f.Memory.LoadCheckpoint(req.SessionID)
		if cp != nil {
			f.Logger.Info("fastpath: HIL resume hit",
				zap.String("sessionID", req.SessionID))
			return true, nil
		}
	}

	// Branch 2: Retry — rerun reusing the last failed task_run's plan
	if req.RequestType == "retry" {
		return f.tryRetry(ctx, req)
	}

	// Branch 3: CanFastTask — FlowStage hit (the request after a card confirmation)
	stage := f.Memory.GetStage(req.SessionID)
	if stage.CanFastTask() {
		return f.tryFastTask(ctx, req)
	}

	f.Logger.Debug("fastpath: no fast-path hit, running ReAct",
		zap.String("sessionID", req.SessionID),
		zap.Bool("resume", req.Resume),
		zap.String("requestType", req.RequestType),
		zap.String("stage", string(stage)))
	return false, nil
}

// tryFastTask is the fast task after a card confirmation: GetNextAgent computes the next agent → extract the preceding script → Submit.
//
// Aligned with the reference system's buildExecuteAgent (judge.go:172): after script confirmation, OriginMsgID
// traces the preceding script (gold_scriptwriter's step_output) and splices it into the next agent's input.
// When the script is empty it does not dispatch (judge.go:200-205 skip) — avoiding an empty context for the next agent.
func (f *FastPath) tryFastTask(ctx context.Context, req *FastTaskRequest) (bool, error) {
	if f.Submitter == nil {
		return false, nil // no submitter wired; no hit
	}
	pipeline := f.Memory.GetPipeline(req.SessionID)
	currentAgent := f.Memory.GetAgent(req.SessionID)
	if pipeline == "" || currentAgent == "" {
		return false, nil // missing pipeline/agent memory; run normal ReAct
	}

	// Extract the preceding script: look up the script task_run by OriginMsgID and take its step_output's final_product.
	// The script content lives in cg_task_step.step_output (not chat_text's sceneItems — a go-learn difference).
	scriptContent := ""
	scriptResolved := false
	if req.OriginMsgID != "" && f.StepRepo != nil && f.TaskRunRepo != nil {
		scriptRun, err := f.TaskRunRepo.GetByMsgID(ctx, req.OriginMsgID)
		if err == nil && scriptRun != nil {
			if out, err := f.StepRepo.GetNewestFinishedOutput(ctx, scriptRun.TaskRunID); err == nil && out != "" {
				scriptContent = extractFinalProduct(out)
			}
		}
		// OriginMsgID is non-empty but the script is not found: mark "the script should have been available but was not".
		scriptResolved = scriptContent != ""
	}

	agents, err := subtaskexecutor.BuildExecuteAgent(
		subtask.Pipeline(pipeline), subtask.AgentID(currentAgent),
		req.UserID, req.SessionID, req.AgentID, req.Query, scriptContent)
	if err != nil {
		return false, fmt.Errorf("fastpath: BuildExecuteAgent: %w", err)
	}
	if agents == nil {
		// Chain end: no next agent; do not dispatch; hand back to normal ReAct (reply completes)
		f.Logger.Info("fastpath: agent chain ended",
			zap.String("sessionID", req.SessionID))
		return false, nil
	}
	// OriginMsgID present but the preceding script did not parse (e.g. the script task_run is missing/has no product):
	// do not dispatch an empty context (aligned with the reference system's judge.go:200-205 skip semantics); hand back to ReAct so the manager fills the context.
	// Note: without OriginMsgID (legacy chain / direct tests) the original behavior dispatches as usual and the sub-agent copes.
	if req.OriginMsgID != "" && !scriptResolved {
		f.Logger.Info("fastpath: preceding script not resolved, skipping fast-task dispatch (handing back to ReAct)",
			zap.String("sessionID", req.SessionID),
			zap.String("nextAgent", agents[0].AgentID),
			zap.String("originMsgID", req.OriginMsgID))
		return false, nil
	}

	run := &model.CGTaskRun{
		UserID:        req.UserID,
		AgentID:       req.AgentID,
		SessionID:     req.SessionID,
		OriginalQuery: req.Query,
		MsgID:         req.MsgID,
		Status:        model.TaskRunStatusPending,
	}
	if err := run.SetPlanSteps(&model.PlanSteps{Agents: agents}); err != nil {
		return false, fmt.Errorf("fastpath: set plan steps: %w", err)
	}
	if _, err := f.Submitter.Submit(ctx, run); err != nil {
		return false, fmt.Errorf("fastpath: submit: %w", err)
	}
	f.Logger.Info("fastpath: fast task dispatching the next agent",
		zap.String("pipeline", pipeline),
		zap.String("currentAgent", currentAgent),
		zap.String("sessionID", req.SessionID))
	return true, nil
}

// tryRetry reruns reusing the last failed task_run's plan_steps.
func (f *FastPath) tryRetry(ctx context.Context, req *FastTaskRequest) (bool, error) {
	if f.Submitter == nil {
		return false, nil
	}
	failedID := f.Memory.GetLastFailedTaskRun(req.SessionID)
	if failedID == 0 {
		return false, nil // no failure record; no hit
	}
	failedRun, err := f.TaskRunRepo.GetByTaskRunID(ctx, failedID)
	if err != nil {
		if errors.Is(err, repository.ErrTaskRunNotFound) {
			// The remembered failed id is stale (e.g. cleaned up) → silent no-op
			f.Logger.Debug("fastpath: the failed task_run no longer exists, skipping retry",
				zap.Int64("taskRunID", failedID))
			return false, nil
		}
		return false, fmt.Errorf("fastpath: read failed task_run: %w", err)
	}

	plan, err := failedRun.ParsePlanSteps()
	if err != nil || plan == nil || len(plan.Agents) == 0 {
		return false, nil
	}

	run := &model.CGTaskRun{
		UserID:        req.UserID,
		AgentID:       req.AgentID,
		SessionID:     req.SessionID,
		OriginalQuery: failedRun.OriginalQuery,
		MsgID:         req.MsgID,
		Status:        model.TaskRunStatusPending,
	}
	if err := run.SetPlanSteps(plan); err != nil {
		return false, fmt.Errorf("fastpath: retry set plan: %w", err)
	}
	if _, err := f.Submitter.Submit(ctx, run); err != nil {
		return false, fmt.Errorf("fastpath: retry submit: %w", err)
	}
	f.Logger.Info("fastpath: Retry redispatching the last failed task",
		zap.Int64("failedTaskRunID", failedID),
		zap.String("sessionID", req.SessionID))
	return true, nil
}

// extractFinalProduct extracts the final_product content from the step_output JSON.
//
// The step_output structure is {"final_product":"<delivered product>"} (the persistence format at task_executor.go:139).
// On parse failure or a missing field it returns the input as-is (compatible with legacy formats / plain-text step_output).
func extractFinalProduct(stepOutput string) string {
	var m struct {
		FinalProduct string `json:"final_product"`
	}
	if err := json.Unmarshal([]byte(stepOutput), &m); err == nil && m.FinalProduct != "" {
		return m.FinalProduct
	}
	return stepOutput
}
