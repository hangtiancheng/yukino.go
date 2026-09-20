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
