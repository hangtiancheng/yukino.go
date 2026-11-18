package tools

import (
	"context"

	sdktools "github.com/hangtiancheng/yukino-code/apps/yukino/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func NewRegistry(ctx context.Context, cfg *config.Config) (*sdktools.Registry, error) {
	list, err := GetLogMcpTool(ctx, cfg.MCP)
	if err != nil {
		return nil, err
	}
	registry := sdktools.NewRegistry()
	for _, t := range list {
		registry.RegisterBorrowed(t)
	}
	constructors := []func() (sdktools.Tool, error){NewGetCurrentTimeTool, NewMysqlCrudTool, func() (sdktools.Tool, error) { return NewPrometheusAlertsQueryTool(cfg.PrometheusURL) }, func() (sdktools.Tool, error) { return NewQueryInternalDocsTool(cfg) }}
	for _, newTool := range constructors {
		t, err := newTool()
		if err != nil {
			registry.Dispose()
			return nil, err
		}
		registry.Register(t)
	}
	return registry, nil
}
