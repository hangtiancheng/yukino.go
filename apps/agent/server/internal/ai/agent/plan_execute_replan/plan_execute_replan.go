package plan_execute_replan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hangtiancheng/yukino-code/apps/yukino/conversation"
	sdktools "github.com/hangtiancheng/yukino-code/apps/yukino/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/runtime"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

type plan struct {
	Steps []string `json:"steps"`
}
type replanResponse struct {
	Done      bool     `json:"done"`
	Remaining []string `json:"remaining"`
	Summary   string   `json:"summary"`
}

func structured(ctx context.Context, cfg *config.Config, prompt string, target any) error {
	client, protocol, err := models.New(cfg, cfg.ThinkChatModel, "Return only the requested JSON object.")
	if err != nil {
		return err
	}
	registry := sdktools.NewRegistry()
	defer registry.Dispose()
	conv := conversation.NewManager()
	conv.AddUserMessage(prompt)
	text, err := runtime.Run(ctx, client, protocol, registry, conv, 1, nil)
	if err != nil {
		return err
	}
	raw, err := extractJSONObject(text)
	if err != nil {
		return err
	}
	if err = json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode structured output: %w", err)
	}
	return nil
}
func validSteps(steps []string) error {
	if len(steps) == 0 {
		return fmt.Errorf("plan has no remaining steps")
	}
	for _, step := range steps {
		if strings.TrimSpace(step) == "" {
			return fmt.Errorf("plan contains an empty step")
		}
	}
	return nil
}
func BuildPlanAgent(ctx context.Context, cfg *config.Config, query string) (string, []string, error) {
	var current plan
	if err := structured(ctx, cfg, "Break the task into concrete steps. Return {\"steps\":[\"step description\"]}. Task:\n"+query, &current); err != nil {
		return "", nil, fmt.Errorf("plan: %w", err)
	}
	if err := validSteps(current.Steps); err != nil {
		return "", nil, err
	}
	registry, err := tools.NewRegistry(ctx, cfg)
	if err != nil {
		return "", nil, err
	}
	defer registry.Dispose()
	var details []string
	var completed strings.Builder
	for iteration := 0; iteration < 20; iteration++ {
		step := current.Steps[0]
		client, protocol, err := models.New(cfg, cfg.QuickChatModel, "Execute the current step using tools. Follow the original task's constraints. Report findings and evidence.")
		if err != nil {
			return "", details, err
		}
		conv := conversation.NewManager()
		conv.AddUserMessage(fmt.Sprintf("Task:\n%s\nCompleted work:\n%s\nCurrent step:\n%s", query, completed.String(), step))
		result, err := runtime.Run(ctx, client, protocol, registry, conv, 100, nil)
		if err != nil {
			return "", details, fmt.Errorf("execute step: %w", err)
		}
		details = append(details, result)
		fmt.Fprintf(&completed, "Step: %s\nResult: %s\n", step, result)
		var next replanResponse
		prompt := fmt.Sprintf("Review progress. Return {\"done\":boolean,\"remaining\":[\"step\"],\"summary\":\"final report when done\"}. Only mark done when the entire task is achieved.\nTask:\n%s\nPlan:\n%v\nCompleted work:\n%s", query, current.Steps, completed.String())
		if err := structured(ctx, cfg, prompt, &next); err != nil {
			return "", details, fmt.Errorf("replan: %w", err)
		}
		if next.Done {
			if strings.TrimSpace(next.Summary) == "" {
				return "", details, fmt.Errorf("replanner completed without summary")
			}
			return next.Summary, details, nil
		}
		if err := validSteps(next.Remaining); err != nil {
			return "", details, err
		}
		current.Steps = next.Remaining
	}
	return "", details, fmt.Errorf("planning exceeded 20 iterations")
}
