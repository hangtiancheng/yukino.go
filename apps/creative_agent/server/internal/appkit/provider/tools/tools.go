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

// ============ §7.2 FunctionDef 类型（定义在 internal/core/types.go）============
// FunctionDef / ParameterSpec / PropertyDef 定义在 core 包，保证 ReAct 引擎与
// 工具系统共用同一份定义。此处通过类型别名保持 provider 包内引用简洁。

// FunctionDef 是 react.FunctionDef 的别名（工具 Schema）。
type FunctionDef = react.FunctionDef

// ParameterSpec 是 react.ParameterSpec 的别名。
type ParameterSpec = react.ParameterSpec

// PropertyDef 是 react.PropertyDef 的别名。
type PropertyDef = react.PropertyDef

// ToolsProvider 工具提供者接口（§7.4）。
// ToolRegistry 是一种实现（Go 代码 Register）；动态 skill 源（SkillProvider）见 appkit/provider/skill 包。
//
// 每次 LLM 调用前通过 GetFunctionDefs 取工具列表，支持动态增减；
// Execute 按名称执行工具，handled=false 表示该名称未注册，isPipeline=true 表示异步产线工具。
type ToolsProvider interface {
	GetFunctionDefs(ctx context.Context) ([]react.FunctionDef, error)
	Execute(ctx context.Context, name, arguments string) (
		result string, handled bool, isPipeline bool, err error,
	)
}

// ============ §7.3 ToolRegistry — 内建工具注册表 ============

// ToolRunner 单个工具的执行函数。
// 入参: ctx, arguments (LLM 传的 JSON 字符串)
// 返回: (结果字符串, 是否为异步产线工具, error)
type ToolRunner func(ctx context.Context, arguments string) (result string, isPipeline bool, err error)

// ToolEntry 注册表中的一条工具。
type ToolEntry struct {
	Def    FunctionDef
	Runner ToolRunner
}

// ToolRegistry 以硬编码 Register(entry) 方式注册工具的具体实现。
// 适用于内建工具（工具名编译期确定）；业务 API 用 appkit/provider/skill 的 SkillProvider（§7.7）。
//
// 支持按 agent 隔离工具集：RegisterDefault 注册全局默认工具（无 agentID 时返回），
// RegisterDefaultForAgent 注册某 agent 专属默认工具（GetFunctionDefs 从 ctx 读 agentID，
// 命中专属配置时只返回该 agent 的工具，否则回退全局默认）。对齐真实系统
// GetFunctions 按 agentID 查 DB 返回该 agent 可调用 function 列表的做法。
type ToolRegistry struct {
	mu       sync.RWMutex
	tools    map[string]*ToolEntry
	defaults []react.FunctionDef
	// agentDefaults 按 agentID 隔离的默认工具集（agentID → 该 agent 专属工具）。
	agentDefaults map[string][]react.FunctionDef
}

// NewToolRegistry 创建空注册表。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:         map[string]*ToolEntry{},
		agentDefaults: map[string][]react.FunctionDef{},
	}
}

// Register 注册单个工具。
func (r *ToolRegistry) Register(entry ToolEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[entry.Def.Name] = &entry
}

// RegisterDefault 标记一个工具为默认工具（GetFunctionDefs 时返回给 LLM）。
// 无 agentID 上下文时使用（全局默认，所有 agent 可见）。
func (r *ToolRegistry) RegisterDefault(def react.FunctionDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaults = append(r.defaults, def)
}

// RegisterDefaultForAgent 注册某 agent 专属的默认工具。
// 该 agent 执行时 GetFunctionDefs 只返回这里登记的工具（不再混入全局默认）。
// agentID 为空等价 RegisterDefault。
func (r *ToolRegistry) RegisterDefaultForAgent(agentID string, def react.FunctionDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if agentID == "" {
		r.defaults = append(r.defaults, def)
		return
	}
	r.agentDefaults[agentID] = append(r.agentDefaults[agentID], def)
}

// GetFunctionDefs 返回工具列表（供 LLM）。
// 从 ctx 读 agentID：命中该 agent 的专属工具集则只返回专属工具（对齐真实系统
// 按 agent 隔离工具）；否则返回全局默认工具。
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

// Execute 按名称执行工具。handled=false 表示工具名未注册。
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

// legacyExecute 是 react.Tools 接口适配层（Phase 1 引擎无 ctx 版）。
// 丢弃 isPipeline，用 context.Background() 兜底。
func (r *ToolRegistry) legacyExecute(name, args string) (string, bool, error) {
	out, handled, _, err := r.Execute(context.Background(), name, args)
	return out, handled, err
}

// RegisterCoreAdapter 让 *ToolRegistry 同时实现 react.Tools（Phase 1 引擎用）。
// 返回包装类型，避免破坏 ToolsProvider 接口签名。
func (r *ToolRegistry) AsCoreTools() react.Tools {
	return coreToolsAdapter{r}
}

// coreToolsAdapter 把 *ToolRegistry 适配为 react.Tools（引擎旧接口）。
type coreToolsAdapter struct {
	reg *ToolRegistry
}

// Execute 实现 react.Tools.Execute（丢弃 ctx 与 isPipeline）。
func (a coreToolsAdapter) Execute(name, args string) (string, bool, error) {
	return a.reg.legacyExecute(name, args)
}

// ============ §7.8 单次工具调用的完整流程 ============

// ToolCallResult 单次工具调用的完整结果。
type ToolCallResult struct {
	ToolCallID string // LLM 分配的调用 ID
	Name       string
	Arguments  string
	Result     string // observation 文本
	IsError    bool   // true=执行失败，把错误字符串当 observation 塞回
	IsPipeline bool   // true=异步产线工具，需要 NeedWait 分流
}

// ToolCallHooks 工具调用前后的生命周期钩子。
// 管家场景：建/更新 thought 记录；子 Agent 场景：建/更新 cg_task_step。
type ToolCallHooks interface {
	OnBefore(ctx context.Context, toolCallID, name, arguments string)
	OnAfter(ctx context.Context, result *ToolCallResult)
}

// ExecuteToolCall ReAct 循环中处理单次工具调用的完整流程（§7.8）。
// 由引擎的 Continue 分支调用。返回的 observation 直接 append 到 messages。
//
// 流程：调用前钩子 → 执行（接口解耦）→ 失败当 observation → 调用后钩子。
// 精髓：工具失败不中断循环，错误字符串当 observation 塞回让 LLM 自己决策。
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

	// 调用前钩子
	if hooks != nil {
		hooks.OnBefore(ctx, toolCallID, name, arguments)
	}

	// 执行（通过接口解耦：超时 → 执行 → 失败当 observation）
	output, _, isPipeline, err := provider.Execute(ctx, name, arguments)
	if err != nil {
		result.Result = fmt.Sprintf("工具执行失败: %v", err)
		result.IsError = true
	} else {
		result.Result = output
		result.IsPipeline = isPipeline
	}

	// 调用后钩子
	if hooks != nil {
		hooks.OnAfter(ctx, result)
	}

	return result
}

// ============ §7.3 MergeFunctions — 两种工具源合并 ============

// MergeFunctions 合并两个 FunctionDef 列表，同名时 strategy 优先。
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

// ============ ctx 身份透传 key ============

// ctxIdentityKey 是 ctx 身份 key 类型（避免 string 直接作 key 冲突）。
type ctxIdentityKey int

const (
	// CtxUserID 是注入到工具执行 ctx 的用户 ID（来自 executor 的 run.UserID）。
	CtxUserID ctxIdentityKey = iota
	// CtxSessionID 是注入到工具执行 ctx 的会话 ID（来自 executor 的 run.SessionID）。
	CtxSessionID
)

// WithIdentity 把 userID/sessionID 注入 ctx，供工具 Runner 读取（跨服务透传身份）。
func WithIdentity(ctx context.Context, userID int64, sessionID string) context.Context {
	ctx = context.WithValue(ctx, CtxUserID, userID)
	ctx = context.WithValue(ctx, CtxSessionID, sessionID)
	return ctx
}

// IdentityFromContext 从 ctx 读身份，缺省返回 (0, "")。
func IdentityFromContext(ctx context.Context) (int64, string) {
	uid, _ := ctx.Value(CtxUserID).(int64)
	sid, _ := ctx.Value(CtxSessionID).(string)
	return uid, sid
}

// ============ ctx 按 agent 取工具 key ============

// ctxAgentKey 是 ctx 中 agentID 的 key 类型。
type ctxAgentKey int

const (
	// CtxAgentID 是注入到 GetFunctionDefs ctx 的 agentID（来自 executor 的 run.Agents[0].AgentID）。
	// ToolRegistry 据此返回该 agent 专属工具集（对齐真实系统按 agent 隔离工具）。
	CtxAgentID ctxAgentKey = iota
)

// WithAgentID 把 agentID 注入 ctx，供 ToolRegistry.GetFunctionDefs 按 agent 过滤工具。
func WithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, CtxAgentID, agentID)
}

// AgentIDFromContext 从 ctx 读 agentID，缺省返回 ""。
func AgentIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(CtxAgentID).(string)
	return id
}

// ============ 参数解析辅助 ============

// ParseArgs 将 LLM 传来的 JSON arguments 解析为 map。
// 解析失败时返回空 map（不中断执行）。
func ParseArgs(arguments string) map[string]any {
	params := map[string]any{}
	if strings.TrimSpace(arguments) == "" {
		return params
	}
	_ = json.Unmarshal([]byte(arguments), &params)
	return params
}

// ArgString 从参数 map 中取字符串参数，缺省返回 fallback。
func ArgString(params map[string]any, key, fallback string) string {
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// ============ FakeTools（Phase 1 测试用，保留兼容）============

// FakeTools 是 Phase 1 测试用的 map 查表工具执行器。
type FakeTools struct{}

func (FakeTools) Execute(name, args string) (string, bool, error) {
	switch name {
	case "search":
		return `{"results":["这是模拟的搜索结果。"]}`, true, nil
	default:
		return "", false, fmt.Errorf("未知工具: %s", name)
	}
}
