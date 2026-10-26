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

// FastTaskRequest 是 CheckFastTask 判断所需上下文。
// 由外壳（同步 DoImpl / 异步 Worker）从请求/flowcontext 组装。
type FastTaskRequest struct {
	SessionID string
	UserID    int64
	AgentID   string
	Query     string
	// RequestType 请求类型：""（普通）/ "retry"（重试上次失败）/ "cron"（定时任务）。
	RequestType string
	// Resume 是否为 HIL 断点恢复请求（前端点"继续"后携带）。
	Resume bool
	// MsgID 关联的 message_id（cron/task_run 关联用）。
	MsgID string
	// OriginMsgID 点卡确认后前端回传的上一步消息 ID。
	// 投递下一个 agent 时用它追溯前序脚本（对齐真实系统 OriginMsgID 链路）。
	OriginMsgID string
}

// FastPath 是快捷路径统一入口（Phase 4 4.1/4.2）。
//
// 对应真实系统 op.Call 的一串"命中就 return"短路（03-supervisor.md §3.2）：
// HIL resume → Retry → CanFastTask → （未命中走 ReAct）。
// 同步 DoImpl 与异步 Worker 共用同一份判断逻辑（双胞胎结构）。
type FastPath struct {
	Memory      *memory.IntentMemory
	Submitter   *subtaskexecutor.Submitter // nil 时投递类快路径返回 handled=false
	TaskRunRepo repository.TaskRunRepository
	StepRepo    repository.TaskStepRepository // 取前序脚本（step_output）用
	Logger      *zap.Logger
}

// FastPathConfig 持有 FastPath 的依赖。
type FastPathConfig struct {
	Memory      *memory.IntentMemory
	Submitter   *subtaskexecutor.Submitter
	TaskRunRepo repository.TaskRunRepository
	StepRepo    repository.TaskStepRepository
	Logger      *zap.Logger
}

// NewFastPath 创建 FastPath。
func NewFastPath(cfg FastPathConfig) *FastPath {
	return &FastPath{
		Memory:      cfg.Memory,
		Submitter:   cfg.Submitter,
		TaskRunRepo: cfg.TaskRunRepo,
		StepRepo:    cfg.StepRepo,
		Logger:      cfg.Logger,
	}
}

// CheckFastTask 判断并执行快捷路径。返回 handled=true 表示命中，
// 调用方应直接返回（不跑 ReAct）。
func (f *FastPath) CheckFastTask(ctx context.Context, req *FastTaskRequest) (bool, error) {
	// 分支 1: HIL resume —— 恢复 checkpoint 里的 ReAct 上下文（不投递）。
	// 必须最优先：中断发生在 params/plan/script_confirmed 等 CanFastTask stage，
	// 若 resume 请求先被 CanFastTask 拦截会错误投递新 agent 而非恢复。
	if req.Resume {
		cp := f.Memory.LoadCheckpoint(req.SessionID)
		if cp != nil {
			f.Logger.Info("fastpath: HIL resume 命中",
				zap.String("sessionID", req.SessionID))
			return true, nil
		}
	}

	// 分支 2: Retry —— 复用上次失败 task_run 的 plan 重跑
	if req.RequestType == "retry" {
		return f.tryRetry(ctx, req)
	}

	// 分支 3: CanFastTask —— FlowStage 命中（点卡确认后的下一次请求）
	stage := f.Memory.GetStage(req.SessionID)
	if stage.CanFastTask() {
		return f.tryFastTask(ctx, req)
	}

	f.Logger.Debug("fastpath: 未命中快捷路径，走 ReAct",
		zap.String("sessionID", req.SessionID),
		zap.Bool("resume", req.Resume),
		zap.String("requestType", req.RequestType),
		zap.String("stage", string(stage)))
	return false, nil
}

// tryFastTask 点卡确认后的快任务：GetNextAgent 算下一个 agent → 提取前序脚本 → Submit。
//
// 对齐真实系统 buildExecuteAgent（judge.go:172）：脚本确认后，用 OriginMsgID
// 追溯前序脚本（gold_scriptwriter 的 step_output），拼进下一个 agent 的 input。
// 脚本为空时不投递（judge.go:200-205 skip）——避免给下一个 agent 空上下文。
func (f *FastPath) tryFastTask(ctx context.Context, req *FastTaskRequest) (bool, error) {
	if f.Submitter == nil {
		return false, nil // 未接线投递器，不命中
	}
	pipeline := f.Memory.GetPipeline(req.SessionID)
	currentAgent := f.Memory.GetAgent(req.SessionID)
	if pipeline == "" || currentAgent == "" {
		return false, nil // 缺 pipeline/agent 记忆，走正常 ReAct
	}

	// 提取前序脚本：用 OriginMsgID 查到脚本 task_run，取其 step_output 的 final_product。
	// 脚本内容存 cg_task_step.step_output（不在 chat_text 的 sceneItems——go-learn 差异点）。
	scriptContent := ""
	scriptResolved := false
	if req.OriginMsgID != "" && f.StepRepo != nil && f.TaskRunRepo != nil {
		scriptRun, err := f.TaskRunRepo.GetByMsgID(ctx, req.OriginMsgID)
		if err == nil && scriptRun != nil {
			if out, err := f.StepRepo.GetNewestFinishedOutput(ctx, scriptRun.TaskRunID); err == nil && out != "" {
				scriptContent = extractFinalProduct(out)
			}
		}
		// OriginMsgID 非空但脚本查不到：标记"本应能拿到脚本却没拿到"。
		scriptResolved = scriptContent != ""
	}

	agents, err := subtaskexecutor.BuildExecuteAgent(
		subtask.Pipeline(pipeline), subtask.AgentID(currentAgent),
		req.UserID, req.SessionID, req.AgentID, req.Query, scriptContent)
	if err != nil {
		return false, fmt.Errorf("fastpath: BuildExecuteAgent: %w", err)
	}
	if agents == nil {
		// 链结束：无下一个 agent，不投递，交回正常 ReAct（回复完成）
		f.Logger.Info("fastpath: agent 链结束",
			zap.String("sessionID", req.SessionID))
		return false, nil
	}
	// 带 OriginMsgID 但前序脚本没解析出来（如脚本 task_run 缺失/无产物）：
	// 不投递空上下文（对齐真实系统 judge.go:200-205 skip 语义），交回 ReAct 由管家补上下文。
	// 注意：无 OriginMsgID 时（旧链路/直接测试）保持原行为照常投递，子 agent 自行处理。
	if req.OriginMsgID != "" && !scriptResolved {
		f.Logger.Info("fastpath: 前序脚本未解析到，跳过快任务投递（交回 ReAct）",
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
	f.Logger.Info("fastpath: 快任务投递下一个 agent",
		zap.String("pipeline", pipeline),
		zap.String("currentAgent", currentAgent),
		zap.String("sessionID", req.SessionID))
	return true, nil
}

// tryRetry 复用上次失败 task_run 的 plan_steps 重跑。
func (f *FastPath) tryRetry(ctx context.Context, req *FastTaskRequest) (bool, error) {
	if f.Submitter == nil {
		return false, nil
	}
	failedID := f.Memory.GetLastFailedTaskRun(req.SessionID)
	if failedID == 0 {
		return false, nil // 无失败记录，不命中
	}
	failedRun, err := f.TaskRunRepo.GetByTaskRunID(ctx, failedID)
	if err != nil {
		if errors.Is(err, repository.ErrTaskRunNotFound) {
			// 记忆里的失败 id 已失效（如被清理）→ 静默 no-op
			f.Logger.Debug("fastpath: 失败 task_run 已不存在，跳过 retry",
				zap.Int64("taskRunID", failedID))
			return false, nil
		}
		return false, fmt.Errorf("fastpath: 读取失败 task_run: %w", err)
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
	f.Logger.Info("fastpath: Retry 重投上次失败任务",
		zap.Int64("failedTaskRunID", failedID),
		zap.String("sessionID", req.SessionID))
	return true, nil
}

// extractFinalProduct 从 step_output JSON 中提取 final_product 内容。
//
// step_output 结构为 {"final_product":"<交付产物>"}（task_executor.go:139 落库格式）。
// 解析失败或字段缺失时原样返回（兼容旧格式/纯文本 step_output）。
func extractFinalProduct(stepOutput string) string {
	var m struct {
		FinalProduct string `json:"final_product"`
	}
	if err := json.Unmarshal([]byte(stepOutput), &m); err == nil && m.FinalProduct != "" {
		return m.FinalProduct
	}
	return stepOutput
}
