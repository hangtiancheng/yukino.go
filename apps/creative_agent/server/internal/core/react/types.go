package react

import "context"

// ToolCall represents one tool call in an LLM response.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// Message is one entry in the conversation history.
type Message struct {
	Role       string     `json:"role"` // system / user / assistant / tool
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// LLMResponse is the result of a single LLM call.
type LLMResponse struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// FunctionDef describes a tool the LLM can call (full JSON Schema version).
// Name is globally unique; the LLM invokes by this name. Description determines when the LLM picks this tool.
type FunctionDef struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Parameters  ParameterSpec `json:"parameters,omitempty"`
}

// ParameterSpec is the JSON Schema parameter definition of a FunctionDef.
type ParameterSpec struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// PropertyDef is a single parameter definition.
type PropertyDef struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// Runtime is the mutable state within one Run invocation.
type Runtime struct {
	Messages     []Message
	Step         int
	LastResponse *LLMResponse
	// Interrupted marks this Run as a resume (the caller pre-sets messages/step before resuming).
	// The flag itself does not participate in engine logic; it only lets the strategy distinguish first runs from resumed rounds.
	Interrupted bool
}

// AgentContext carries the entire state of one Agent run.
type AgentContext struct {
	Query   string
	Runtime Runtime
	kv      map[string]any
	// Ctx is the context of one Run, threaded through the ReAct loop (for LLM calls / trace propagation).
	// Set by the caller before Run; the engine falls back to context.Background() when nil.
	Ctx context.Context
}

// NewAgentContext creates an AgentContext with the initial query.
func NewAgentContext(query string) *AgentContext {
	return &AgentContext{Query: query, kv: make(map[string]any), Ctx: context.Background()}
}

// Set stores an arbitrary value in the agent context.
func (c *AgentContext) Set(k string, v any) { c.kv[k] = v }

// Get retrieves a value from the agent context.
func (c *AgentContext) Get(k string) any { return c.kv[k] }

// Action is one of the five decisions the Supervisor makes after inspecting the LLM output.
type Action int

const (
	ActionContinue  Action = iota // execute the tool, feed the result back as an observation, keep looping
	ActionTerminal                // terminate normally, output friendly_reply
	ActionInterrupt               // HIL breakpoint: save the checkpoint and return
	ActionRelay                   // server-side self-Q&A: inject a new query, goto to rebuild the prompt
	ActionRetry                   // non-conforming output: inject a correction hint, redo without consuming a step
)

// String returns the human-readable name of the Action.
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

// Decision is the Strategy's verdict on the LLM output.
type Decision struct {
	Action   Action
	Metadata map[string]any
}

// AgentResult is the return value of Supervisor.Run.
type AgentResult struct {
	Content   string
	StepCount int
	// Interrupted indicates whether the run was interrupted by an HIL breakpoint (for the caller to persist the checkpoint).
	Interrupted bool
	// Ext is the extra structured data written by the Strategy at the terminal state (e.g. new_agents_plan).
	// Used by the Phase 3 replica: the manager Strategy puts plan_output into Metadata and the engine passes it through here.
	Ext map[string]any
}

// CheckpointKey is the key under which the HIL checkpoint is stored in the AgentContext.
const CheckpointKey = "checkpoint"

// LLMInputMessagesKey is the key of the input-message snapshot taken before this round's LLM call in the AgentContext.
// Used by OnAfterLLM to persist model_input into the thought table; the engine writes it after each LLM call, before Decide.
const LLMInputMessagesKey = "llm_input_messages"

// LLMStepStartKey is the key of this round's LLM call start timestamp (milliseconds) in the AgentContext.
const LLMStepStartKey = "llm_step_start_at"

// Checkpoint is the recovery point saved on an HIL interrupt.
type Checkpoint struct {
	Messages []Message // conversation history at the interrupt
	Step     int       // step count at the interrupt (resume continues from this step)
	Query    string    // original query (used to rebuild the prompt on resume)
}
