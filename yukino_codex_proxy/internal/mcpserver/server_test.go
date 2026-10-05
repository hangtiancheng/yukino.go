package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_codex_proxy/internal/daemon"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPTools(t *testing.T) {
	root := t.TempDir()
	server := New(daemon.Options{Protocol: "anthropic", StateDir: filepath.Join(root, "state"), ConfigPath: filepath.Join(root, "missing.yaml")})
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
	for _, name := range []string{"proxy_status", "shutdown_proxy"} {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("%s failed: %v", name, err)
		}
		data, _ := json.Marshal(result.StructuredContent)
		var decoded map[string]any
		if json.Unmarshal(data, &decoded) != nil || decoded["running"] != false {
			t.Fatalf("bad structured result: %s", data)
		}
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "start_proxy", Arguments: map[string]any{"protocol": "openai-compat", "name": "missing"}})
	if err != nil || !result.IsError {
		t.Fatal("start errors should be reported as MCP tool errors")
	}
}
