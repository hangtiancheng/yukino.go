package chat_pipeline

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

// newReactAgentLambda creates a ReAct (Reasoning + Acting) agent that can use tools
// to answer questions. The agent iteratively reasons about the problem, selects tools,
// observes results, and generates a final response.
func newReactAgentLambda(ctx context.Context, cfg *config.Config) (*compose.Lambda, error) {
	// Use agentCfg (not config) to avoid shadowing the imported config package.
	agentCfg := &react.AgentConfig{
		MaxStep:            25,
		ToolReturnDirectly: map[string]struct{}{},
	}

	chatModel, err := newChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	agentCfg.ToolCallingModel = chatModel

	// Register MCP tools (log querying). GetLogMcpTool degrades to an empty set
	// when the MCP server is unreachable, so it never blocks agent construction.
	mcpTools, err := tools.GetLogMcpTool(ctx, cfg.MCP)
	if err != nil {
		return nil, err
	}
	agentCfg.ToolsConfig.Tools = mcpTools

	// Register additional tools.
	promTool, err := tools.NewPrometheusAlertsQueryTool(cfg.PrometheusURL)
	if err != nil {
		return nil, err
	}
	agentCfg.ToolsConfig.Tools = append(agentCfg.ToolsConfig.Tools, promTool)

	mysqlTool, err := tools.NewMysqlCrudTool()
	if err != nil {
		return nil, err
	}
	agentCfg.ToolsConfig.Tools = append(agentCfg.ToolsConfig.Tools, mysqlTool)

	timeTool, err := tools.NewGetCurrentTimeTool()
	if err != nil {
		return nil, err
	}
	agentCfg.ToolsConfig.Tools = append(agentCfg.ToolsConfig.Tools, timeTool)

	docsTool, err := tools.NewQueryInternalDocsTool(cfg)
	if err != nil {
		return nil, err
	}
	agentCfg.ToolsConfig.Tools = append(agentCfg.ToolsConfig.Tools, docsTool)

	agent, err := react.NewAgent(ctx, agentCfg)
	if err != nil {
		return nil, err
	}

	return compose.AnyLambda(agent.Generate, agent.Stream, nil, nil)
}
