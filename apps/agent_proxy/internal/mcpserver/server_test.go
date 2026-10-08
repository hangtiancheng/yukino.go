package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/daemon"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPTools(t *testing.T) {
	for _, defaultAgent := range agent.Names() {
		t.Run(defaultAgent, func(t *testing.T) { testMCPTools(t, defaultAgent) })
	}
}

func testMCPTools(t *testing.T, defaultAgent string) {
	t.Helper()
	root := t.TempDir()
	server := New(daemon.Options{Agent: defaultAgent, Protocol: "anthropic", StateDir: filepath.Join(root, "state"), ConfigPath: filepath.Join(root, "missing.yaml")})
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	ct, st := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 3 {
		t.Fatal("expected start, shutdown, and status tools")
	}
	for _, tool := range listed.Tools {
		data, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(data, &schema); err != nil || schema.Properties["agent"] == nil {
			t.Fatalf("%s does not advertise the agent option: %s", tool.Name, data)
		}
		for _, field := range schema.Required {
			if field == "agent" {
				t.Fatalf("%s requires agent", tool.Name)
			}
		}
		for _, invalid := range []string{"", "gemini"} {
			result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: map[string]any{"agent": invalid}})
			if err != nil || !result.IsError {
				t.Fatalf("%s accepted invalid agent %q: %v", tool.Name, invalid, err)
			}
		}
	}
	for _, name := range []string{"proxy_status", "shutdown_proxy"} {
		for _, selected := range []string{agent.Claude, agent.Codex, ""} {
			args := map[string]any{}
			wantAgent := selected
			if selected != "" {
				args["agent"] = selected
			} else {
				wantAgent = defaultAgent
			}
			result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil || result.IsError {
				t.Fatalf("%s failed: %v", name, err)
			}
			data, _ := json.Marshal(result.StructuredContent)
			var decoded map[string]any
			if json.Unmarshal(data, &decoded) != nil || decoded["running"] != false || decoded["agent"] != wantAgent {
				t.Fatalf("bad structured result: %s", data)
			}
		}
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "start_proxy", Arguments: map[string]any{"protocol": "openai-compat", "name": "missing"}})
	if err != nil || !result.IsError {
		t.Fatal("start errors should be reported as MCP tool errors")
	}
}

func TestAgentOptions(t *testing.T) {
	for _, from := range agent.Names() {
		current, _ := agent.Get(from)
		options, err := daemon.Defaults(current)
		if err != nil {
			t.Fatal(err)
		}
		options.ConfigPath = filepath.Join(t.TempDir(), "config.yaml")
		options.Executable = "test-proxy"
		for _, to := range agent.Names() {
			t.Run(from+"/"+to, func(t *testing.T) {
				selected, err := agentOptions(options, &to)
				if err != nil {
					t.Fatal(err)
				}
				a, _ := agent.Get(to)
				defaults, err := daemon.Defaults(a)
				if err != nil {
					t.Fatal(err)
				}
				if selected.Agent != to || selected.AgentDir != defaults.AgentDir || selected.StateDir != defaults.StateDir || selected.Listen != defaults.Listen {
					t.Fatalf("wrong defaults for %s: %+v", to, selected)
				}
				if selected.ConfigPath != options.ConfigPath || selected.Executable != options.Executable {
					t.Fatal("shared runtime configuration changed")
				}
				custom := options
				custom.AgentDir = filepath.Join(t.TempDir(), "settings")
				custom.StateDir = filepath.Join(t.TempDir(), "state")
				custom.Listen = "127.0.0.1:0"
				selected, err = agentOptions(custom, &to)
				if err != nil || selected.Agent != to || selected.AgentDir != custom.AgentDir || selected.StateDir != custom.StateDir || selected.Listen != custom.Listen {
					t.Fatalf("custom overrides changed: %+v, %v", selected, err)
				}
			})
		}
		selected, err := agentOptions(options, nil)
		if err != nil || selected != options {
			t.Fatalf("omitted agent changed server options: %+v, %v", selected, err)
		}
	}
}
