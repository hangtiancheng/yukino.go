package plan_execute_replan

import (
	"context"

	"github.com/cloudwego/eino/adk"
	plan_execute "github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/compose"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/tools"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
)

// NewExecutor creates the execution agent that carries out each step of the plan
// using the available tools. It uses the quick model for fast tool execution.
//
// The tool set mirrors the chat pipeline (MCP + Prometheus + MySQL + docs + time)
// so plan-execute-replan and chat have identical capabilities. MaxIterations is
// capped at 10 per step (aligned with Next.js isStepCount(10)) to prevent runaway
// tool-calling loops.
func NewExecutor(ctx context.Context, cfg *config.Config) (adk.Agent, error) {
	// Register MCP tools for log querying. GetLogMcpTool degrades to an empty set
	// when the MCP server is unreachable.
	toolList, err := tools.GetLogMcpTool(ctx, cfg.MCP_URL)
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
		MaxIterations: 10,
	})
}
