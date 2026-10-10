package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const stdioServerEnv = "YUKINO_TEST_MCP_STDIO_SERVER"

func TestMain(m *testing.M) {
	if os.Getenv(stdioServerEnv) == "1" {
		ctx := context.Background()
		server := newTestMCPServer()
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

type echoArgs struct {
	Message string `json:"message"`
}

func newTestMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "v0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "echo a message"},
		func(ctx context.Context, req *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + args.Message}},
			}, nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "fail", Description: "always fails"},
		func(ctx context.Context, req *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "boom"}},
			}, nil, nil
		})
	return server
}

func TestGetLogMcpToolTransports(t *testing.T) {
	cases := []struct {
		name        string
		startServer func(t *testing.T) config.MCPConfig
	}{
		{
			name: "streamable_http",
			startServer: func(t *testing.T) config.MCPConfig {
				server := newTestMCPServer()
				handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
				ts := httptest.NewServer(handler)
				t.Cleanup(func() {
					ts.CloseClientConnections()
					ts.Close()
				})
				return config.MCPConfig{Transport: config.MCPTransportStreamableHTTP, URL: ts.URL}
			},
		},
		{
			name: "sse",
			startServer: func(t *testing.T) config.MCPConfig {
				server := newTestMCPServer()
				handler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil)
				ts := httptest.NewServer(handler)
				t.Cleanup(func() {
					ts.CloseClientConnections()
					ts.Close()
				})
				return config.MCPConfig{Transport: config.MCPTransportSSE, URL: ts.URL}
			},
		},
		{
			name: "stdio",
			startServer: func(t *testing.T) config.MCPConfig {
				return config.MCPConfig{
					Transport: config.MCPTransportStdio,
					Command:   os.Args[0],
					Args:      []string{"-test.run=^TestStdioHelper$"},
					Env:       map[string]string{stdioServerEnv: "1"},
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := tc.startServer(t)

			tools, err := GetLogMcpTool(ctx, cfg)
			if err != nil {
				t.Fatalf("GetLogMcpTool returned error: %v", err)
			}
			if len(tools) != 2 {
				t.Fatalf("got %d tools, want 2", len(tools))
			}

			byName := map[string]tool.BaseTool{}
			for _, tl := range tools {
				info, err := tl.Info(ctx)
				if err != nil {
					t.Fatalf("Info: %v", err)
				}
				if info.ParamsOneOf == nil {
					t.Errorf("tool %s has nil ParamsOneOf", info.Name)
				}
				byName[info.Name] = tl
			}
			if byName["echo"] == nil || byName["fail"] == nil {
				t.Fatalf("missing expected tools, got %v", keys(byName))
			}

			echo := byName["echo"].(tool.InvokableTool)
			out, err := echo.InvokableRun(ctx, `{"message":"hi"}`)
			if err != nil {
				t.Fatalf("InvokableRun echo: %v", err)
			}
			if out != "echo: hi" {
				t.Errorf("echo output = %q, want %q", out, "echo: hi")
			}

			fail := byName["fail"].(tool.InvokableTool)
			if _, err := fail.InvokableRun(ctx, `{"message":"x"}`); err == nil || !strings.Contains(err.Error(), "boom") {
				t.Errorf("fail tool error = %v, want it to contain %q", err, "boom")
			}

			cached, err := GetLogMcpTool(ctx, cfg)
			if err != nil {
				t.Fatalf("second GetLogMcpTool: %v", err)
			}
			if len(cached) != len(tools) || cached[0] != tools[0] {
				t.Errorf("second call did not hit the cache")
			}
		})
	}
}

func TestStdioHelper(t *testing.T) {}

func TestGetLogMcpToolDegradation(t *testing.T) {
	ctx := context.Background()

	server := newTestMCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	ts := httptest.NewServer(handler)
	url := ts.URL
	ts.Close()

	tools, err := GetLogMcpTool(ctx, config.MCPConfig{Transport: config.MCPTransportStreamableHTTP, URL: url})
	if err != nil {
		t.Fatalf("unreachable server: err = %v, want nil", err)
	}
	if len(tools) != 0 {
		t.Errorf("unreachable server: got %d tools, want 0", len(tools))
	}

	tools, err = GetLogMcpTool(ctx, config.MCPConfig{Transport: "carrier-pigeon", URL: url})
	if err != nil {
		t.Fatalf("invalid transport: err = %v, want nil", err)
	}
	if len(tools) != 0 {
		t.Errorf("invalid transport: got %d tools, want 0", len(tools))
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

type sentinelTool struct {
	id string
}

func (s *sentinelTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: s.id}, nil
}

func newThreeToolMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server-3", Version: "v0.0.1"}, nil)
	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: name},
			func(ctx context.Context, req *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: name}},
				}, nil, nil
			})
	}
	return server
}

// Callers append their own tools (prometheus, mysql, ...) to the slice
// returned by GetLogMcpTool. If the returned slice shared the cache's backing
// array with spare capacity, concurrent requests would overwrite each other's
// appended tools. The returned slice must therefore never have spare capacity
// and two calls must yield independent slices.
func TestGetLogMcpToolReturnsIndependentSlices(t *testing.T) {
	ctx := context.Background()

	server := newThreeToolMCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
	})
	cfg := config.MCPConfig{Transport: config.MCPTransportStreamableHTTP, URL: ts.URL}

	first, err := GetLogMcpTool(ctx, cfg)
	if err != nil {
		t.Fatalf("GetLogMcpTool: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("got %d tools, want 3", len(first))
	}
	if cap(first) != len(first) {
		t.Errorf("returned slice has spare capacity (cap=%d len=%d); caller appends could alias the cache", cap(first), len(first))
	}

	second, err := GetLogMcpTool(ctx, cfg)
	if err != nil {
		t.Fatalf("second GetLogMcpTool: %v", err)
	}
	if len(second) != len(first) {
		t.Fatalf("second call returned %d tools, want %d", len(second), len(first))
	}

	extFirst := append(first, &sentinelTool{id: "sentinel-a"})
	extSecond := append(second, &sentinelTool{id: "sentinel-b"})

	if s, ok := extFirst[len(first)].(*sentinelTool); !ok || s.id != "sentinel-a" {
		t.Errorf("first caller's appended tool was corrupted: %#v", extFirst[len(first)])
	}
	if s, ok := extSecond[len(second)].(*sentinelTool); !ok || s.id != "sentinel-b" {
		t.Errorf("second caller's appended tool was corrupted: %#v", extSecond[len(second)])
	}
	if len(first) != 3 || len(second) != 3 {
		t.Errorf("cached tool list length changed by caller appends: first=%d second=%d", len(first), len(second))
	}
}

// GetLogMcpTool is invoked with the HTTP request context, and the session
// behind the returned tools is cached for the process lifetime. The SSE
// transport binds its long-lived GET stream to the Connect context, so if the
// request context's cancellation propagated into the session, the cached
// session would die as soon as the first request completes and every later
// tool call would hang on the dead stream.
func TestSSECachedSessionSurvivesRequestContextCancellation(t *testing.T) {
	server := newTestMCPServer()
	handler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
	})
	cfg := config.MCPConfig{Transport: config.MCPTransportSSE, URL: ts.URL}

	reqCtx, cancel := context.WithCancel(context.Background())
	tools, err := GetLogMcpTool(reqCtx, cfg)
	if err != nil {
		t.Fatalf("GetLogMcpTool: %v", err)
	}
	var echo tool.InvokableTool
	for _, tl := range tools {
		info, err := tl.Info(reqCtx)
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if info.Name == "echo" {
			echo = tl.(tool.InvokableTool)
		}
	}
	if echo == nil {
		t.Fatalf("echo tool not found among %d tools", len(tools))
	}

	cancel() // simulate the first HTTP request finishing
	time.Sleep(200 * time.Millisecond)

	// Bounded call context so a regression fails fast instead of hanging.
	callCtx, callCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer callCancel()
	out, err := echo.InvokableRun(callCtx, `{"message":"hi"}`)
	if err != nil {
		t.Fatalf("cached SSE tool invocation failed after request ctx cancellation: %v", err)
	}
	if out != "echo: hi" {
		t.Errorf("echo output = %q, want %q", out, "echo: hi")
	}
}
