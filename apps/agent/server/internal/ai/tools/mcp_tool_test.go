package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stdioServerEnv marks the test binary to run as an MCP stdio server
// subprocess instead of running tests (fork-and-exec trick).
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
		name string
		// startServer returns the MCP config endpoint for the transport and a cleanup func.
		startServer func(t *testing.T) config.MCPConfig
	}{
		{
			name: "streamable_http",
			startServer: func(t *testing.T) config.MCPConfig {
				server := newTestMCPServer()
				handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
				ts := httptest.NewServer(handler)
				// CloseClientConnections first: the cached MCP session keeps a
				// long-lived SSE stream open, and Close blocks on outstanding requests.
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

			// A second call with the same config is served from cache.
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

// TestStdioHelper is a placeholder so -test.run=^TestStdioHelper$ matches; the
// actual stdio server runs through TestMain via stdioServerEnv.
func TestStdioHelper(t *testing.T) {}

func TestGetLogMcpToolDegradation(t *testing.T) {
	ctx := context.Background()

	// Closed server: connection fails, GetLogMcpTool must degrade to an empty tool set.
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

	// Invalid transport degrades the same way.
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
