package react

import "context"

// ToolCall 表示 LLM 响应中的一次工具调用。
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// Message 是对话历史中的一条消息。
type Message struct {
	Role       string     `json:"role"` // system / user / assistant / tool
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// LLMResponse 是单次 LLM 调用的返回结果。
type LLMResponse struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// FunctionDef 描述一个可供 LLM 调用的工具（完整 JSON Schema 版）。
// Name 全局唯一，LLM 靠此名发起调用；Description 决定 LLM 何时选用此工具。
type FunctionDef struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Parameters  ParameterSpec `json:"parameters,omitempty"`
}

// ParameterSpec 是 FunctionDef 的 JSON Schema 参数定义。
type ParameterSpec struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// PropertyDef 是单个参数定义。
type PropertyDef struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// Runtime 是一次 Run 调用中的可变状态。
type Runtime struct {
	Messages     []Message
	Step         int
	LastResponse *LLMResponse
	// Interrupted 标记本次 Run 是 resume 恢复（调用方在恢复前预置 messages/step）。
	// 该标记本身不参与引擎逻辑，仅供 strategy 区分首次 vs 恢复轮次。
	Interrupted bool
}

// AgentContext 承载一次 Agent 运行的全部状态。
type AgentContext struct {
	Query   string
	Runtime Runtime
	kv      map[string]any
	// Ctx 是一次 Run 的上下文，贯穿 ReAct 循环（LLM 调用/trace 传播用）。
	// 由调用方在 Run 前设置；nil 时引擎回退 context.Background()。
	Ctx context.Context
}

// NewAgentContext 创建带初始 query 的 AgentContext。
func NewAgentContext(query string) *AgentContext {
	return &AgentContext{Query: query, kv: make(map[string]any), Ctx: context.Background()}
}

// Set 在 agent 上下文中存入一个任意值。
func (c *AgentContext) Set(k string, v any) { c.kv[k] = v }

// Get 从 agent 上下文中取出一个值。
func (c *AgentContext) Get(k string) any { return c.kv[k] }

// Action 表示 Supervisor 在检查 LLM 输出后做出的五种决策之一。
type Action int

const (
	ActionContinue  Action = iota // 执行工具，结果作为 observation 塞回，继续循环
	ActionTerminal                // 正常终止，输出 friendly_reply
	ActionInterrupt               // HIL 断点：保存 checkpoint 后返回
	ActionRelay                   // 服务端自问自答：注入新 query，goto 重建 prompt
	ActionRetry                   // 输出格式不合规：注入纠错提示，不消耗步数重来
)

// String 返回 Action 的可读名称。
func (a Action) String() string {
	switch a {
	case ActionContinue:
		return "Continue"
	case ActionTerminal:
		return "Terminal"
	case ActionInterrupt:
		return "Interrupt"
	case ActionRelay:
		return "Relay"
	case ActionRetry:
		return "Retry"
	default:
		return "Unknown"
	}
}

// Decision 是 Strategy 对 LLM 输出的决策结果。
type Decision struct {
	Action   Action
	Metadata map[string]any
}

// AgentResult 是 Supervisor.Run 的返回结果。
type AgentResult struct {
	Content   string
	StepCount int
	// Interrupted 表示是否因 HIL 断点中断（供调用方持久化 checkpoint）。
	Interrupted bool
	// Ext 是终态时 Strategy 写入的附加结构化数据（如 new_agents_plan）。
	// 复刻 Phase 3 用：管家 Strategy 在 Metadata 放 plan_output，引擎透传到此处。
	Ext map[string]any
}

// CheckpointKey 是 AgentContext 中保存 HIL checkpoint 的键名。
const CheckpointKey = "checkpoint"

// LLMInputMessagesKey 是 AgentContext 中本轮 LLM 调用前输入消息快照的键名。
// OnAfterLLM 落 thought 表取 model_input 用；引擎在每轮 LLM 调用后、Decide 前写入。
const LLMInputMessagesKey = "llm_input_messages"

// LLMStepStartKey 是 AgentContext 中本轮 LLM 调用开始时间戳（毫秒）的键名。
const LLMStepStartKey = "llm_step_start_at"

// Checkpoint 是 HIL 中断时保存的恢复点。
type Checkpoint struct {
	Messages []Message // 中断时的对话历史
	Step     int       // 中断时的步数（resume 从该步继续）
	Query    string    // 原 query（恢复 prompt 用）
}
