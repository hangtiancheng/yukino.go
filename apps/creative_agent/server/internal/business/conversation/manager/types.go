package manager

import (
	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
)

// ============ 类型别名：manager → provider（组件层）============
//
// manager 业务包复用 provider 组件层的类型定义，通过别名保持本包内引用简洁
// （与 provider 对 core 的别名模式一致）。依赖方向 manager → provider（单向，无环）。

// FunctionDef 是 provider.tools.FunctionDef 的别名（工具 Schema，本质 core.FunctionDef）。
type FunctionDef = appkittools.FunctionDef

// ParameterSpec 是 provider.tools.ParameterSpec 的别名。
type ParameterSpec = appkittools.ParameterSpec

// PropertyDef 是 provider.tools.PropertyDef 的别名。
type PropertyDef = appkittools.PropertyDef

// ToolRegistry 是 provider.tools.ToolRegistry 的别名（内建工具注册表）。
type ToolRegistry = appkittools.ToolRegistry

// ToolEntry 是 provider.tools.ToolEntry 的别名（单个工具注册项）。
type ToolEntry = appkittools.ToolEntry

// PlanOutput 是管家产出 new_agents_plan 的解析结果（定义在 provider/runner，
// 作为 Runner.RunWithOutput 的 OnOutput 回调参数类型，故保留在组件层）。
type PlanOutput = appkitrunner.PlanOutput

// PlanStepsPayload 是计划步骤 JSON 的结构。
type PlanStepsPayload = appkitrunner.PlanStepsPayload

// PlanAgentPayload 是计划中的一个 agent。
type PlanAgentPayload = appkitrunner.PlanAgentPayload

// HotCardData 是热点卡数据（定义在 provider/runner 的 ui_card.go）。
// strategy 把外部热点服务结果组织成它，经 AgentResult.Ext["hot_card"] 透传给 runner 渲染。
type HotCardData = appkitrunner.HotCardData

// PromptItem 是热点卡单个选项（定义在 provider/runner 的 ui_card.go）。
type PromptItem = appkitrunner.PromptItem

// ParseArgs 将 LLM 传来的 JSON arguments 解析为 map。
func ParseArgs(arguments string) map[string]any {
	return appkittools.ParseArgs(arguments)
}

// ArgString 从参数 map 中取字符串参数，缺省返回 fallback。
func ArgString(params map[string]any, key, fallback string) string {
	return appkittools.ArgString(params, key, fallback)
}

// ArgInt 从参数 map 中取整型参数，缺省返回 fallback。
// 支持 float64（JSON 数字）与 int 两种类型。
func ArgInt(params map[string]any, key string, fallback int) int {
	if params == nil {
		return fallback
	}
	switch v := params[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return fallback
}
