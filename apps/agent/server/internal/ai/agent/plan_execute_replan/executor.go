package plan_execute_replan

import (
	"context"

	"github.com/cloudwego/eino/adk"
	plan_execute "github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/compose"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func NewExecutor(ctx context.Context, cfg *config.Config) (adk.Agent, error) {
	toolList, err := tools.GetLogMcpTool(ctx, cfg.MCP)
	if err != nil {
		return nil, err
	}

	promTool, err := tools.NewPrometheusAlertsQueryTool(cfg.PrometheusURL)
	if err != nil {
		return nil, err
	}
	toolList = append(toolList, promTool)

	docsTool, err := tools.NewQueryInternalDocsTool(cfg)
	if err != nil {
		return nil, err
	}
	toolList = append(toolList, docsTool)

	timeTool, err := tools.NewGetCurrentTimeTool()
	if err != nil {
		return nil, err
	}
	toolList = append(toolList, timeTool)

	mysqlTool, err := tools.NewMysqlCrudTool()
	if err != nil {
		return nil, err
	}
	toolList = append(toolList, mysqlTool)

	execModel, err := models.NewQuickChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return plan_execute.NewExecutor(ctx, &plan_execute.ExecutorConfig{
		Model: execModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolList,
			},
		},
		// Legacy parity: a single plan step may fan out one tool call per alert
		// (query_internal_docs / log queries for every active alert), so the
		// per-step ReAct loop stays effectively unbounded. The overall loop is
		// still capped by plan_execute.Config.MaxIterations in BuildPlanAgent.
		MaxIterations: 999999,
	})
}
