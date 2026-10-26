package appkitrunner

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	appkitfake "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/fake"
	appkitllm "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/llm"
	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
)

// runnerTracerName 是 Runner 的链路追踪器名称。
// 每次 Run 时用 otel.Tracer(runnerTracerName) 现取：全局 provider 可能在
// 测试中被子进程替换（otel.SetTracerProvider），包 init 捕获的 tracer 会钉死旧 provider。
const runnerTracerName = "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/provider"

// SupervisorRunner 封装了 ReAct 引擎的完整运行流程。
//
// 同步路径和异步 Worker 均通过此结构体执行业务逻辑，
// 差异仅在于 onFrame 回调的目标：
//   - 同步路径: onFrame → pushFrame → ctx.Push(ConOnePackSSEKey, json)
//   - 异步路径: onFrame → chunkStore.Write(msgID, json, false)
//
// 这使得 ReAct 引擎 (react.Supervisor) 完全零修改即可在两种路径下复用。
//
// Phase 3 扩展：RunWithOutput 在管家产出 new_agents_plan 时通过 onOutput 回调
// 通知调用方（Worker）投递子任务——这是"管家 → 子任务"链路的接线点。
type SupervisorRunner struct {
	frameSuffixes []func()

	llm      react.LLM
	strategy react.Strategy
	hooks    react.Hooks

	// planStrategy 是 RunWithOutput 的管家决策策略（约束链）。由接线层（app.go/worker）
	// 注入 manager.ManagerStrategy；nil 时回退 FakePlanStrategy（Phase 3 行为不变）。
	// 通过接口注入解耦：provider 组件层不直接 import manager 业务包，避免依赖环。
	planStrategy react.Strategy

	// planTools 是 RunWithOutput 的管家工具注册表（协议工具 generate_response/idea_king_api）。
	// 由接线层注入 manager.NewManagerToolRegistry()；nil 时回退 FakeTools。
	// 同样通过接口解耦，provider 组件层不直接 import manager 业务包。
	planTools react.Tools

	// onInterrupt 是 HIL 断点回调：引擎保存 checkpoint 到 agentCtx 后，Runner 透传给调用方持久化。
	onInterrupt OnInterrupt

	// prepare 是 prompt 注入函数。SetPrepare 设置后优先，否则 DefaultAssistantPrepare。
	prepare PrepareFunc

	// streaming 是流式开关：开启时 LLM 走 ChatStream 逐 chunk 实时出 text-delta 帧
	// （替换终态 10-rune 事后切块）。默认关闭（同步 supervisor_agent 路径零变化）。
	streaming bool
	// streamedAnyContent 记录本轮 Run 流式是否发过任何 content 帧。
	// 用于终态判断：强制 tool_choice=required 时 LLM 可能只返回 tool_call（content 空），
	// 流式无帧可发，此时必须补发终态文本（friendly_reply）保证前端有内容。
	streamedAnyContent bool
}

// NewRunner creates a SupervisorRunner with default providers.
func NewRunner() *SupervisorRunner {
	return &SupervisorRunner{}
}

// EnableStreaming 开启流式输出：LLM 走 ChatStream 逐 chunk 实时出 text-delta 帧，
// 替换终态 10-rune 事后切块。默认关闭（同步 supervisor_agent 路径零变化）。
func (r *SupervisorRunner) EnableStreaming() { r.streaming = true }

// buildTextDeltaCallback 把 LLM 流式 chunk 转 text-delta 帧回调 onFrame。
// 同一次 stream 内共享一个 textId（参考 comic buildStreamCallback 的 ensureTextID）。
// 跳过 isEndingChunk=true 的末包（其 content 是完整累积值，逐 chunk 已发过，避免重复帧）。
func (r *SupervisorRunner) buildTextDeltaCallback(onFrame func(SSEFrame)) react.StreamCallbackFunc {
	textID := ""
	return func(_ context.Context, payload *react.StreamedChatResponsePayload, isEnding bool) error {
		if isEnding || payload == nil || len(payload.Choices) == 0 {
			return nil
		}
		delta := payload.Choices[0].Delta
		if textID == "" {
			textID = "t1"
		}
		if delta.Content != "" {
			r.streamedAnyContent = true
			onFrame(SSEFrame{Type: "text-delta", TextID: textID, Content: delta.Content})
		}
		return nil
	}
}

// SetProviders 注入 LLM/Strategy/Hooks。传入 nil 表示使用默认值。
func (r *SupervisorRunner) SetProviders(llm react.LLM, strategy react.Strategy, hooks react.Hooks) {
	r.llm = llm
	r.strategy = strategy
	r.hooks = hooks
}

// 私有方法：取组件（nil 时用默认）。
func (r *SupervisorRunner) llmOr() react.LLM {
	if r.llm != nil {
		return r.llm
	}
	if appkitllm.DefaultRealLLM != nil {
		return appkitllm.DefaultRealLLM
	}
	return &appkitfake.FakeLLM{}
}

func (r *SupervisorRunner) strategyOr() react.Strategy {
	if r.strategy != nil {
		return r.strategy
	}
	return appkitfake.FakeStrategy{}
}

func (r *SupervisorRunner) hooksOr() react.Hooks {
	if r.hooks != nil {
		return r.hooks
	}
	return &appkitfake.FakeHooks{}
}

// SetPrepare 注入 prompt（业务层调用）。nil 时用 DefaultAssistantPrepare。
func (r *SupervisorRunner) SetPrepare(fn PrepareFunc) { r.prepare = fn }

// prepareOr 取 prompt：优先注入值，否则默认通用 assistant。
func (r *SupervisorRunner) prepareOr() PrepareFunc {
	if r.prepare != nil {
		return r.prepare
	}
	return DefaultAssistantPrepare
}

// RunWithOutput 的默认组件（Plan 专属：FakePlanLLM/FakePlanStrategy）。
func (r *SupervisorRunner) planLLMOr() react.LLM {
	if r.llm != nil {
		return r.llm
	}
	if appkitllm.DefaultManagerLLM != nil {
		return appkitllm.DefaultManagerLLM
	}
	if appkitllm.DefaultRealLLM != nil {
		return appkitllm.DefaultRealLLM
	}
	return &appkitfake.FakePlanLLM{}
}

func (r *SupervisorRunner) planStrategyOr() react.Strategy {
	if r.strategy != nil {
		return r.strategy
	}
	if r.planStrategy != nil {
		return r.planStrategy
	}
	return appkitfake.FakePlanStrategy{}
}

// SetPlanStrategy 注入 RunWithOutput 的管家决策策略（约束链）。
// 接线层（app.go/worker）在配置了真实 LLM 时注入 manager.ManagerStrategy。
func (r *SupervisorRunner) SetPlanStrategy(s react.Strategy) {
	r.planStrategy = s
}

// SetPlanTools 注入 RunWithOutput 的管家工具注册表（协议工具）。
// 接线层注入 manager.NewManagerToolRegistry()。
func (r *SupervisorRunner) SetPlanTools(t react.Tools) {
	r.planTools = t
}

// planToolsOr 取 RunWithOutput 的工具：优先注入值，否则 FakeTools。
func (r *SupervisorRunner) planToolsOr() react.Tools {
	if r.planTools != nil {
		return r.planTools
	}
	return appkittools.FakeTools{}
}

// AddFrameSuffix registers a callback invoked after ReAct completes
// (before the final message-complete frame).
func (r *SupervisorRunner) AddFrameSuffix(fn func()) {
	r.frameSuffixes = append(r.frameSuffixes, fn)
}

// SSEFrame 是 ReAct 执行过程中产出的一帧，对应 SSE 协议的一个事件。
type SSEFrame struct {
	// Type 是 SSE 事件类型：message-start / text-delta / tool-call-start / message-complete / error /
	// ui-block-start / ui-block-complete
	Type string `json:"type"`
	// Content 是 text-delta 或 error 事件的文本内容
	Content string `json:"content,omitempty"`
	// MsgID 是 message-start / message-complete 事件的消息 ID
	MsgID string `json:"msgId,omitempty"`
	// TextID 是 text-delta 事件的文本块 ID（用于前端增量渲染）
	TextID string `json:"textId,omitempty"`
	// BlockID 是 ui-block-start / ui-block-complete 事件的卡片块 ID
	BlockID string `json:"blockId,omitempty"`
	// UIMeta 是 ui-block-start 事件的 UI 组件元数据（flex 布局 + 组件模板）
	UIMeta any `json:"uiMeta,omitempty"`
	// UIData 是 ui-block-start 事件的实际数据（如 PromptOptionGroup 的 promptItems）
	UIData any `json:"uiData,omitempty"`
	// Interactive 标记 ui-block-start 卡片是否可交互（用户可点击）
	Interactive bool `json:"interactive,omitempty"`
}

// Run executes the full ReAct pipeline: setup fake providers → push message-start →
// run Supervisor → emit text-delta chunks → push message-complete.
// Returns the full response text.
func (r *SupervisorRunner) Run(ctx context.Context, query, serverMsgID string, onFrame func(SSEFrame)) (string, error) {
	llm := r.llmOr()
	tools := appkittools.FakeTools{}
	strategy := r.strategyOr()
	hooks := r.hooksOr()

	sup := &react.Supervisor{}
	sup.SetMaxStep(10)
	sup.SetMaxRelay(3)
	sup.SetMaxRetry(2)
	sup.SetLLM(llm)
	sup.SetTools(tools)
	sup.SetStrategy(strategy)
	sup.SetHooks(hooks)
	sup.SetPrepare(r.prepareOr())

	// 流式开关：开启时 LLM 走 ChatStream 逐 chunk 实时出 text-delta（替换终态切块）
	if r.streaming {
		sup.SetStreamCallback(r.buildTextDeltaCallback(onFrame))
	}

	agentCtx := react.NewAgentContext(query)
	// 先建 supervisor.react span 再写 agentCtx.Ctx：LLM 调用（react.Supervisor 读 c.Ctx）
	// 会挂进 span 下，而不是父 span（worker.process / HTTP）下。
	ctx, span := otel.Tracer(runnerTracerName).Start(ctx, "supervisor.react")
	defer span.End()
	agentCtx.Ctx = ctx
	span.SetAttributes(attribute.String("msg_id", serverMsgID))

	// Push message-start frame
	onFrame(SSEFrame{
		Type:  "message-start",
		MsgID: serverMsgID,
	})

	result, err := sup.Run(agentCtx)
	if err != nil {
		onFrame(SSEFrame{
			Type:    "error",
			Content: "agent 执行出错: " + err.Error(),
		})
		return "", err
	}

	// HIL 中断：透传 checkpoint 给调用方持久化 + 产 interrupt 帧
	if result.Interrupted {
		if cp := r.takeCheckpoint(agentCtx); cp != nil && r.onInterrupt != nil {
			r.onInterrupt(cp)
		}
		onFrame(SSEFrame{
			Type:    "interrupt",
			MsgID:   serverMsgID,
			Content: result.Content,
		})
		onFrame(SSEFrame{Type: "message-complete", MsgID: serverMsgID})
		return result.Content, nil
	}

	// 热点卡：strategy 直出（同步路径若接管家策略也会产出）。
	if card := r.extractHotCard(result); card != nil {
		r.emitCardFrames(onFrame, serverMsgID, card)
		for _, fn := range r.frameSuffixes {
			fn()
		}
		onFrame(SSEFrame{Type: "message-complete", MsgID: serverMsgID})
		return result.Content, nil
	}

	// 终态文本：流式已实时发过 content 则跳过（避免重复）；否则必须补发——
	// 强制 tool_choice=required 时 LLM 可能只返回 tool_call（content 空），
	// 流式无帧可发，补发 strategy 封装的终态文本（friendly_reply）。
	r.emitTerminalText(onFrame, result.Content)

	// Push message-complete frame
	for _, fn := range r.frameSuffixes {
		fn()
	}
	onFrame(SSEFrame{
		Type:  "message-complete",
		MsgID: serverMsgID,
	})

	return result.Content, nil
}

// emitTerminalText 按流式状态决定是否补发终态文本帧。
//   - 非流式：正常 10-rune 切块（原模拟流）
//   - 流式已发过 content：跳过（已实时出帧）
//   - 流式但没发过 content：补发终态文本（保证前端有内容）
func (r *SupervisorRunner) emitTerminalText(onFrame func(SSEFrame), text string) {
	if r.streaming && r.streamedAnyContent {
		return
	}
	runes := []rune(text)
	const chunkSize = 10
	for i := 0; i < len(runes); i += chunkSize {
		end := min(i+chunkSize, len(runes))
		onFrame(SSEFrame{Type: "text-delta", TextID: "t1", Content: string(runes[i:end])})
	}
}

// extractHotCard 从终态 AgentResult.Ext 提取热点卡数据（strategy 直出透传）。
// 优先类型断言（同进程），fallback 到 json.Unmarshal（跨序列化鲁棒）。无卡返回 nil。
func (r *SupervisorRunner) extractHotCard(result *react.AgentResult) *HotCardData {
	if result == nil {
		return nil
	}
	if v, ok := result.Ext["hot_card"].(*HotCardData); ok && v != nil {
		return v
	}
	// 跨序列化兜底：hot_card 可能是 JSON 字符串或 map
	if raw, ok := result.Ext["hot_card"].(string); ok && raw != "" {
		var card HotCardData
		if err := json.Unmarshal([]byte(raw), &card); err == nil && len(card.TitleData) > 0 {
			return &card
		}
	}
	return nil
}

// emitCardFrames 渲染热点卡帧序列：hotspot text-delta → ui-block-start → ui-block-complete。
// 对齐参考抓包帧序（message-start → text-delta(热点markdown) → ui-block-start →
// ui-block-complete → message-complete）。
func (r *SupervisorRunner) emitCardFrames(onFrame func(SSEFrame), serverMsgID string, card *HotCardData) {
	if card.HotspotData != "" {
		onFrame(SSEFrame{Type: "text-delta", TextID: "t1", Content: card.HotspotData})
	}
	for _, f := range BuildHotIssueFrames(serverMsgID, card) {
		onFrame(f)
	}
}

// RunResume 与 Run 相同，但用 HIL checkpoint 预置 AgentContext 后从断点恢复。
// 预置 Messages/Step 并置 Interrupted=true（resume 轮 strategy 据此直接 Terminal）。
func (r *SupervisorRunner) RunResume(ctx context.Context, query, serverMsgID string, onFrame func(SSEFrame), cp *react.Checkpoint) (string, error) {
	llm := r.llmOr()
	tools := appkittools.FakeTools{}
	strategy := r.strategyOr()
	hooks := r.hooksOr()

	sup := &react.Supervisor{}
	sup.SetMaxStep(10)
	sup.SetMaxRelay(3)
	sup.SetMaxRetry(2)
	sup.SetLLM(llm)
	sup.SetTools(tools)
	sup.SetStrategy(strategy)
	sup.SetHooks(hooks)
	sup.SetPrepare(r.prepareOr())

	// 流式开关：开启时 LLM 走 ChatStream 逐 chunk 实时出 text-delta（替换终态切块）
	if r.streaming {
		sup.SetStreamCallback(r.buildTextDeltaCallback(onFrame))
	}

	agentCtx := react.NewAgentContext(query)
	// 先建 supervisor.react span 再写 agentCtx.Ctx：LLM 调用（react.Supervisor 读 c.Ctx）
	// 会挂进 span 下，而不是父 span（worker.process / HTTP）下。
	ctx, span := otel.Tracer(runnerTracerName).Start(ctx, "supervisor.react")
	defer span.End()
	agentCtx.Ctx = ctx
	span.SetAttributes(attribute.String("msg_id", serverMsgID))
	agentCtx.Runtime.Messages = cp.Messages
	agentCtx.Runtime.Step = cp.Step
	agentCtx.Runtime.Interrupted = true

	onFrame(SSEFrame{Type: "message-start", MsgID: serverMsgID})

	result, err := sup.Run(agentCtx)
	if err != nil {
		onFrame(SSEFrame{Type: "error", Content: "agent 执行出错: " + err.Error()})
		return "", err
	}

	// 终态文本：流式已发过则跳过，否则补发（保证前端有内容）
	r.emitTerminalText(onFrame, result.Content)
	for _, fn := range r.frameSuffixes {
		fn()
	}
	onFrame(SSEFrame{Type: "message-complete", MsgID: serverMsgID})
	return result.Content, nil
}

// FrameToJSON serializes an SSEFrame to its JSON representation.
func FrameToJSON(f SSEFrame) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// PlanOutput 是管家 ReAct 产出 new_agents_plan 时的解析结果。
type PlanOutput struct {
	// TaskType 任务类型（"new_agents_plan"）。
	TaskType string `json:"task_type"`
	// FriendlyReply 展示给用户的回复文本。
	FriendlyReply string `json:"friendly_reply"`
	// AgentsPipeline 管家选的产线（对齐真实系统 PlanConfig.AgentsPipeline）。
	// LLM 决定链，产线限定合法 agent 集合；随 ToModelPlan 落库供下游记录/复用。
	AgentsPipeline string `json:"agents_pipeline,omitempty"`
	// PlanSteps 计划步骤 JSON（含完整 Agents 列表）。
	PlanSteps PlanStepsPayload `json:"plan_steps"`
}

// PlanStepsPayload 是管家产出计划的结构（与 model.PlanSteps 对应）。
type PlanStepsPayload struct {
	Agents []PlanAgentPayload `json:"agents"`
}

// PlanAgentPayload 计划中的一个 agent。
type PlanAgentPayload struct {
	AgentID string `json:"agent_id"`
	Input   string `json:"input"`
}

// OnOutput 回调签名：管家产出 new_agents_plan 时触发。
type OnOutput func(plan *PlanOutput)

// OnInterrupt 回调签名：ReAct 产出 HIL 断点（ActionInterrupt）时触发，携带完整 checkpoint。
// 调用方（Worker）在此 SaveCheckpoint 持久化恢复点，使 interrupt → persist → resume 循环闭环。
type OnInterrupt func(cp *react.Checkpoint)

// SetOnInterrupt 注册 HIL 断点回调（调用方在此 SaveCheckpoint 持久化恢复点）。
func (r *SupervisorRunner) SetOnInterrupt(fn OnInterrupt) {
	r.onInterrupt = fn
}

// takeCheckpoint 从 AgentContext 取引擎存的 *react.Checkpoint（取不到则 nil）。
func (r *SupervisorRunner) takeCheckpoint(agentCtx *react.AgentContext) *react.Checkpoint {
	if v := agentCtx.Get(react.CheckpointKey); v != nil {
		if cp, ok := v.(*react.Checkpoint); ok {
			return cp
		}
	}
	return nil
}

// RunWithOutput 与 Run 相同，但在管家产出 new_agents_plan 时通过 onOutput 通知调用方。
// 用于接线 Phase 3 的"管家 → Submitter"链路。
func (r *SupervisorRunner) RunWithOutput(ctx context.Context, query, serverMsgID string, onFrame func(SSEFrame), onOutput OnOutput) (string, error) {
	llm := r.planLLMOr()
	tools := r.planToolsOr()
	strategy := r.planStrategyOr()
	hooks := r.hooksOr()

	sup := &react.Supervisor{}
	sup.SetMaxStep(10)
	sup.SetMaxRelay(3)
	sup.SetMaxRetry(2)
	sup.SetLLM(llm)
	sup.SetTools(tools)
	sup.SetStrategy(strategy)
	sup.SetHooks(hooks)
	sup.SetPrepare(r.prepareOr())

	// 流式开关：开启时 LLM 走 ChatStream 逐 chunk 实时出 text-delta（替换终态切块）
	if r.streaming {
		sup.SetStreamCallback(r.buildTextDeltaCallback(onFrame))
	}

	agentCtx := react.NewAgentContext(query)
	// 先建 supervisor.react span 再写 agentCtx.Ctx：LLM 调用（react.Supervisor 读 c.Ctx）
	// 会挂进 span 下，而不是父 span（worker.process / HTTP）下。
	ctx, span := otel.Tracer(runnerTracerName).Start(ctx, "supervisor.react")
	defer span.End()
	agentCtx.Ctx = ctx
	span.SetAttributes(attribute.String("msg_id", serverMsgID))

	onFrame(SSEFrame{Type: "message-start", MsgID: serverMsgID})

	result, err := sup.Run(agentCtx)
	if err != nil {
		onFrame(SSEFrame{Type: "error", Content: "agent 执行出错: " + err.Error()})
		return "", err
	}

	// HIL 中断：透传 checkpoint 给调用方持久化 + 产 interrupt 帧。
	// 中断时不触发 onOutput、不拆 text-delta。
	if result.Interrupted {
		if cp := r.takeCheckpoint(agentCtx); cp != nil && r.onInterrupt != nil {
			r.onInterrupt(cp)
		}
		onFrame(SSEFrame{
			Type:    "interrupt",
			MsgID:   serverMsgID,
			Content: result.Content,
		})
		onFrame(SSEFrame{Type: "message-complete", MsgID: serverMsgID})
		return result.Content, nil
	}

	// 热点卡：strategy 直出（idea_king_api 调外部热点服务 → Terminal + hot_card）。
	// 渲染成 ui-block-start/ui-block-complete 卡帧序列，不进 plan/onOutput。
	if card := r.extractHotCard(result); card != nil {
		r.emitCardFrames(onFrame, serverMsgID, card)
		for _, fn := range r.frameSuffixes {
			fn()
		}
		onFrame(SSEFrame{Type: "message-complete", MsgID: serverMsgID})
		return result.Content, nil
	}

	// 解析终态 JSON：若为 new_agents_plan，触发 onOutput 回调。
	// 优先取 Strategy 透传的 plan_output（完整 JSON），fallback 到解析 result.Content。
	rawPlan, _ := result.Ext["plan_output"].(string)
	if rawPlan == "" {
		rawPlan = result.Content
	}
	if plan := ParsePlanOutput(rawPlan); plan != nil {
		// 终态结构化输出（new_agents_plan）：friendly_reply 帧必须发（流式 text-delta
		// 是 LLM 原始 content，这里发的是终态封装的友好回复），onOutput 投子任务。
		onFrame(SSEFrame{
			Type:    "text-delta",
			TextID:  "t1",
			Content: plan.FriendlyReply,
		})
		if onOutput != nil {
			onOutput(plan)
		}
	} else {
		// 普通回复：终态文本。流式已实时发过 content 则跳过（避免重复），
		// 否则补发——强制 tool_choice=required 时 LLM 可能只返回 tool_call，
		// 流式无帧可发，必须补发 strategy 封装的终态文本保证前端有内容。
		r.emitTerminalText(onFrame, result.Content)
	}

	for _, fn := range r.frameSuffixes {
		fn()
	}
	onFrame(SSEFrame{Type: "message-complete", MsgID: serverMsgID})

	return result.Content, nil
}

// ParsePlanOutput 解析 LLM 终态 JSON，若 task_type 为 new_agents_plan 返回计划，否则 nil。
func ParsePlanOutput(content string) *PlanOutput {
	var plan PlanOutput
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil
	}
	if plan.TaskType != "new_agents_plan" {
		return nil
	}
	return &plan
}

// ToModelPlan 将 PlanOutput 转换为 model.PlanSteps（供 Submitter 落库）。
func (p *PlanOutput) ToModelPlan() *model.PlanSteps {
	agents := make([]model.PlanAgent, 0, len(p.PlanSteps.Agents))
	for _, a := range p.PlanSteps.Agents {
		agents = append(agents, model.PlanAgent{
			AgentID: a.AgentID,
			Input:   a.Input,
		})
	}
	return &model.PlanSteps{AgentsPipeline: p.AgentsPipeline, Agents: agents}
}
