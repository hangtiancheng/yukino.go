package manager

import (
	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
)

// ============ Type aliases: manager → provider (component layer) ============
//
// The manager business package reuses the type definitions of the provider component layer; aliases keep references concise
// (same alias pattern as provider → core). Dependency direction manager → provider (one-way, no cycle).

// FunctionDef is an alias of provider.tools.FunctionDef (the tool schema; essentially core.FunctionDef).
type FunctionDef = appkittools.FunctionDef

// ParameterSpec is an alias of provider.tools.ParameterSpec.
type ParameterSpec = appkittools.ParameterSpec

// PropertyDef is an alias of provider.tools.PropertyDef.
type PropertyDef = appkittools.PropertyDef

// ToolRegistry is an alias of provider.tools.ToolRegistry (the built-in tool registry).
type ToolRegistry = appkittools.ToolRegistry

// ToolEntry is an alias of provider.tools.ToolEntry (a single tool registration).
type ToolEntry = appkittools.ToolEntry

// PlanOutput is the parsed result of the manager's new_agents_plan (defined in provider/runner,
// kept in the component layer as the OnOutput callback parameter type of Runner.RunWithOutput).
type PlanOutput = appkitrunner.PlanOutput

// PlanStepsPayload is the structure of the plan-steps JSON.
type PlanStepsPayload = appkitrunner.PlanStepsPayload

// PlanAgentPayload is one agent in the plan.
type PlanAgentPayload = appkitrunner.PlanAgentPayload

// HotCardData is the hot-card data (defined in provider/runner's ui_card.go).
// The strategy organizes external hotspot-service results into it and passes it to the runner via AgentResult.Ext["hot_card"] for rendering.
type HotCardData = appkitrunner.HotCardData

// PromptItem is a single hot-card option (defined in provider/runner's ui_card.go).
type PromptItem = appkitrunner.PromptItem

// ParseArgs parses the JSON arguments from the LLM into a map.
func ParseArgs(arguments string) map[string]any {
	return appkittools.ParseArgs(arguments)
}

// ArgString fetches a string argument from the params map; returns fallback by default.
func ArgString(params map[string]any, key, fallback string) string {
	return appkittools.ArgString(params, key, fallback)
}

// ArgInt fetches an integer argument from the params map; returns fallback by default.
// Supports both float64 (JSON numbers) and int.
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
