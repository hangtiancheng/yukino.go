package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/baseapi"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.uber.org/zap"
)

// CallbackDeps holds the dependencies of the callback route.
type CallbackDeps struct {
	StepRepo    repository.TaskStepRepository
	TaskRunRepo repository.TaskRunRepository
	Logger      *zap.Logger

	// FindWaitingRun locates the Waiting task from the callback. Used first when non-nil (tests inject a mock);
	// when nil, the default implementation findWaitingRunDefault is used (calls TaskRunRepo.GetWaitingBySession).
	FindWaitingRun func(ctx context.Context, cb *baseapi.CallbackReq) (*model.CGTaskRun, error)
}

// CallbackTaskDetailHandler handles base-server/fakegen callbacks, advancing Waiting→Finished.
func CallbackTaskDetailHandler(deps CallbackDeps) yukino.Middleware {
	if deps.Logger == nil {
		deps.Logger = zap.NewNop()
	}
	return func(c *yukino.Context, _ func()) {
		var req baseapi.CallbackReq
		if err := c.BindJSON(&req); err != nil {
			deps.Logger.Warn("callback: bind body failed", zap.Error(err))
			c.SetStatus(http.StatusOK)
			c.JSON(yukino.H{"errno": 1, "errmsg": "invalid body"})
			return
		}
		if err := AdvanceTaskOnCallback(c.Request.Context(), deps, &req); err != nil {
			deps.Logger.Warn("callback: advance failed", zap.Error(err))
		}
		// HTTP returns 200 immediately (idempotent, guards against duplicate callbacks).
		c.SetStatus(http.StatusOK)
		c.JSON(yukino.H{"errno": 0})
	}
}

// AdvanceTaskOnCallback matches the Waiting task by the callback's project_id(session_id) and advances it to Finished.
// A returned error indicates a match/advance failure; the operation is idempotent (duplicate callbacks have no side effects).
func AdvanceTaskOnCallback(ctx context.Context, deps CallbackDeps, cb *baseapi.CallbackReq) error {
	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	// 1. Locate the Waiting task_run (project_id is the session_id).
	//    Prefer the injected FindWaitingRun (tests); otherwise use the default findWaitingRunDefault (calls GetWaitingBySession).
	var run *model.CGTaskRun
	var err error
	if deps.FindWaitingRun != nil {
		run, err = deps.FindWaitingRun(ctx, cb)
	} else {
		run, err = findWaitingRunDefault(ctx, deps, cb)
	}
	if err != nil {
		// No Waiting run found (duplicate callback / task already advanced): idempotent skip, not an error.
		if errors.Is(err, repository.ErrTaskRunNotFound) {
			logger.Info("callback: no waiting run matched, idempotent skip",
				zap.Int64("taskID", cb.TaskID))
			return nil
		}
		return err
	}
	if run == nil {
		logger.Info("callback: no waiting run matched, idempotent skip",
			zap.Int64("taskID", cb.TaskID))
		return nil
	}

	// 2. Update the anchored agent top-level step (NeedWait=Yes and Running) to Finished + step_output.
	//    Anchor to the matching step (not any top-level one) to avoid touching the wrong step across sessions / duplicate callbacks.
	steps, err := deps.StepRepo.GetByTaskRunID(ctx, run.TaskRunID)
	if err != nil {
		return err
	}
	agentStepID := int64(0)
	for _, s := range steps {
		if s.ParentStepID == 0 && s.NeedWait == model.NeedWaitYes && s.Status == model.TaskStepStatusRunning {
			agentStepID = s.TaskStepID
			break
		}
	}
	if agentStepID == 0 {
		// No anchored step (e.g. identity drift matched a run without NeedWait):
		// do not advance to Finished, avoiding the "Finished but no product" zombie state (step_output is the product data source for poll→SSE).
		// The run stays Waiting for the RecoveryScanner or manual investigation.
		logger.Warn("callback: no anchored NeedWait step, run not advanced",
			zap.Int64("taskRunID", run.TaskRunID), zap.Int64("taskID", cb.TaskID))
		return nil
	}
	output := formatFinalProduct(cb)
	if err := deps.StepRepo.UpdateByTaskStepID(ctx, agentStepID, map[string]any{
		"status":      model.TaskStepStatusFinished,
		"step_output": output,
		"end_time":    time.Now().UnixMilli(),
	}); err != nil {
		logger.Warn("callback: failed to update agent step", zap.Int64("taskStepID", agentStepID), zap.Error(err))
	}

	// 3. task_run Waiting→Finished.
	return deps.TaskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFinished, "")
}

// findWaitingRunDefault locates the Waiting run by the callback's project_id(session_id) + user_id.
// project_id is the session_id (the base-server contract stipulates ProjectID == SessionID).
func findWaitingRunDefault(ctx context.Context, deps CallbackDeps, cb *baseapi.CallbackReq) (*model.CGTaskRun, error) {
	if cb.ProjectID == "" {
		return nil, fmt.Errorf("callback project_id is empty, cannot locate the task")
	}
	return deps.TaskRunRepo.GetWaitingBySession(ctx, cb.ProjectID, cb.UserID)
}

// formatFinalProduct formats the callback product into the step_output JSON.
func formatFinalProduct(cb *baseapi.CallbackReq) string {
	out, _ := json.Marshal(map[string]any{
		"final_product": string(cb.SpecialInfo),
		"task_id":       cb.TaskID,
	})
	return string(out)
}
