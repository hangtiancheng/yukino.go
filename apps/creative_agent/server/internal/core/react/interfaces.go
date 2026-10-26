package react

import "context"

// LLM 是大语言模型接口。实现方负责将消息发送给 LLM 并返回响应。
type LLM interface {
	// Chat 同步调用，一次性返回完整响应。
	Chat(ctx context.Context, messages []Message, tools []FunctionDef) (*LLMResponse, error)

	// ChatStream 流式调用：每个增量 chunk 经 callback 回调（可传 nil）。
	// 返回值与 Chat 相同（完整响应，供 ReAct 决策用）；流式只是旁路推送。
	// 不支持流式的实现应降级为：先同步调用 Chat，再触发一次 isEndingChunk=true 回调。
	ChatStream(ctx context.Context, messages []Message, tools []FunctionDef, callback StreamCallbackFunc) (*LLMResponse, error)
}

// Tools 负责按名称和参数执行工具调用。
type Tools interface {
	Execute(name, args string) (result string, handled bool, err error)
}

// Strategy 是纯决策接口：根据当前 agent 上下文判定下一步 Action。
// 不产生任何副作用。
type Strategy interface {
	GetTools(c *AgentContext) []FunctionDef
	Decide(c *AgentContext) *Decision
}

// Hooks 提供 ReAct 循环中的生命周期回调。
// 实现方可只覆写关注的方法，其余保持空实现。
type Hooks interface {
	OnAfterLLM(c *AgentContext)
	OnBeforeTerminal(c *AgentContext)
	OnBeforeInterrupt(c *AgentContext)
	OnBeforeRelay(c *AgentContext)
}
