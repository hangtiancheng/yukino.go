// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Package tools provides AI agent tool implementations for the Yukino Chatbot.
// Each tool wraps a specific capability (MCP integration, Prometheus alerts,
// MySQL queries, documentation search, time retrieval) as an Eino tool
// that can be invoked by the AI agent during reasoning.
package tools

import (
	"context"
	"sync"

	eino_mcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/utility/logger"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCP tool cache. The MCP server provides log querying capabilities; it is a
// non-essential dependency, so connection failures degrade gracefully (empty tool
// set) rather than failing the whole chat/agent build. Successful connections are
// cached per URL so we don't reopen the SSE client on every request.
var (
	mcpMu        sync.Mutex
	cachedTools  []tool.BaseTool
	cachedURL    string
	mcpConnected bool
)

// GetLogMcpTool connects to an MCP (Model Context Protocol) server via SSE and
// returns the available tools from that server. Results are cached per mcpURL.
//
// If the MCP server is unreachable, it logs a warning and returns an empty tool
// slice with a nil error, so the chat / plan-execute pipelines keep working
// without log-querying capabilities. This mirrors the Next.js graceful-degradation
// behavior in lib/ai/tools/query-log.ts.
//
// See:
//   - https://www.cloudwego.io/zh/docs/eino/ecosystem_integration/tool/tool_mcp/
//   - https://mcp-go.dev/clients
func GetLogMcpTool(ctx context.Context, mcpURL string) ([]tool.BaseTool, error) {
	mcpMu.Lock()
	defer mcpMu.Unlock()

	// Serve from cache when we already connected to the same URL.
	if mcpConnected && cachedURL == mcpURL && cachedTools != nil {
		return cachedTools, nil
	}

	tools, err := buildMcpTools(ctx, mcpURL)
	if err != nil {
		// Degrade gracefully: log and return an empty tool set. Do not cache the
		// failure so the next request can retry once the MCP server comes back.
		logger.L().Warn("mcp connect failed, skipping log tools", "url", mcpURL, "err", err)
		return []tool.BaseTool{}, nil
	}

	cachedTools = tools
	cachedURL = mcpURL
	mcpConnected = true
	return tools, nil
}

// buildMcpTools opens a fresh SSE connection to the MCP server, initializes the
// session, and returns the tools advertised by that server.
func buildMcpTools(ctx context.Context, mcpURL string) ([]tool.BaseTool, error) {
	cli, err := client.NewSSEMCPClient(mcpURL)
	if err != nil {
		return nil, err
	}

	if err := cli.Start(ctx); err != nil {
		return nil, err
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "yukino-agent-client",
		Version: "1.0.0",
	}

	if _, err := cli.Initialize(ctx, initReq); err != nil {
		return nil, err
	}

	return eino_mcp.GetTools(ctx, &eino_mcp.Config{Cli: cli})
}
