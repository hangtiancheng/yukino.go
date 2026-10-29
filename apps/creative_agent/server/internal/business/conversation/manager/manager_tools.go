package manager

import (
	"context"

	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
)

// NewManagerToolRegistry builds the manager (Supervisor) tool registry (scheduling class + protocol execution).
//
// Migrated from provider/builtin_tools.go: this is the manager business builder; it depends on the provider's
// ToolRegistry base and generic tools (search/dispatch_agent), and registers the manager-specific
// protocol tool (idea_king_api). Dependency direction manager → provider (one-way, no cycle).
// p is the hotspot retrieval provider (when nil, RunIdeaKingWithProvider falls back to MockProvider internally).
func NewManagerToolRegistry(p ideaking.IdeaKingProvider) *appkittools.ToolRegistry {
	r := appkittools.NewToolRegistry()
	r.Register(appkittools.ToolEntry{Def: appkittools.ToolSearch, Runner: appkittools.RunSearch})
	r.Register(appkittools.ToolEntry{Def: appkittools.ToolDispatchAgent, Runner: appkittools.RunDispatchAgent})
	r.Register(appkittools.ToolEntry{Def: ToolIdeaKingAPI, Runner: func(ctx context.Context, args string) (string, bool, error) {
		return RunIdeaKingWithProvider(ctx, p, args)
	}})
	for _, def := range []appkittools.FunctionDef{appkittools.ToolSearch, appkittools.ToolDispatchAgent, ToolIdeaKingAPI} {
		r.RegisterDefault(def)
	}
	return r
}
