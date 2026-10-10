package chat_pipeline

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func newReactAgentLambda(ctx context.Context, cfg *config.Config) (*compose.Lambda, error) {
	agentCfg := &react.AgentConfig{
		MaxStep:            25,
		ToolReturnDirectly: map[string]struct{}{},
	}

	chatModel, err := newChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	agentCfg.ToolCallingModel = chatModel

	mcpTools, err := tools.GetLogMcpTool(ctx, cfg.MCP)
	if err != nil {
		return nil, err
	}
	agentCfg.ToolsConfig.Tools = mcpTools

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
