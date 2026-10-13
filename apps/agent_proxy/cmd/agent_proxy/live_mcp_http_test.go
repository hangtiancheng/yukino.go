package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/daemon"
)

var liveMCPHTTP = flag.Bool("live-mcp-http", false, "Run MCP HTTP tools and real inference with temporary agent settings")

func TestLiveMCPHTTP(t *testing.T) {
	if !*liveMCPHTTP {
		t.Skip("pass -args -live-mcp-http to enable real MCP HTTP endpoint tests")
	}
	configPath, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Select(config.OpenAICompat, ""); err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"http", "sse"} {
		for _, name := range agent.Names() {
			t.Run(transport+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				options := daemon.Options{Agent: name, ConfigPath: configPath, AgentDir: filepath.Join(root, "settings"), StateDir: filepath.Join(root, "state"), Listen: "127.0.0.1:0"}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				endpoint := startMCPHTTPCLI(t, ctx, transport, "--agent", name, "--config", configPath, "--agent-dir", options.AgentDir, "--state-dir", options.StateDir, "--listen", options.Listen)
				// Always clean up the test daemon, including when an assertion fails.
				t.Cleanup(func() {
					cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
					defer stop()
					manager := daemon.Manager{Options: options}
					if err := manager.Shutdown(cleanupCtx); err != nil {
						t.Error(err)
					}
				})
				var wire mcp.Transport = &mcp.StreamableClientTransport{Endpoint: endpoint}
				if transport == "sse" {
					wire = &mcp.SSEClientTransport{Endpoint: endpoint}
				}
				cs, err := mcp.NewClient(&mcp.Implementation{Name: "live-mcp-test", Version: "1"}, nil).Connect(ctx, wire, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer cs.Close()
				call := func(tool string, args map[string]any) *mcp.CallToolResult {
					t.Helper()
					result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
					if err != nil || result.IsError {
						t.Fatalf("MCP %s failed: %+v, %v", tool, result, err)
					}
					return result
				}
				decodeStatus := func(result *mcp.CallToolResult) daemon.Status {
					t.Helper()
					data, err := json.Marshal(result.StructuredContent)
					if err != nil {
						t.Fatal(err)
					}
					var status daemon.Status
					if err := json.Unmarshal(data, &status); err != nil {
						t.Fatal(err)
					}
					return status
				}
				status := decodeStatus(call("start_proxy", map[string]any{"protocol": config.OpenAICompat}))
				if !status.Running || status.Agent != name || status.Protocol != config.OpenAICompat {
					t.Fatalf("wrong running daemon: %+v", status)
				}
				if current := decodeStatus(call("proxy_status", nil)); current.PID != status.PID || !current.Running {
					t.Fatalf("MCP status lost the daemon: %+v", current)
				}
				for _, stream := range []bool{false, true} {
					path := "/v1/messages"
					body := map[string]any{"model": "client-alias", "max_tokens": 256, "messages": []any{map[string]any{"role": "user", "content": "Reply with the single word OK."}}, "thinking": map[string]any{"type": "disabled"}, "stream": stream}
					terminal := []byte("event: message_stop")
					if name == agent.Codex {
						path = "/v1/responses"
						body = map[string]any{"model": "client-alias", "max_output_tokens": 256, "input": "Reply with the single word OK.", "reasoning": map[string]any{"effort": "none"}, "stream": stream}
						terminal = []byte("event: response.completed")
					}
					data, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, status.GatewayURL+path, bytes.NewReader(data))
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Authorization", "Bearer local-placeholder")
					res, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal("live proxy request failed")
					}
					response, err := io.ReadAll(res.Body)
					_ = res.Body.Close()
					if err != nil || res.StatusCode != http.StatusOK {
						t.Fatalf("live proxy response failed: status=%d stream=%t", res.StatusCode, stream)
					}
					if stream {
						if !bytes.Contains(response, terminal) || bytes.Count(response, []byte("data: ")) < 2 {
							t.Fatal("live SSE response did not complete")
						}
					} else if !json.Valid(response) || !bytes.Contains(response, []byte("OK")) {
						t.Fatal("live JSON response did not contain the requested answer")
					}
				}
				call("shutdown_proxy", nil)
				if stopped := decodeStatus(call("proxy_status", nil)); stopped.Running {
					t.Fatal("MCP shutdown did not stop the test daemon")
				}
				t.Logf("%s/%s: HTTP MCP start/status/shutdown and real Chat Completions JSON/SSE passed", transport, name)
			})
		}
	}
}
