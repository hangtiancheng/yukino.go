package subtaskexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appkitfake "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/fake"
	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"

	"go.uber.org/zap"
)

// TaskExecutor 是子任务的执行体（对应真实系统 BaseTaskExecutor.Run）。
//
// 生命周期：
//  1. 加载 task_run + plan_steps（主从延迟重试已由 Consumer 完成）
//  2. CAS 抢占（Pending→Running）已由 Consumer 完成，此处直接执行
//  3. 只取 Agents[0]（一个 task_run 只跑一个子智能体）
//  4. 跑 SubAgent.ReAct（手写循环，复用 react.Supervisor 的组件）
//  5. 按 NeedWait 分流回写：
//     - No  → Finished + final_product
//     - Yes → Waiting（等异步产线回调）
//
// 多步串联不在本执行器内循环（见 05-subagent.md §5.5）——"下一个 agent"
// 由外部驱动（对话：卡片确认 + 快任务重新投递；Cron：Poller 自动推进）。
type TaskExecutor struct {
	taskRunRepo repository.TaskRunRepository
	stepRepo    repository.TaskStepRepository
	tools       appkittools.ToolsProvider
	llm         react.LLM
	logger      *zap.Logger
}

// TaskExecutorConfig 持有 TaskExecutor 的依赖。
type TaskExecutorConfig struct {
	TaskRunRepo repository.TaskRunRepository
	StepRepo    repository.TaskStepRepository
	// Tools 是子 Agent 可用的工具提供者（内建产线工具，见 appkittools.NewSubAgentToolRegistry）。
	Tools appkittools.ToolsProvider
	// LLM 可选；为 nil 时使用 appkitfake.FakeSubLLM（复刻的查表 LLM）。
	LLM    react.LLM
	Logger *zap.Logger
}

// NewTaskExecutor 创建 TaskExecutor。
func NewTaskExecutor(cfg TaskExecutorConfig) *TaskExecutor {
	return &TaskExecutor{
		taskRunRepo: cfg.TaskRunRepo,
		stepRepo:    cfg.StepRepo,
		tools:       cfg.Tools,
		llm:         cfg.LLM,
		logger:      cfg.Logger,
	}
}

// Execute 执行一个 task_run。返回 shouldAck（true=执行完毕应 ACK；false=需重投）。
//
// 结果处理（05-subagent.md §5.2 ④）：
//   - NeedWait=No  → 子智能体自己产出终态 → Finished + final_product
//   - NeedWait=Yes → 触发了异步产线 → Waiting（等产线回调）
func (e *TaskExecutor) Execute(ctx context.Context, run *model.CGTaskRun) bool {
	plan := model.MustParsePlanSteps(run)
	if len(plan.Agents) == 0 {
		e.logger.Error("task executor: plan_steps 无 agents",
			zap.Int64("taskRunID", run.TaskRunID))
		_ = e.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFailed, "plan_steps 无 agents")
		return true
	}

	// ③ 只取第一个 agent（一个 task_run = 一个子智能体步骤）
	agent := plan.Agents[0]
	e.logger.Info("task executor: running agent",
		zap.Int64("taskRunID", run.TaskRunID),
		zap.String("agentID", agent.AgentID))

	// 建顶层步骤（子智能体级）
	agentStep := &model.CGTaskStep{
		TaskStepID: model.NextID(),
		TaskRunID:  run.TaskRunID,
		UserID:     run.UserID,
		AgentID:    agent.AgentID,
		SessionID:  run.SessionID,
		StepName:   agent.AgentID,
		StepInput:  agent.Input,
		Status:     model.TaskStepStatusRunning,
		StartTime:  time.Now().UnixMilli(),
	}
	if err := e.stepRepo.Insert(ctx, agentStep); err != nil {
		e.logger.Error("task executor: 建 agent step 失败",
			zap.Int64("taskRunID", run.TaskRunID), zap.Error(err))
		_ = e.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFailed, err.Error())
		return true
	}

	// 组装 SubAgent（复用 react.Supervisor 组件，循环手写）
	input := agent.Input
	if input == "" {
		input = run.OriginalQuery
	}
	sub := NewSubAgent(SubAgentConfig{
		TaskRunID: run.TaskRunID,
		AgentStep: agentStep,
		LLM:       e.llm,
		Tools:     e.tools,
		StepRepo:  e.stepRepo,
		Logger:    e.logger,
	})
	result, err := sub.Run(ctx, input)
	if err != nil {
		e.logger.Error("task executor: sub agent run error",
			zap.Int64("taskRunID", run.TaskRunID),
			zap.String("agentID", agent.AgentID),
			zap.Error(err))
		_ = e.stepRepo.UpdateByTaskStepID(ctx, agentStep.TaskStepID, map[string]any{
			"status": model.TaskStepStatusFailed,
		})
		_ = e.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFailed, err.Error())
		return true
	}

	// ④ 按 NeedWait 分流回写
	if result.NeedWait == model.NeedWaitYes {
		// 触发了异步产线：task_run 置 Waiting（等产线回调），intermediate_result 已写
		e.logger.Info("task executor: NeedWait=Yes, run 置 Waiting 等产线回调",
			zap.Int64("taskRunID", run.TaskRunID))
		_ = e.taskRunRepo.UpdateByTaskRunID(ctx, run.TaskRunID, map[string]any{
			"status": model.TaskRunStatusWaiting,
		})
		return true
	}

	// NeedWait=No：子智能体自己产出终态 → Finished
	// final_product 落 agent step 的 step_output，供 poll 回流交付产物
	output, _ := json.Marshal(map[string]any{
		"final_product": result.Content,
	})
	_ = e.stepRepo.UpdateByTaskStepID(ctx, agentStep.TaskStepID, map[string]any{
		"status":      model.TaskStepStatusFinished,
		"step_output": string(output),
		"end_time":    time.Now().UnixMilli(),
	})
	_ = e.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFinished, "")
	return true
}

// SubResult 是 SubAgent.Run 的返回结果。
type SubResult struct {
	// Content 是子智能体的最终产出文本。
	Content string
	// NeedWait 表示是否触发了异步产线（1=Yes 置 Waiting，0=No 直接终态）。
	NeedWait int8
}

// SubAgent 是子智能体执行器（对应真实系统 subagent/executor.go）。
//
// 复用了 react.Supervisor 的组件（Strategy/Hooks/LLM/Tools），但循环是手写的
// 标准 ReAct——子 Strategy 是"有工具就 Continue、无就 Terminal"，
// 不做管家那种 task_type 路由（见 05-subagent.md §5.3）。
type SubAgent struct {
	taskRunID int64
	agentStep *model.CGTaskStep
	llm       react.LLM
	tools     appkittools.ToolsProvider
	stepRepo  repository.TaskStepRepository
	logger    *zap.Logger
}

// SubAgentConfig 持有 SubAgent 的依赖。
type SubAgentConfig struct {
	TaskRunID int64
	AgentStep *model.CGTaskStep
	// LLM 可选；为 nil 时使用 appkitfake.FakeSubLLM（复刻的查表 LLM）。
	LLM      react.LLM
	Tools    appkittools.ToolsProvider
	StepRepo repository.TaskStepRepository
	Logger   *zap.Logger
}

// NewSubAgent 创建 SubAgent。
func NewSubAgent(cfg SubAgentConfig) *SubAgent {
	llm := cfg.LLM
	if llm == nil {
		llm = appkitfake.DefaultSubLLM()
	}
	return &SubAgent{
		taskRunID: cfg.TaskRunID,
		agentStep: cfg.AgentStep,
		llm:       llm,
		tools:     cfg.Tools,
		stepRepo:  cfg.StepRepo,
		logger:    cfg.Logger,
	}
}

// Run 执行子智能体的手写 ReAct 循环。maxStep=10。
//
// 每步：
//   - 建工具调用子步骤（tool step，parent=agentStep）
//   - 执行工具 → 结果当 observation 塞回
//   - 产线工具（isPipeline）→ 写 intermediate_result + need_wait=Yes + 提前终止
func (s *SubAgent) Run(ctx context.Context, input string) (*SubResult, error) {
	hooks := &subAgentToolHooks{
		taskRunID: s.taskRunID,
		agentStep: s.agentStep,
		stepRepo:  s.stepRepo,
		logger:    s.logger,
	}

	messages := []react.Message{
		{Role: "system", Content: "你是一个子智能体，请用 JSON 格式回复。"},
		{Role: "user", Content: input},
	}

	maxStep := 10
	var lastContent string
	for step := 0; step < maxStep; step++ {
		// 每轮重取工具列表（支持动态变化，与管家引擎一致）。
		// 按当前 agentID 隔离工具集：ToolRegistry 从 ctx 读 agentID 返回该 agent 专属工具
		// （对齐真实系统 GetFunctions 按 agentID 过滤）。这样 gold_scriptwriter 看不到产线工具，
		// edit_master 才可调 create_video_task——"写完脚本必须走脚本卡"由工具集保证。
		toolCtx := appkittools.WithAgentID(ctx, s.agentStep.AgentID)
		defs, err := s.tools.GetFunctionDefs(toolCtx)
		if err != nil {
			return nil, fmt.Errorf("sub agent: get function defs: %w", err)
		}

		resp, err := s.llm.Chat(ctx, messages, defs)
		if err != nil {
			return nil, fmt.Errorf("sub agent: llm chat at step %d: %w", step+1, err)
		}

		// 无 tool_calls → Terminal
		if len(resp.ToolCalls) == 0 {
			lastContent = resp.Content
			break
		}

		// Continue：执行工具，结果当 observation 塞回
		messages = append(messages, react.Message{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			// 身份透传：把 agentStep 的 UserID/SessionID（来自 run）注入 ctx，
			// 工具 Runner 从 ctx 读身份（LLM 工具参数不会传这些，ctx 是可信来源）。
			res := appkittools.ExecuteToolCall(
				appkittools.WithIdentity(ctx, s.agentStep.UserID, s.agentStep.SessionID),
				s.tools, tc.ID, tc.Name, tc.Args, hooks)
			messages = append(messages, react.Message{
				Role:       "tool",
				Content:    res.Result,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
			// 产线工具 → 提前终止：intermediate_result + need_wait 已在 OnAfter hook 写入
			if res.IsPipeline {
				return &SubResult{
					Content:  res.Result,
					NeedWait: model.NeedWaitYes,
				}, nil
			}
		}
	}

	if lastContent == "" {
		lastContent = "子智能体已处理完毕"
	}
	return &SubResult{Content: lastContent, NeedWait: model.NeedWaitNo}, nil
}

// subAgentToolHooks 是子 Agent 的工具调用钩子（07-tool-system.md §7.12）。
//
//   - OnBefore: 建工具调用子步骤（cg_task_step, parent=agentStep, Status=Running）
//   - OnAfter: 写 intermediate_result 到 agent step；产线工具 → need_wait=Yes
type subAgentToolHooks struct {
	taskRunID int64
	agentStep *model.CGTaskStep
	stepRepo  repository.TaskStepRepository
	logger    *zap.Logger

	// currentStepID 记录最近一次 OnBefore 建的工具子步骤（串行循环，无并发）。
	currentStepID int64
}

func (h *subAgentToolHooks) OnBefore(ctx context.Context, tcID, name, args string) {
	step := &model.CGTaskStep{
		TaskStepID:   model.NextID(),
		TaskRunID:    h.taskRunID,
		ParentStepID: h.agentStep.TaskStepID,
		UserID:       h.agentStep.UserID,
		AgentID:      h.agentStep.AgentID,
		SessionID:    h.agentStep.SessionID,
		StepName:     name,
		StepInput:    args,
		Status:       model.TaskStepStatusRunning,
		StartTime:    time.Now().UnixMilli(),
	}
	if err := h.stepRepo.Insert(ctx, step); err != nil {
		h.logger.Error("sub agent hooks: 建 tool step 失败",
			zap.String("tool", name), zap.Error(err))
		return
	}
	h.currentStepID = step.TaskStepID
}

func (h *subAgentToolHooks) OnAfter(ctx context.Context, result *appkittools.ToolCallResult) {
	// 写 intermediate_result（无论成败都写，失败时是错误 observation）
	assigns := map[string]any{
		"intermediate_result": result.Result,
	}
	// 工具子步骤：终态（成功=Finished / 失败=Failed）
	if result.IsError {
		assigns["status"] = model.TaskStepStatusFailed
	} else {
		assigns["status"] = model.TaskStepStatusFinished
	}
	if h.currentStepID != 0 {
		_ = h.stepRepo.UpdateByTaskStepID(ctx, h.currentStepID, assigns)
	}
	// agent 顶层步骤：产线工具 → need_wait=Yes + 保持 Running（等产线回调）
	agentAssigns := map[string]any{
		"intermediate_result": result.Result,
	}
	if result.IsPipeline {
		agentAssigns["need_wait"] = model.NeedWaitYes
	}
	if err := h.stepRepo.UpdateByTaskStepID(ctx, h.agentStep.TaskStepID, agentAssigns); err != nil {
		h.logger.Error("sub agent hooks: 更新 agent step 失败",
			zap.Error(err))
	}
}
