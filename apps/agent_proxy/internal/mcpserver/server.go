package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/daemon"
)

type StartInput struct {
	Agent    *string `json:"agent,omitempty" jsonschema:"Optional coding agent: claude or codex. Omit to use the MCP server's --agent (default claude)"`
	Protocol *string `json:"protocol,omitempty" jsonschema:"Optional protocol filter: anthropic, openai (Responses), or openai-compat (Chat Completions)"`
	Name     *string `json:"name,omitempty" jsonschema:"Optional name filter; names may repeat. Omit both filters to use default_provider (default index 0)"`
}

type AgentInput struct {
	Agent *string `json:"agent,omitempty" jsonschema:"Optional coding agent: claude or codex. Omit to use the MCP server's --agent (default claude)"`
}

func agentOptions(options daemon.Options, input *string) (daemon.Options, error) {
	name := options.Agent
	if input != nil {
		if *input == "" {
			return daemon.Options{}, fmt.Errorf("agent must not be empty")
		}
		name = *input
	}
	selected, err := agent.Get(name)
	if err != nil {
		return daemon.Options{}, err
	}
	if name == options.Agent {
		return options, nil
	}
	current, err := agent.Get(options.Agent)
	if err != nil {
		return daemon.Options{}, err
	}
	previous, err := daemon.Defaults(current)
	if err != nil {
		return daemon.Options{}, err
	}
	defaults, err := daemon.Defaults(selected)
	if err != nil {
		return daemon.Options{}, err
	}
	options.Agent = name
	if options.AgentDir == "" || options.AgentDir == previous.AgentDir {
		options.AgentDir = defaults.AgentDir
	}
	if options.StateDir == "" || options.StateDir == previous.StateDir {
		options.StateDir = defaults.StateDir
	}
	if options.Listen == "" || options.Listen == previous.Listen {
		options.Listen = defaults.Listen
	}
	return options, nil
}

func New(options daemon.Options) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "yukino-agent-proxy", Version: "0.1.0"}, nil)
	agentDescription := fmt.Sprintf("Select claude or codex with the optional agent argument; omit it to use %s. ", options.Agent)
	startDescription := agentDescription + "Start or switch using the first provider matching the supplied protocol and/or name. With neither filter, select default_provider (default index 0) in the configured provider file. Check the provider connection before backing up and updating the selected agent's settings. Backups are retained for manual restoration."
	shutdownDescription := agentDescription + "Stop the selected agent's background proxy. Leave settings and backups untouched; restoration is manual."
	mcp.AddTool(server, &mcp.Tool{Name: "start_proxy", Description: startDescription}, func(ctx context.Context, _ *mcp.CallToolRequest, input StartInput) (*mcp.CallToolResult, daemon.Status, error) {
		opts, err := agentOptions(options, input.Agent)
		if err != nil {
			return nil, daemon.Status{}, err
		}
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
	mcp.AddTool(server, &mcp.Tool{Name: "shutdown_proxy", Description: shutdownDescription}, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentInput) (*mcp.CallToolResult, map[string]any, error) {
		opts, err := agentOptions(options, input.Agent)
		if err != nil {
			return nil, nil, err
		}
		manager := daemon.Manager{Options: opts}
		err = manager.Shutdown(ctx)
		return nil, map[string]any{"agent": opts.Agent, "running": false, "settings_restored": false}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "proxy_status", Description: agentDescription + "Report proxy status, selected provider, gateway URL and backup path without exposing credentials."}, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentInput) (*mcp.CallToolResult, daemon.Status, error) {
		opts, err := agentOptions(options, input.Agent)
		if err != nil {
			return nil, daemon.Status{}, err
		}
		manager := daemon.Manager{Options: opts}
		status, err := manager.Status(ctx)
		if status.Agent == "" {
			status.Agent = opts.Agent
		}
		return nil, status, err
	})
	return server
}
