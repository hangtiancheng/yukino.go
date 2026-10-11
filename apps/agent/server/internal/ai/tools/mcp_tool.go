package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	mcpMu         sync.Mutex
	cachedTools   []tool.BaseTool
	cachedKey     string
	cachedSession *mcp.ClientSession
)

func GetLogMcpTool(ctx context.Context, cfg config.MCPConfig) ([]tool.BaseTool, error) {
	mcpMu.Lock()
	defer mcpMu.Unlock()

	key, err := mcpCacheKey(cfg)
	if err != nil {
		return nil, err
	}

	if cachedKey == key && cachedTools != nil {
		// Clone so callers appending to the returned slice can never alias (and
		// race on) the cached backing array.
		return slices.Clone(cachedTools), nil
	}

	tools, session, err := buildMcpTools(ctx, cfg)
	if err != nil {
		logger.L().Warn("mcp connect failed, skipping log tools", "transport", cfg.Transport, "err", err)
		return []tool.BaseTool{}, nil
	}

	if cachedSession != nil {
		// The config changed; release the previous session (this also stops a
		// stdio MCP server process we may have started).
		cachedSession.Close()
	}
	cachedTools = tools
	cachedKey = key
	cachedSession = session
	return slices.Clone(tools), nil
}

// invalidateMcpSession drops the cached tools when they belong to the given
// session. It runs when the session's connection dies (watchSession) or when a
// tool call fails at the transport level (mcpToolAdapter.InvokableRun), so the
// next GetLogMcpTool call reconnects instead of serving tools bound to a dead
// session for the rest of the process lifetime. The legacy implementation
// reconnected on every request; this preserves that resilience while keeping
// one long-lived session.
func invalidateMcpSession(session *mcp.ClientSession) {
	mcpMu.Lock()
	if cachedSession == session {
		cachedTools = nil
		cachedKey = ""
		cachedSession = nil
	}
	mcpMu.Unlock()

	// Close outside the lock. Close is idempotent; for a session whose
	// transport already died it is a no-op, and it unblocks the session's
	// watchSession goroutine so it cannot leak.
	session.Close()
}

// watchSession blocks until the MCP connection is closed by the server or
// broken by a transport failure, then invalidates the cache. Transports
// without a persistent stream (newer streamable-HTTP protocol revisions drop
// the standalone SSE listener) never notice server death here; those are
// covered by the CallTool-time invalidation in InvokableRun.
func watchSession(session *mcp.ClientSession) {
	_ = session.Wait()
	invalidateMcpSession(session)
}

func mcpCacheKey(cfg config.MCPConfig) (string, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal mcp config: %w", err)
	}
	return string(raw), nil
}

func buildMcpTools(ctx context.Context, cfg config.MCPConfig) ([]tool.BaseTool, *mcp.ClientSession, error) {
	// GetLogMcpTool caches the session behind the returned tools for the
	// lifetime of the process, but ctx here is typically the first HTTP
	// request's context. The SSE transport binds its long-lived GET stream to
	// the context passed to Connect, so inheriting the request's cancellation
	// would kill the cached session as soon as that request completes and
	// every later tool call would hang on the dead stream. Detach
	// cancellation (keeping values) so the session only ends when the server
	// closes it or the process exits.
	ctx = context.WithoutCancel(ctx)

	transport, err := newMcpTransport(cfg)
	if err != nil {
		return nil, nil, err
	}

	cli := mcp.NewClient(&mcp.Implementation{
		Name:    "yukino-agent-client",
		Version: "1.0.0",
	}, nil)

	session, err := cli.Connect(ctx, transport, nil)
	if err != nil {
		return nil, nil, err
	}

	tools, err := listMcpTools(ctx, session)
	if err != nil {
		session.Close()
		return nil, nil, err
	}

	go watchSession(session)

	return tools, session, nil
}

func newMcpTransport(cfg config.MCPConfig) (mcp.Transport, error) {
	switch cfg.Transport {
	case config.MCPTransportStreamableHTTP:
		if cfg.URL == "" {
			return nil, fmt.Errorf("mcp transport %q requires mcp.url", cfg.Transport)
		}
		return &mcp.StreamableClientTransport{Endpoint: cfg.URL}, nil
	case config.MCPTransportSSE:
		if cfg.URL == "" {
			return nil, fmt.Errorf("mcp transport %q requires mcp.url", cfg.Transport)
		}
		return &mcp.SSEClientTransport{Endpoint: cfg.URL}, nil
	case config.MCPTransportStdio:
		if cfg.Command == "" {
			return nil, fmt.Errorf("mcp transport %q requires mcp.command", cfg.Transport)
		}
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		return &mcp.CommandTransport{Command: cmd}, nil
	default:
		return nil, fmt.Errorf("unsupported mcp transport %q (want streamable_http, sse or stdio)", cfg.Transport)
	}
}

func listMcpTools(ctx context.Context, session *mcp.ClientSession) ([]tool.BaseTool, error) {
	tools := make([]tool.BaseTool, 0)
	for t, err := range session.Tools(ctx, &mcp.ListToolsParams{}) {
		if err != nil {
			return nil, fmt.Errorf("list mcp tools: %w", err)
		}
		info, err := einoToolInfo(t)
		if err != nil {
			return nil, err
		}
		tools = append(tools, &mcpToolAdapter{session: session, name: t.Name, info: info})
	}
	return tools, nil
}

func einoToolInfo(t *mcp.Tool) (*schema.ToolInfo, error) {
	info := &schema.ToolInfo{
		Name: t.Name,
		Desc: t.Description,
	}
	if t.InputSchema == nil {
		return info, nil
	}

	raw, err := json.Marshal(t.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal input schema of mcp tool %s: %w", t.Name, err)
	}
	s := &jsonschema.Schema{}
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, fmt.Errorf("parse input schema of mcp tool %s: %w", t.Name, err)
	}
	info.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(s)
	return info, nil
}

type mcpToolAdapter struct {
	session *mcp.ClientSession
	name    string
	info    *schema.ToolInfo
}

func (t *mcpToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *mcpToolAdapter) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args map[string]any
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
			return "", fmt.Errorf("parse arguments for mcp tool %s: %w", t.name, err)
		}
	}

	result, err := t.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      t.name,
		Arguments: args,
	})
	if err != nil {
		// A request-level failure (as opposed to result.IsError, which is a
		// tool-level failure) usually means the session's transport is gone.
		// Drop the cache so the next agent build reconnects. Skip when our own
		// context was cancelled: the session may be perfectly healthy.
		if ctx.Err() == nil {
			invalidateMcpSession(t.session)
		}
		return "", fmt.Errorf("call mcp tool %s: %w", t.name, err)
	}
	return formatCallResult(t.name, result)
}

func formatCallResult(name string, result *mcp.CallToolResult) (string, error) {
	text := extractText(result)
	if result.IsError {
		if text == "" {
			text = marshalContent(result)
		}
		return "", fmt.Errorf("mcp tool %s returned error: %s", name, text)
	}
	if text != "" {
		return text, nil
	}
	return marshalContent(result), nil
}

func extractText(result *mcp.CallToolResult) string {
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func marshalContent(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		if result.StructuredContent == nil {
			return ""
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return fmt.Sprintf("%v", result.StructuredContent)
		}
		return string(raw)
	}
	raw, err := json.Marshal(result.Content)
	if err != nil {
		return fmt.Sprintf("%v", result.Content)
	}
	return string(raw)
}
