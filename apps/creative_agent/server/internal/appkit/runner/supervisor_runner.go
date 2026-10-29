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

// runnerTracerName is the tracer name of the Runner.
// Each Run fetches otel.Tracer(runnerTracerName) on the fly: the global provider may be
// replaced in tests (otel.SetTracerProvider), and a tracer captured at package init would pin the old provider.
const runnerTracerName = "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/provider"

// SupervisorRunner encapsulates the full run flow of the ReAct engine.
//
// Both the sync path and the async Worker execute business logic through this struct;
// the only difference is the target of the onFrame callback:
//   - Sync path: onFrame → pushFrame → ctx.Push(ConOnePackSSEKey, json)
//   - Async path: onFrame → chunkStore.Write(msgID, json, false)
//
// This lets the ReAct engine (react.Supervisor) be reused on both paths with zero modifications.
//
// Phase 3 extension: RunWithOutput notifies the caller (Worker) via the onOutput callback when the
// manager produces new_agents_plan, so it can dispatch subtasks — the wiring point of the "manager → subtask" chain.
type SupervisorRunner struct {
	frameSuffixes []func()

	llm      react.LLM
	strategy react.Strategy
	hooks    react.Hooks

	// planStrategy is the manager decision strategy (constraint chain) for RunWithOutput. The wiring layer (app.go/worker)
	// injects manager.ManagerStrategy; when nil it falls back to FakePlanStrategy (Phase 3 behavior unchanged).
	// Interface injection decouples the layers: the provider component layer does not import the manager business package directly, avoiding a dependency cycle.
	planStrategy react.Strategy

	// planTools is the manager tool registry for RunWithOutput (protocol tools generate_response/idea_king_api).
	// The wiring layer injects manager.NewManagerToolRegistry(); when nil it falls back to FakeTools.
	// Also decoupled via interfaces: the provider component layer does not import the manager business package directly.
	planTools react.Tools

	// onInterrupt is the HIL breakpoint callback: after the engine saves the checkpoint into agentCtx, the Runner passes it through for the caller to persist.
	onInterrupt OnInterrupt

	// prepare is the prompt-injection function. The value set via SetPrepare takes priority; otherwise DefaultAssistantPrepare.
	prepare PrepareFunc

	// streaming is the streaming switch: when enabled the LLM uses ChatStream and emits text-delta frames per chunk in realtime
	// (replacing the terminal 10-rune after-the-fact chunking). Disabled by default (zero change to the sync supervisor_agent path).
	streaming bool
	// streamedAnyContent records whether this Run's streaming emitted any content frame.
	// Used for the terminal decision: with forced tool_choice=required the LLM may return only a tool_call (empty content),
	// leaving no frames to stream; the terminal text (friendly_reply) must then be emitted to guarantee the frontend has content.
	streamedAnyContent bool
}

// NewRunner creates a SupervisorRunner with default providers.
func NewRunner() *SupervisorRunner {
	return &SupervisorRunner{}
}

// EnableStreaming turns on streaming output: the LLM uses ChatStream and emits text-delta frames per chunk in realtime,
// replacing the terminal 10-rune after-the-fact chunking. Disabled by default (zero change to the sync supervisor_agent path).
func (r *SupervisorRunner) EnableStreaming() { r.streaming = true }

// buildTextDeltaCallback converts LLM streaming chunks into text-delta frames via the onFrame callback.
// One textId is shared within a single stream (see comic buildStreamCallback's ensureTextID).
// The final chunk with isEndingChunk=true is skipped (its content is the full accumulated value already sent chunk by chunk, avoiding duplicate frames).
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

// SetProviders injects LLM/Strategy/Hooks. Passing nil means using the defaults.
func (r *SupervisorRunner) SetProviders(llm react.LLM, strategy react.Strategy, hooks react.Hooks) {
	r.llm = llm
	r.strategy = strategy
	r.hooks = hooks
}

// Private helpers: fetch components (defaults when nil).
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

// SetPrepare injects the prompt (called by the business layer). When nil, DefaultAssistantPrepare is used.
func (r *SupervisorRunner) SetPrepare(fn PrepareFunc) { r.prepare = fn }

// prepareOr fetches the prompt: the injected value takes priority, otherwise the generic assistant default.
func (r *SupervisorRunner) prepareOr() PrepareFunc {
	if r.prepare != nil {
		return r.prepare
	}
	return DefaultAssistantPrepare
}

// Default components for RunWithOutput (plan-specific: FakePlanLLM/FakePlanStrategy).
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

// SetPlanStrategy injects the manager decision strategy (constraint chain) for RunWithOutput.
// The wiring layer (app.go/worker) injects manager.ManagerStrategy when a real LLM is configured.
func (r *SupervisorRunner) SetPlanStrategy(s react.Strategy) {
	r.planStrategy = s
}

// SetPlanTools injects the manager tool registry (protocol tools) for RunWithOutput.
// The wiring layer injects manager.NewManagerToolRegistry().
func (r *SupervisorRunner) SetPlanTools(t react.Tools) {
	r.planTools = t
}

// planToolsOr fetches the tools for RunWithOutput: the injected value takes priority, otherwise FakeTools.
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

// SSEFrame is one frame produced during ReAct execution, corresponding to one SSE protocol event.
type SSEFrame struct {
	// Type is the SSE event type: message-start / text-delta / tool-call-start / message-complete / error /
	// ui-block-start / ui-block-complete
	Type string `json:"type"`
	// Content is the text content of a text-delta or error event
	Content string `json:"content,omitempty"`
	// MsgID is the message ID of message-start / message-complete events
	MsgID string `json:"msgId,omitempty"`
	// TextID is the text-block ID of text-delta events (for incremental frontend rendering)
	TextID string `json:"textId,omitempty"`
	// BlockID is the card-block ID of ui-block-start / ui-block-complete events
	BlockID string `json:"blockId,omitempty"`
	// UIMeta is the UI component metadata of ui-block-start events (flex layout + component template)
	UIMeta any `json:"uiMeta,omitempty"`
	// UIData is the actual data of ui-block-start events (e.g. PromptOptionGroup's promptItems)
	UIData any `json:"uiData,omitempty"`
	// Interactive marks whether the ui-block-start card is interactive (user-clickable)
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

	// Streaming switch: when enabled the LLM uses ChatStream and emits text-delta per chunk in realtime (replacing terminal chunking)
	if r.streaming {
		sup.SetStreamCallback(r.buildTextDeltaCallback(onFrame))
	}

	agentCtx := react.NewAgentContext(query)
	// Create the supervisor.react span before writing agentCtx.Ctx: LLM calls (react.Supervisor reads c.Ctx)
	// then hang under this span instead of the parent span (worker.process / HTTP).
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
			Content: "agent execution error: " + err.Error(),
		})
		return "", err
	}

	// HIL interrupt: pass the checkpoint through for the caller to persist + emit the interrupt frame
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

	// Hot-topic card: emitted directly by the strategy (the sync path also produces it when wired to the manager strategy).
	if card := r.extractHotCard(result); card != nil {
		r.emitCardFrames(onFrame, serverMsgID, card)
		for _, fn := range r.frameSuffixes {
			fn()
		}
		onFrame(SSEFrame{Type: "message-complete", MsgID: serverMsgID})
		return result.Content, nil
	}

	// Terminal text: skip if streaming already sent content in realtime (avoid duplicates); otherwise it must be emitted —
	// with forced tool_choice=required the LLM may return only a tool_call (empty content),
	// leaving no frames to stream, so emit the strategy-wrapped terminal text (friendly_reply).
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

// emitTerminalText decides whether to emit the terminal text frame based on the streaming state.
//   - Non-streaming: normal 10-rune chunking (the original simulated stream)
//   - Streaming already sent content: skip (frames were emitted in realtime)
//   - Streaming but no content sent: emit the terminal text (guarantee the frontend has content)
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

// extractHotCard extracts hot-card data from the terminal AgentResult.Ext (passed through from the strategy).
// Type assertion first (same process), falling back to json.Unmarshal (robust across serialization). Returns nil when there is no card.
func (r *SupervisorRunner) extractHotCard(result *react.AgentResult) *HotCardData {
	if result == nil {
		return nil
	}
	if v, ok := result.Ext["hot_card"].(*HotCardData); ok && v != nil {
		return v
	}
	// Cross-serialization fallback: hot_card may be a JSON string or a map
	if raw, ok := result.Ext["hot_card"].(string); ok && raw != "" {
		var card HotCardData
		if err := json.Unmarshal([]byte(raw), &card); err == nil && len(card.TitleData) > 0 {
			return &card
		}
	}
	return nil
}

// emitCardFrames renders the hot-card frame sequence: hotspot text-delta → ui-block-start → ui-block-complete.
// Aligned with the reference capture's frame order (message-start → text-delta(hotspot markdown) → ui-block-start →
// ui-block-complete → message-complete).
func (r *SupervisorRunner) emitCardFrames(onFrame func(SSEFrame), serverMsgID string, card *HotCardData) {
	if card.HotspotData != "" {
		onFrame(SSEFrame{Type: "text-delta", TextID: "t1", Content: card.HotspotData})
	}
	for _, f := range BuildHotIssueFrames(serverMsgID, card) {
		onFrame(f)
	}
}

// RunResume is the same as Run, but pre-loads the AgentContext from the HIL checkpoint and resumes from the breakpoint.
// It pre-sets Messages/Step and marks Interrupted=true (the resume-round strategy terminates directly based on this).
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

	// Streaming switch: when enabled the LLM uses ChatStream and emits text-delta per chunk in realtime (replacing terminal chunking)
	if r.streaming {
		sup.SetStreamCallback(r.buildTextDeltaCallback(onFrame))
	}

	agentCtx := react.NewAgentContext(query)
	// Create the supervisor.react span before writing agentCtx.Ctx: LLM calls (react.Supervisor reads c.Ctx)
	// then hang under this span instead of the parent span (worker.process / HTTP).
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
		onFrame(SSEFrame{Type: "error", Content: "agent execution error: " + err.Error()})
		return "", err
	}

	// Terminal text: skip if streaming already sent it, otherwise emit it (guarantee the frontend has content)
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

// PlanOutput is the parsed result when the manager ReAct produces new_agents_plan.
type PlanOutput struct {
	// TaskType is the task type ("new_agents_plan").
	TaskType string `json:"task_type"`
	// FriendlyReply is the reply text shown to the user.
	FriendlyReply string `json:"friendly_reply"`
	// AgentsPipeline is the pipeline chosen by the manager (aligned with the reference system's PlanConfig.AgentsPipeline).
	// The LLM decides the chain; the pipeline bounds the valid agent set. Persisted via ToModelPlan for downstream recording/reuse.
	AgentsPipeline string `json:"agents_pipeline,omitempty"`
	// PlanSteps is the plan-steps JSON (containing the full Agents list).
	PlanSteps PlanStepsPayload `json:"plan_steps"`
}

// PlanStepsPayload is the structure of the plan produced by the manager (corresponds to model.PlanSteps).
type PlanStepsPayload struct {
	Agents []PlanAgentPayload `json:"agents"`
}

// PlanAgentPayload is one agent in the plan.
type PlanAgentPayload struct {
	AgentID string `json:"agent_id"`
	Input   string `json:"input"`
}

// OnOutput callback signature: triggered when the manager produces new_agents_plan.
type OnOutput func(plan *PlanOutput)

// OnInterrupt callback signature: triggered when ReAct produces an HIL breakpoint (ActionInterrupt), carrying the full checkpoint.
// The caller (Worker) SaveCheckpoints here to persist the recovery point, closing the interrupt → persist → resume loop.
type OnInterrupt func(cp *react.Checkpoint)

// SetOnInterrupt registers the HIL breakpoint callback (the caller SaveCheckpoints here to persist the recovery point).
func (r *SupervisorRunner) SetOnInterrupt(fn OnInterrupt) {
	r.onInterrupt = fn
}

// takeCheckpoint fetches the *react.Checkpoint stored by the engine from the AgentContext (nil when absent).
func (r *SupervisorRunner) takeCheckpoint(agentCtx *react.AgentContext) *react.Checkpoint {
	if v := agentCtx.Get(react.CheckpointKey); v != nil {
		if cp, ok := v.(*react.Checkpoint); ok {
			return cp
		}
	}
	return nil
}

// RunWithOutput is the same as Run, but notifies the caller via onOutput when the manager produces new_agents_plan.
// Used to wire the Phase 3 "manager → Submitter" chain.
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

	// Streaming switch: when enabled the LLM uses ChatStream and emits text-delta per chunk in realtime (replacing terminal chunking)
	if r.streaming {
		sup.SetStreamCallback(r.buildTextDeltaCallback(onFrame))
	}

	agentCtx := react.NewAgentContext(query)
	// Create the supervisor.react span before writing agentCtx.Ctx: LLM calls (react.Supervisor reads c.Ctx)
	// then hang under this span instead of the parent span (worker.process / HTTP).
	ctx, span := otel.Tracer(runnerTracerName).Start(ctx, "supervisor.react")
	defer span.End()
	agentCtx.Ctx = ctx
	span.SetAttributes(attribute.String("msg_id", serverMsgID))

	onFrame(SSEFrame{Type: "message-start", MsgID: serverMsgID})

	result, err := sup.Run(agentCtx)
	if err != nil {
		onFrame(SSEFrame{Type: "error", Content: "agent execution error: " + err.Error()})
		return "", err
	}

	// HIL interrupt: pass the checkpoint through for the caller to persist + emit the interrupt frame.
	// On interrupt, onOutput is not triggered and no text-delta is split out.
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

	// Hot-topic card: emitted directly by the strategy (idea_king_api calls the external hotspot service → Terminal + hot_card).
	// Rendered into the ui-block-start/ui-block-complete card frame sequence; does not enter plan/onOutput.
	if card := r.extractHotCard(result); card != nil {
		r.emitCardFrames(onFrame, serverMsgID, card)
		for _, fn := range r.frameSuffixes {
			fn()
		}
		onFrame(SSEFrame{Type: "message-complete", MsgID: serverMsgID})
		return result.Content, nil
	}

	// Parse the terminal JSON: if it is new_agents_plan, trigger the onOutput callback.
	// Prefer the plan_output passed through by the Strategy (complete JSON), falling back to parsing result.Content.
	rawPlan, _ := result.Ext["plan_output"].(string)
	if rawPlan == "" {
		rawPlan = result.Content
	}
	if plan := ParsePlanOutput(rawPlan); plan != nil {
		// Terminal structured output (new_agents_plan): the friendly_reply frame must be sent (streamed text-deltas
		// are the LLM's raw content; what is sent here is the terminal-wrapped friendly reply); onOutput dispatches the subtask.
		onFrame(SSEFrame{
			Type:    "text-delta",
			TextID:  "t1",
			Content: plan.FriendlyReply,
		})
		if onOutput != nil {
			onOutput(plan)
		}
	} else {
		// Plain reply: terminal text. Skip if streaming already sent content in realtime (avoid duplicates);
		// otherwise emit it — with forced tool_choice=required the LLM may return only a tool_call,
		// leaving no frames to stream, so the strategy-wrapped terminal text must be emitted to guarantee the frontend has content.
		r.emitTerminalText(onFrame, result.Content)
	}

	for _, fn := range r.frameSuffixes {
		fn()
	}
	onFrame(SSEFrame{Type: "message-complete", MsgID: serverMsgID})

	return result.Content, nil
}

// ParsePlanOutput parses the LLM terminal JSON; returns the plan when task_type is new_agents_plan, otherwise nil.
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

// ToModelPlan converts PlanOutput into model.PlanSteps (for the Submitter to persist).
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
