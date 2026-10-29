package appkittools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// ============ §7.2 FunctionDef types (defined in internal/core/types.go) ============
// FunctionDef / ParameterSpec / PropertyDef are defined in the core package so the ReAct engine and
// the tool system share the same definitions. Type aliases keep references inside the provider package concise.

// FunctionDef is an alias of react.FunctionDef (the tool schema).
type FunctionDef = react.FunctionDef

// ParameterSpec is an alias of react.ParameterSpec.
type ParameterSpec = react.ParameterSpec

// PropertyDef is an alias of react.PropertyDef.
type PropertyDef = react.PropertyDef

// ToolsProvider is the tool-provider interface (§7.4).
// ToolRegistry is one implementation (Register in Go code); for dynamic skill sources (SkillProvider) see the appkit/provider/skill package.
//
// The tool list is fetched via GetFunctionDefs before every LLM call, supporting dynamic add/remove;
// Execute runs a tool by name; handled=false means the name is unregistered, isPipeline=true marks an async pipeline tool.
type ToolsProvider interface {
	GetFunctionDefs(ctx context.Context) ([]react.FunctionDef, error)
	Execute(ctx context.Context, name, arguments string) (
		result string, handled bool, isPipeline bool, err error,
	)
}

// ============ §7.3 ToolRegistry — built-in tool registry ============

// ToolRunner is the execution function of a single tool.
// Inputs: ctx, arguments (the JSON string passed by the LLM)
// Returns: (result string, whether it is an async pipeline tool, error)
type ToolRunner func(ctx context.Context, arguments string) (result string, isPipeline bool, err error)

// ToolEntry is one tool in the registry.
type ToolEntry struct {
	Def    FunctionDef
	Runner ToolRunner
}

// ToolRegistry is the concrete implementation that registers tools via hard-coded Register(entry).
// Suitable for built-in tools (tool names fixed at compile time); business APIs use the SkillProvider from appkit/provider/skill (§7.7).
//
// Supports per-agent tool-set isolation: RegisterDefault registers global default tools (returned when there is no agentID),
// RegisterDefaultForAgent registers default tools exclusive to one agent (GetFunctionDefs reads agentID from ctx;
// when the exclusive config matches, only that agent's tools are returned, otherwise it falls back to the global defaults). Aligned with the reference system's
// GetFunctions, which queries the DB by agentID and returns the callable function list of that agent.
type ToolRegistry struct {
	mu       sync.RWMutex
	tools    map[string]*ToolEntry
	defaults []react.FunctionDef
	// agentDefaults is the per-agentID isolated default tool set (agentID → tools exclusive to that agent).
	agentDefaults map[string][]react.FunctionDef
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:         map[string]*ToolEntry{},
		agentDefaults: map[string][]react.FunctionDef{},
	}
}

// Register registers a single tool.
func (r *ToolRegistry) Register(entry ToolEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[entry.Def.Name] = &entry
}

// RegisterDefault marks a tool as a default tool (returned to the LLM by GetFunctionDefs).
// Used when there is no agentID context (global default, visible to all agents).
func (r *ToolRegistry) RegisterDefault(def react.FunctionDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaults = append(r.defaults, def)
}

// RegisterDefaultForAgent registers default tools exclusive to one agent.
// When that agent runs, GetFunctionDefs returns only the tools registered here (global defaults are not mixed in).
// An empty agentID is equivalent to RegisterDefault.
func (r *ToolRegistry) RegisterDefaultForAgent(agentID string, def react.FunctionDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if agentID == "" {
		r.defaults = append(r.defaults, def)
		return
	}
	r.agentDefaults[agentID] = append(r.agentDefaults[agentID], def)
}

// GetFunctionDefs returns the tool list (for the LLM).
// It reads agentID from ctx: when the agent's exclusive tool set matches, only those tools are returned (aligned with the
// reference system's per-agent tool isolation); otherwise the global default tools are returned.
func (r *ToolRegistry) GetFunctionDefs(ctx context.Context) ([]react.FunctionDef, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if agentID := AgentIDFromContext(ctx); agentID != "" {
		if agentTools, ok := r.agentDefaults[agentID]; ok {
			result := make([]react.FunctionDef, len(agentTools))
			copy(result, agentTools)
			return result, nil
		}
	}
	result := make([]react.FunctionDef, len(r.defaults))
	copy(result, r.defaults)
	return result, nil
}

// Execute runs a tool by name. handled=false means the tool name is unregistered.
func (r *ToolRegistry) Execute(ctx context.Context, name, arguments string) (
	result string, handled bool, isPipeline bool, err error) {
	r.mu.RLock()
	entry, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", false, false, fmt.Errorf("unknown tool: %s", name)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, isPipeline, err = entry.Runner(ctx, arguments)
	return result, true, isPipeline, err
}

// legacyExecute is the adapter layer for the react.Tools interface (the Phase 1 engine has no ctx version).
// It drops isPipeline and falls back to context.Background().
func (r *ToolRegistry) legacyExecute(name, args string) (string, bool, error) {
	out, handled, _, err := r.Execute(context.Background(), name, args)
	return out, handled, err
}

// RegisterCoreAdapter makes *ToolRegistry also implement react.Tools (for the Phase 1 engine).
// It returns a wrapper type to avoid breaking the ToolsProvider interface signature.
func (r *ToolRegistry) AsCoreTools() react.Tools {
	return coreToolsAdapter{r}
}

// coreToolsAdapter adapts *ToolRegistry to react.Tools (the engine's legacy interface).
type coreToolsAdapter struct {
	reg *ToolRegistry
}

// Execute implements react.Tools.Execute (dropping ctx and isPipeline).
func (a coreToolsAdapter) Execute(name, args string) (string, bool, error) {
	return a.reg.legacyExecute(name, args)
}

// ============ §7.8 Full flow of a single tool call ============

// ToolCallResult is the complete result of a single tool call.
type ToolCallResult struct {
	ToolCallID string // call ID assigned by the LLM
	Name       string
	Arguments  string
	Result     string // observation text
	IsError    bool   // true = execution failed; the error string is fed back as an observation
	IsPipeline bool   // true = async pipeline tool; requires NeedWait branching
}

// ToolCallHooks are lifecycle hooks around a tool call.
// Manager scenario: create/update thought records; sub-agent scenario: create/update cg_task_step.
type ToolCallHooks interface {
	OnBefore(ctx context.Context, toolCallID, name, arguments string)
	OnAfter(ctx context.Context, result *ToolCallResult)
}

// ExecuteToolCall handles the full flow of a single tool call inside the ReAct loop (§7.8).
// Called by the engine's Continue branch. The returned observation is appended directly to messages.
//
// Flow: before-hook → execute (decoupled via interface) → failure becomes an observation → after-hook.
// The essence: tool failures do not break the loop; the error string is fed back as an observation so the LLM decides what to do.
func ExecuteToolCall(
	ctx context.Context,
	provider ToolsProvider,
	toolCallID, name, arguments string,
	hooks ToolCallHooks,
) *ToolCallResult {
	result := &ToolCallResult{
		ToolCallID: toolCallID,
		Name:       name,
		Arguments:  arguments,
	}

	// Before-hook
	if hooks != nil {
		hooks.OnBefore(ctx, toolCallID, name, arguments)
	}

	// Execute (decoupled via interface: timeout → execute → failure becomes an observation)
	output, _, isPipeline, err := provider.Execute(ctx, name, arguments)
	if err != nil {
		result.Result = fmt.Sprintf("工具执行失败: %v", err)
		result.IsError = true
	} else {
		result.Result = output
		result.IsPipeline = isPipeline
	}

	// After-hook
	if hooks != nil {
		hooks.OnAfter(ctx, result)
	}

	return result
}

// ============ §7.3 MergeFunctions — merging two tool sources ============

// MergeFunctions merges two FunctionDef lists; on name collisions, strategy wins.
func MergeFunctions(strategyTools, providerTools []react.FunctionDef) []react.FunctionDef {
	seen := map[string]bool{}
	result := make([]react.FunctionDef, 0, len(strategyTools)+len(providerTools))
	for _, t := range strategyTools {
		seen[t.Name] = true
		result = append(result, t)
	}
	for _, t := range providerTools {
		if !seen[t.Name] {
			result = append(result, t)
		}
	}
	return result
}

// ============ ctx identity propagation keys ============

// ctxIdentityKey is the ctx identity key type (avoiding collisions from using plain strings as keys).
type ctxIdentityKey int

const (
	// CtxUserID is the user ID injected into the tool-execution ctx (from the executor's run.UserID).
	CtxUserID ctxIdentityKey = iota
	// CtxSessionID is the session ID injected into the tool-execution ctx (from the executor's run.SessionID).
	CtxSessionID
)

// WithIdentity injects userID/sessionID into ctx for tool Runners to read (cross-service identity propagation).
func WithIdentity(ctx context.Context, userID int64, sessionID string) context.Context {
	ctx = context.WithValue(ctx, CtxUserID, userID)
	ctx = context.WithValue(ctx, CtxSessionID, sessionID)
	return ctx
}

// IdentityFromContext reads the identity from ctx; returns (0, "") by default.
func IdentityFromContext(ctx context.Context) (int64, string) {
	uid, _ := ctx.Value(CtxUserID).(int64)
	sid, _ := ctx.Value(CtxSessionID).(string)
	return uid, sid
}

// ============ ctx key for per-agent tool lookup ============

// ctxAgentKey is the key type of the agentID in ctx.
type ctxAgentKey int

const (
	// CtxAgentID is the agentID injected into the GetFunctionDefs ctx (from the executor's run.Agents[0].AgentID).
	// ToolRegistry uses it to return the agent's exclusive tool set (aligned with the reference system's per-agent tool isolation).
	CtxAgentID ctxAgentKey = iota
)

// WithAgentID injects agentID into ctx so ToolRegistry.GetFunctionDefs can filter tools per agent.
func WithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, CtxAgentID, agentID)
}

// AgentIDFromContext reads agentID from ctx; returns "" by default.
func AgentIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(CtxAgentID).(string)
	return id
}

// ============ Argument parsing helpers ============

// ParseArgs parses the JSON arguments from the LLM into a map.
// It returns an empty map on parse failure (execution is not interrupted).
func ParseArgs(arguments string) map[string]any {
	params := map[string]any{}
	if strings.TrimSpace(arguments) == "" {
		return params
	}
	_ = json.Unmarshal([]byte(arguments), &params)
	return params
}

// ArgString fetches a string argument from the params map; returns fallback by default.
func ArgString(params map[string]any, key, fallback string) string {
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// ============ FakeTools (for Phase 1 tests, kept for compatibility) ============

// FakeTools is the map-lookup tool executor for Phase 1 tests.
type FakeTools struct{}

func (FakeTools) Execute(name, args string) (string, bool, error) {
	switch name {
	case "search":
		return `{"results":["这是模拟的搜索结果。"]}`, true, nil
	default:
		return "", false, fmt.Errorf("未知工具: %s", name)
	}
}
