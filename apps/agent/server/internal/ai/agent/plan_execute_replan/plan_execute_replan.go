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
		if event.AgentName == executorName && msg.Role == schema.Assistant && len(msg.ToolCalls) == 0 && msg.Content != "" {
			detail = append(detail, msg.Content)
		}
	}

	if lastMessage == nil {
		return "", nil, fmt.Errorf("no response generated")
	}
	return lastMessage.Content, detail, nil
}
