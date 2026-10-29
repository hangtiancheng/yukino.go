package react

import "context"

// LLM is the large-language-model interface. Implementations send the messages to the LLM and return the response.
type LLM interface {
	// Chat is a synchronous call returning the complete response at once.
	Chat(ctx context.Context, messages []Message, tools []FunctionDef) (*LLMResponse, error)

	// ChatStream is a streaming call: each delta chunk is delivered via callback (may be nil).
	// The return value is the same as Chat (the complete response, used for ReAct decisions); streaming is only a side-channel push.
	// Implementations without streaming support should degrade to: call Chat synchronously first, then trigger one isEndingChunk=true callback.
	ChatStream(ctx context.Context, messages []Message, tools []FunctionDef, callback StreamCallbackFunc) (*LLMResponse, error)
}

// Tools executes tool calls by name and arguments.
type Tools interface {
	Execute(name, args string) (result string, handled bool, err error)
}

// Strategy is a pure-decision interface: it decides the next Action from the current agent context.
// It produces no side effects.
type Strategy interface {
	GetTools(c *AgentContext) []FunctionDef
	Decide(c *AgentContext) *Decision
}

// Hooks provides lifecycle callbacks within the ReAct loop.
// Implementations may override only the methods they care about, leaving the rest as no-ops.
type Hooks interface {
	OnAfterLLM(c *AgentContext)
	OnBeforeTerminal(c *AgentContext)
	OnBeforeInterrupt(c *AgentContext)
	OnBeforeRelay(c *AgentContext)
}
