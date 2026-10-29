package appkittools

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// compositeProvider tries multiple ToolsProviders in order and returns the first result with handled=true.
// Used to merge built-in tools (ToolRegistry) + dynamic skill tools (the SkillProvider from appkit/provider/skill).
// GetFunctionDefs merges and dedupes with MergeFunctions; earlier providers win.
type compositeProvider struct {
	providers []ToolsProvider
}

// NewCompositeProvider creates the composite tool provider.
func NewCompositeProvider(providers ...ToolsProvider) *compositeProvider {
	return &compositeProvider{providers: providers}
}

// GetFunctionDefs merges the tool lists of all providers (earlier ones win).
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

// Execute tries each provider in order; the first result with handled=true takes effect.
func (c *compositeProvider) Execute(ctx context.Context, name, arguments string) (string, bool, bool, error) {
	for _, p := range c.providers {
		res, handled, isPipeline, err := p.Execute(ctx, name, arguments)
		if handled {
			return res, true, isPipeline, err
		}
	}
	return "", false, false, fmt.Errorf("unknown tool: %s", name)
}
