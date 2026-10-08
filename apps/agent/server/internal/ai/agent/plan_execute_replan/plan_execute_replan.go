// Package plan_execute_replan implements a Plan-Execute-Replan agent pattern.
// The planner creates an execution plan, the executor carries out each step
// using available tools, and the replanner adjusts the plan based on results.
// This pattern is well-suited for complex multi-step AI operations tasks.
package plan_execute_replan

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	plan_execute "github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/logger"
)

// BuildPlanAgent creates and runs a plan-execute-replan agent pipeline.
// It returns the final response content, a list of detail messages from each step,
// and any error encountered during execution.
func BuildPlanAgent(ctx context.Context, cfg *config.Config, query string) (string, []string, error) {
	planAgent, err := NewPlanner(ctx, cfg)
	if err != nil {
		return "", nil, err
	}
	executeAgent, err := NewExecutor(ctx, cfg)
	if err != nil {
		return "", nil, err
	}
	replanAgent, err := NewRePlanAgent(ctx, cfg)
	if err != nil {
		return "", nil, err
	}

	planExecuteAgent, err := plan_execute.New(ctx, &plan_execute.Config{
		Planner:       planAgent,
		Executor:      executeAgent,
		Replanner:     replanAgent,
		MaxIterations: 20,
	})
	if err != nil {
		return "", nil, fmt.Errorf("build PlanExecuteAgent: %w", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: planExecuteAgent})
	iter := runner.Query(ctx, query)

	executorName := executeAgent.Name(ctx)

	var lastMessage adk.Message
	var detail []string
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		// Log one structured line per event. The event payload is deliberately
		// not dumped here: reading Output.MessageOutput.MessageStream would
		// drain it before adk.GetMessage below could consume it.
		logger.L().Info("plan-execute event",
			"agent", event.AgentName,
			"path", event.RunPath,
			"has_output", event.Output != nil,
			"err", event.Err,
		)
		if event.Output == nil {
			continue
		}
		msg, _, err := adk.GetMessage(event)
		if err != nil {
			continue
		}
		lastMessage = msg
		// Collect only each step's final executor answer as plain markdown.
		// Planner/replanner JSON, tool-call turns and Message.String() debug
		// dumps would render as garbage in the client step list.
		if event.AgentName == executorName && msg.Role == schema.Assistant && len(msg.ToolCalls) == 0 && msg.Content != "" {
			detail = append(detail, msg.Content)
		}
	}

	if lastMessage == nil {
		return "", nil, fmt.Errorf("no response generated")
	}
	return lastMessage.Content, detail, nil
}
