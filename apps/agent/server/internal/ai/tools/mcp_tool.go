// Package tools provides AI agent tool implementations for the Yukino Chatbot.
// Each tool wraps a specific capability (MCP integration, Prometheus alerts,
// MySQL queries, documentation search, time retrieval) as an Eino tool
// that can be invoked by the AI agent during reasoning.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP tool cache. The MCP server provides log querying capabilities; it is a
// non-essential dependency, so connection failures degrade gracefully (empty tool
// set) rather than failing the whole chat/agent build. The connected session is
// cached per config so we don't reopen the transport on every request.
var (
	mcpMu       sync.Mutex
	cachedTools []tool.BaseTool
	cachedKey   string
)

// GetLogMcpTool connects to an MCP (Model Context Protocol) server using the
// configured transport (streamable_http, sse or stdio) and returns the tools
// advertised by that server as Eino tools. Results are cached per config.
//
// If the MCP server is unreachable, it logs a warning and returns an empty tool
// slice with a nil error, so the chat / plan-execute pipelines keep working
// without log-querying capabilities.
//
// See:
//   - https://modelcontextprotocol.io/
//   - https://github.com/modelcontextprotocol/go-sdk
func GetLogMcpTool(ctx context.Context, cfg config.MCPConfig) ([]tool.BaseTool, error) {
	mcpMu.Lock()
	defer mcpMu.Unlock()

	key, err := mcpCacheKey(cfg)
	if err != nil {
		return nil, err
	}

	// Serve from cache when we already connected with the same config.
	if cachedKey == key && cachedTools != nil {
		return cachedTools, nil
	}

	tools, err := buildMcpTools(ctx, cfg)
	if err != nil {
		// Degrade gracefully: log and return an empty tool set. Do not cache the
		// failure so the next request can retry once the MCP server comes back.
		logger.L().Warn("mcp connect failed, skipping log tools", "transport", cfg.Transport, "err", err)
		return []tool.BaseTool{}, nil
	}

	cachedTools = tools
	cachedKey = key
	return tools, nil
}

// mcpCacheKey derives a deterministic cache key from the MCP config.
// encoding/json sorts map keys, so the marshaled config is stable.
func mcpCacheKey(cfg config.MCPConfig) (string, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal mcp config: %w", err)
	}
	return string(raw), nil
}

// buildMcpTools opens a fresh connection over the configured transport,
// initializes the session, and returns the tools advertised by the server.
// On failure the partially established session is closed; on success the
// session stays open and is shared by the returned tools.
func buildMcpTools(ctx context.Context, cfg config.MCPConfig) ([]tool.BaseTool, error) {
	transport, err := newMcpTransport(cfg)
	if err != nil {
		return nil, err
	}

	cli := mcp.NewClient(&mcp.Implementation{
		Name:    "yukino-agent-client",
		Version: "1.0.0",
	}, nil)

	// Connect starts the transport and performs the MCP initialize handshake.
	session, err := cli.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}

	tools, err := listMcpTools(ctx, session)
	if err != nil {
		session.Close()
		return nil, err
	}
	return tools, nil
}

// newMcpTransport builds the go-sdk transport matching cfg.Transport.
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

// listMcpTools enumerates the server tools (following pagination) and wraps
// each one as an Eino tool backed by the shared session.
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

// einoToolInfo converts an MCP tool definition into Eino tool metadata. The
// input schema is round-tripped through JSON because the go-sdk exposes it as
// an opaque value (map[string]any from the wire).
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

// mcpToolAdapter exposes one MCP server tool as an Eino invokable tool backed
// by the established go-sdk client session.
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
		return "", fmt.Errorf("call mcp tool %s: %w", t.name, err)
	}
	return formatCallResult(t.name, result)
}

// formatCallResult renders an MCP call result as the string observation sent
// back to the model. Text content is passed through directly; other content
// types (images, resources, structured output) fall back to JSON. A result
// flagged IsError is surfaced as a Go error so the model can self-correct.
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

// extractText concatenates the text parts of the result content.
func extractText(result *mcp.CallToolResult) string {
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// marshalContent renders non-text result payloads as JSON.
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
