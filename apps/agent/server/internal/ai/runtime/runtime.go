package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino-code/apps/yukino/agent"
	"github.com/hangtiancheng/yukino-code/apps/yukino/conversation"
	"github.com/hangtiancheng/yukino-code/apps/yukino/llm"
	"github.com/hangtiancheng/yukino-code/apps/yukino/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/logger"
)

func Run(ctx context.Context, client llm.Client, protocol string, registry *tools.Registry, conv *conversation.Manager, maxIterations int, emit func(string)) (string, error) {
	initialLength := len(conv.GetMessages())
	runner := agent.New(client, registry, protocol)
	runner.MaxIterations = maxIterations
	var runErr error
	complete := false
	for event := range runner.Run(ctx, conv) {
		switch event := event.(type) {
		case agent.StreamText:
			if emit != nil && ctx.Err() == nil {
				emit(event.Text)
			}
		case agent.ErrorEvent:
			runErr = errors.Join(runErr, errors.New(event.Message))
		case agent.ToolUseEvent:
			logger.L().Info("agent tool call", "tool", event.ToolName)
		case agent.ToolResultEvent:
			logger.L().Info("agent tool result", "tool", event.ToolName, "error", event.IsError, "elapsed", event.Elapsed)
		case agent.LoopComplete:
			complete = true
			switch event.StopReason {
			case "end_turn", "stop", "stop_sequence", "completed":
			default:
				runErr = errors.Join(runErr, fmt.Errorf("agent stopped: %s", event.StopReason))
			}
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if runErr != nil {
		return "", runErr
	}
	if !complete {
		return "", fmt.Errorf("agent ended without completion")
	}
	messages := conv.GetMessages()
	for n := len(messages) - 1; n >= initialLength; n-- {
		if messages[n].Role == "assistant" && len(messages[n].ToolUses) == 0 && messages[n].ContentText() != "" {
			return messages[n].ContentText(), nil
		}
	}
	return "", fmt.Errorf("agent generated no answer")
}
