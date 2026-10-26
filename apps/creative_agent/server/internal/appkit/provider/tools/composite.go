package appkittools

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// compositeProvider 依次尝试多个 ToolsProvider，返回第一个 handled=true 的结果。
// 用于合并内建工具（ToolRegistry）+ 动态 skill 工具（appkit/provider/skill 的 SkillProvider）。
// GetFunctionDefs 用 MergeFunctions 合并去重，前面的 provider 优先。
type compositeProvider struct {
	providers []ToolsProvider
}

// NewCompositeProvider 创建组合工具提供者。
func NewCompositeProvider(providers ...ToolsProvider) *compositeProvider {
	return &compositeProvider{providers: providers}
}

// GetFunctionDefs 合并所有 provider 的工具列表（前者优先）。
func (c *compositeProvider) GetFunctionDefs(ctx context.Context) ([]react.FunctionDef, error) {
	var merged []react.FunctionDef
	for i, p := range c.providers {
		defs, err := p.GetFunctionDefs(ctx)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			merged = defs
			continue
		}
		merged = MergeFunctions(merged, defs)
	}
	return merged, nil
}

// Execute 依次尝试各 provider，第一个 handled=true 的结果生效。
func (c *compositeProvider) Execute(ctx context.Context, name, arguments string) (string, bool, bool, error) {
	for _, p := range c.providers {
		res, handled, isPipeline, err := p.Execute(ctx, name, arguments)
		if handled {
			return res, true, isPipeline, err
		}
	}
	return "", false, false, fmt.Errorf("unknown tool: %s", name)
}
