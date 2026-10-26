package manager

import (
	"context"

	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
)

// NewManagerToolRegistry 构建管家（Supervisor）的工具注册表（调度类 + 协议执行）。
//
// 迁移自 provider/builtin_tools.go：这是管家业务构建函数，依赖 provider 的
// ToolRegistry 基类与通用工具（search/dispatch_agent），并注册管家专属的
// 协议工具（idea_king_api）。依赖方向 manager → provider（单向，无环）。
// p 为热点检索 provider（nil 时 RunIdeaKingWithProvider 内部用 MockProvider 兜底）。
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
