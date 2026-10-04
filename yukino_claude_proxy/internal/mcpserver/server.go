// Package mcpserver exposes the same persistent service lifecycle as the CLI.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/daemon"
)

type StartInput struct {
	Protocol *string `json:"protocol,omitempty" jsonschema:"Optional protocol filter: anthropic, openai (Responses), or openai-compat (Chat Completions)"`
	Name     *string `json:"name,omitempty" jsonschema:"Optional name filter; names may repeat. Omit both filters to use default_provider (default index 0)"`
}

func New(options daemon.Options) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "yukino-claude-proxy", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "start_proxy", Description: "Start or switch using the first provider matching the supplied protocol and/or name. With neither filter, select default_provider (default index 0) in ~/.yukino/config.yaml. Check the provider connection before backing up and updating Claude Code settings. Backups are retained for manual restoration."}, func(ctx context.Context, _ *mcp.CallToolRequest, input StartInput) (*mcp.CallToolResult, daemon.Status, error) {
		opts := options
		opts.Protocol, opts.Name = "", ""
		if input.Protocol != nil {
			if *input.Protocol == "" {
				return nil, daemon.Status{}, fmt.Errorf("protocol must not be empty")
			}
			opts.Protocol = *input.Protocol
		}
		if input.Name != nil {
			if *input.Name == "" {
				return nil, daemon.Status{}, fmt.Errorf("name must not be empty")
			}
			opts.Name = *input.Name
		}
		manager := daemon.Manager{Options: opts}
		status, err := manager.Start(ctx)
		return nil, status, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "shutdown_proxy", Description: "Stop the background proxy. Leave Claude Code settings and backups untouched; restoration is manual."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		manager := daemon.Manager{Options: options}
		err := manager.Shutdown(ctx)
		return nil, map[string]any{"running": false, "settings_restored": false}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "proxy_status", Description: "Report proxy status, selected provider, gateway URL and backup path without exposing credentials."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, daemon.Status, error) {
		manager := daemon.Manager{Options: options}
		status, err := manager.Status(ctx)
		return nil, status, err
	})
	return server
}
