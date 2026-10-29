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

// TaskExecutor is the executor of subtasks (corresponding to the reference system's BaseTaskExecutor.Run).
//
// Lifecycle:
//  1. Load task_run + plan_steps (the replica-lag retry is already done by the Consumer)
//  2. The CAS claim (Pending→Running) is already done by the Consumer; execute directly here
//  3. Take only Agents[0] (one task_run runs one sub-agent)
//  4. Run SubAgent.ReAct (hand-written loop, reusing react.Supervisor's components)
//  5. Write back by NeedWait branch:
//     - No  → Finished + final_product
//     - Yes → Waiting (awaiting the async pipeline callback)
//
// Multi-step chaining does not loop inside this executor (see 05-subagent.md §5.5) — the "next agent"
// is driven externally (conversation: card confirmation + fast-task redispatch; cron: the Poller auto-advances).
type TaskExecutor struct {
	taskRunRepo repository.TaskRunRepository
	stepRepo    repository.TaskStepRepository
	tools       appkittools.ToolsProvider
	llm         react.LLM
	logger      *zap.Logger
}

// TaskExecutorConfig holds the TaskExecutor's dependencies.
type TaskExecutorConfig struct {
	TaskRunRepo repository.TaskRunRepository
	StepRepo    repository.TaskStepRepository
	// Tools is the tool provider available to sub-agents (built-in pipeline tools, see appkittools.NewSubAgentToolRegistry).
	Tools appkittools.ToolsProvider
	// LLM is optional; when nil, appkitfake.FakeSubLLM is used (the replica's table-lookup LLM).
	LLM    react.LLM
	Logger *zap.Logger
}

// NewTaskExecutor creates a TaskExecutor.
func NewTaskExecutor(cfg TaskExecutorConfig) *TaskExecutor {
	return &TaskExecutor{
		taskRunRepo: cfg.TaskRunRepo,
		stepRepo:    cfg.StepRepo,
		tools:       cfg.Tools,
		llm:         cfg.LLM,
		logger:      cfg.Logger,
	}
}

// Execute runs one task_run. Returns shouldAck (true = execution finished, should ACK; false = needs redispatch).
//
// Result handling (05-subagent.md §5.2 ④):
//   - NeedWait=No  → the sub-agent produced the terminal state itself → Finished + final_product
//   - NeedWait=Yes → an async pipeline was triggered → Waiting (awaiting the pipeline callback)
func (e *TaskExecutor) Execute(ctx context.Context, run *model.CGTaskRun) bool {
	plan := model.MustParsePlanSteps(run)
	if len(plan.Agents) == 0 {
		e.logger.Error("task executor: plan_steps has no agents",
			zap.Int64("taskRunID", run.TaskRunID))
		_ = e.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFailed, "plan_steps has no agents")
		return true
	}

	// ③ Take only the first agent (one task_run = one sub-agent step)
	agent := plan.Agents[0]
	e.logger.Info("task executor: running agent",
		zap.Int64("taskRunID", run.TaskRunID),
		zap.String("agentID", agent.AgentID))

	// Create the top-level step (sub-agent level)
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
		e.logger.Error("task executor: failed to create agent step",
			zap.Int64("taskRunID", run.TaskRunID), zap.Error(err))
		_ = e.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFailed, err.Error())
		return true
	}

	// Assemble the SubAgent (reusing react.Supervisor components; the loop is hand-written)
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

	// ④ Write back by NeedWait branch
	if result.NeedWait == model.NeedWaitYes {
		// An async pipeline was triggered: task_run set to Waiting (awaiting the pipeline callback); intermediate_result already written
		e.logger.Info("task executor: NeedWait=Yes, run set to Waiting for the pipeline callback",
			zap.Int64("taskRunID", run.TaskRunID))
		_ = e.taskRunRepo.UpdateByTaskRunID(ctx, run.TaskRunID, map[string]any{
			"status": model.TaskRunStatusWaiting,
		})
		return true
	}

	// NeedWait=No: the sub-agent produced the terminal state itself → Finished
	// final_product is written into the agent step's step_output for the poll backflow to deliver
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

// SubResult is the return value of SubAgent.Run.
type SubResult struct {
	// Content is the sub-agent's final output text.
	Content string
	// NeedWait indicates whether an async pipeline was triggered (1=Yes sets Waiting, 0=No goes terminal directly).
	NeedWait int8
}

// SubAgent is the sub-agent executor (corresponding to the reference system's subagent/executor.go).
//
// It reuses react.Supervisor's components (Strategy/Hooks/LLM/Tools), but the loop is a hand-written
// standard ReAct — the sub Strategy is "Continue while there are tools, Terminal when none",
// without the manager's task_type routing (see 05-subagent.md §5.3).
type SubAgent struct {
	taskRunID int64
	agentStep *model.CGTaskStep
	llm       react.LLM
	tools     appkittools.ToolsProvider
	stepRepo  repository.TaskStepRepository
	logger    *zap.Logger
}

// SubAgentConfig holds the SubAgent's dependencies.
type SubAgentConfig struct {
	TaskRunID int64
	AgentStep *model.CGTaskStep
	// LLM is optional; when nil, appkitfake.FakeSubLLM is used (the replica's table-lookup LLM).
	LLM      react.LLM
	Tools    appkittools.ToolsProvider
	StepRepo repository.TaskStepRepository
	Logger   *zap.Logger
}

// NewSubAgent creates a SubAgent.
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

// Run executes the sub-agent's hand-written ReAct loop. maxStep=10.
//
// Each step:
//   - Create the tool-call sub-step (tool step, parent=agentStep)
//   - Execute the tool → feed the result back as an observation
//   - Pipeline tool (isPipeline) → write intermediate_result + need_wait=Yes + terminate early
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
		// Re-fetch the tool list every round (supports dynamic changes, consistent with the manager engine).
		// Isolate the tool set by the current agentID: ToolRegistry reads agentID from ctx and returns that agent's exclusive tools
		// (aligned with the reference system's GetFunctions filtering by agentID). This way gold_scriptwriter cannot see pipeline tools,
		// and only edit_master can call create_video_task — "after writing the script the script card must be shown" is guaranteed by the tool set.
		toolCtx := appkittools.WithAgentID(ctx, s.agentStep.AgentID)
		defs, err := s.tools.GetFunctionDefs(toolCtx)
		if err != nil {
			return nil, fmt.Errorf("sub agent: get function defs: %w", err)
		}

		resp, err := s.llm.Chat(ctx, messages, defs)
		if err != nil {
			return nil, fmt.Errorf("sub agent: llm chat at step %d: %w", step+1, err)
		}

		// No tool_calls → Terminal
		if len(resp.ToolCalls) == 0 {
			lastContent = resp.Content
			break
		}

		// Continue: execute the tool and feed the result back as an observation
		messages = append(messages, react.Message{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			// Identity propagation: inject agentStep's UserID/SessionID (from run) into ctx;
			// tool Runners read identity from ctx (LLM tool arguments never carry these; ctx is the trusted source).
			res := appkittools.ExecuteToolCall(
				appkittools.WithIdentity(ctx, s.agentStep.UserID, s.agentStep.SessionID),
				s.tools, tc.ID, tc.Name, tc.Args, hooks)
			messages = append(messages, react.Message{
				Role:       "tool",
				Content:    res.Result,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
			// Pipeline tool → terminate early: intermediate_result + need_wait were already written in the OnAfter hook
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

// subAgentToolHooks are the sub-agent's tool-call hooks (07-tool-system.md §7.12).
//
//   - OnBefore: create the tool-call sub-step (cg_task_step, parent=agentStep, Status=Running)
//   - OnAfter: write intermediate_result into the agent step; pipeline tool → need_wait=Yes
type subAgentToolHooks struct {
	taskRunID int64
	agentStep *model.CGTaskStep
	stepRepo  repository.TaskStepRepository
	logger    *zap.Logger

	// currentStepID records the tool sub-step created by the latest OnBefore (serial loop, no concurrency).
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
		h.logger.Error("sub agent hooks: failed to create tool step",
			zap.String("tool", name), zap.Error(err))
		return
	}
	h.currentStepID = step.TaskStepID
}

func (h *subAgentToolHooks) OnAfter(ctx context.Context, result *appkittools.ToolCallResult) {
	// Write intermediate_result (written regardless of success; on failure it is the error observation)
	assigns := map[string]any{
		"intermediate_result": result.Result,
	}
	// Tool sub-step: terminal state (success=Finished / failure=Failed)
	if result.IsError {
		assigns["status"] = model.TaskStepStatusFailed
	} else {
		assigns["status"] = model.TaskStepStatusFinished
	}
	if h.currentStepID != 0 {
		_ = h.stepRepo.UpdateByTaskStepID(ctx, h.currentStepID, assigns)
	}
	// Agent top-level step: pipeline tool → need_wait=Yes + stay Running (awaiting the pipeline callback)
	agentAssigns := map[string]any{
		"intermediate_result": result.Result,
	}
	if result.IsPipeline {
		agentAssigns["need_wait"] = model.NeedWaitYes
	}
	if err := h.stepRepo.UpdateByTaskStepID(ctx, h.agentStep.TaskStepID, agentAssigns); err != nil {
		h.logger.Error("sub agent hooks: failed to update agent step",
			zap.Error(err))
	}
}
