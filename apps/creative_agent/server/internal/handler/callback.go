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

// CallbackDeps 持有回调路由的依赖。
type CallbackDeps struct {
	StepRepo    repository.TaskStepRepository
	TaskRunRepo repository.TaskRunRepository
	Logger      *zap.Logger

	// FindWaitingRun 按回调定位 Waiting 任务。非 nil 时优先使用（测试注入 mock）；
	// 为 nil 时用默认实现 findWaitingRunDefault（调 TaskRunRepo.GetWaitingBySession）。
	FindWaitingRun func(ctx context.Context, cb *baseapi.CallbackReq) (*model.CGTaskRun, error)
}

// CallbackTaskDetailHandler 处理 base-server/fakegen 的回调，推进 Waiting→Finished。
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
		// HTTP 立即返回 200（幂等，防重复回调）。
		c.SetStatus(http.StatusOK)
		c.JSON(yukino.H{"errno": 0})
	}
}

// AdvanceTaskOnCallback 按回调 project_id(session_id) 匹配 Waiting 任务，推进为 Finished。
// 返回 error 表示匹配/推进异常；幂等（重复回调无副作用）。
func AdvanceTaskOnCallback(ctx context.Context, deps CallbackDeps, cb *baseapi.CallbackReq) error {
	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	// 1. 定位 Waiting 的 task_run（project_id 即 session_id）。
	//    优先用注入的 FindWaitingRun（测试），否则走默认实现 findWaitingRunDefault（调 GetWaitingBySession）。
	var run *model.CGTaskRun
	var err error
	if deps.FindWaitingRun != nil {
		run, err = deps.FindWaitingRun(ctx, cb)
	} else {
		run, err = findWaitingRunDefault(ctx, deps, cb)
	}
	if err != nil {
		// 未找到 Waiting run（重复回调/任务已推进）：幂等跳过，不作为错误。
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

	// 2. 更新锚定的 agent 顶层 step（NeedWait=Yes 且 Running）为 Finished + step_output。
	//    锚定到匹配 step（而非任意顶层），避免跨会话/重复回调改错 step。
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
		// 无锚定 step（如身份漂移导致匹配到无 NeedWait 的 run）：
		// 不推进 Finished，避免"Finished 但无产物"的僵尸态（step_output 是 poll→SSE 的产物数据源）。
		// run 保持 Waiting，留给 RecoveryScanner 或人工排查。
		logger.Warn("callback: 无锚定 NeedWait step，不推进 run",
			zap.Int64("taskRunID", run.TaskRunID), zap.Int64("taskID", cb.TaskID))
		return nil
	}
	output := formatFinalProduct(cb)
	if err := deps.StepRepo.UpdateByTaskStepID(ctx, agentStepID, map[string]any{
		"status":      model.TaskStepStatusFinished,
		"step_output": output,
		"end_time":    time.Now().UnixMilli(),
	}); err != nil {
		logger.Warn("callback: 更新 agent step 失败", zap.Int64("taskStepID", agentStepID), zap.Error(err))
	}

	// 3. task_run Waiting→Finished。
	return deps.TaskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFinished, "")
}

// findWaitingRunDefault 按回调 project_id(session_id) + user_id 定位 Waiting run。
// project_id 即 session_id（base-server 契约约定 ProjectID == SessionID）。
func findWaitingRunDefault(ctx context.Context, deps CallbackDeps, cb *baseapi.CallbackReq) (*model.CGTaskRun, error) {
	if cb.ProjectID == "" {
		return nil, fmt.Errorf("callback project_id 为空，无法定位任务")
	}
	return deps.TaskRunRepo.GetWaitingBySession(ctx, cb.ProjectID, cb.UserID)
}

// formatFinalProduct 把回调产物格式化为 step_output JSON。
func formatFinalProduct(cb *baseapi.CallbackReq) string {
	out, _ := json.Marshal(map[string]any{
		"final_product": string(cb.SpecialInfo),
		"task_id":       cb.TaskID,
	})
	return string(out)
}
