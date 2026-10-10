package chat_pipeline

import (
	"context"
	"time"
)

func newInputToRagLambda(ctx context.Context, input *UserMessage, opts ...any) (string, error) {
	return input.Query, nil
}

func newInputToChatLambda(ctx context.Context, input *UserMessage, opts ...any) (map[string]any, error) {
	return map[string]any{
		"content": input.Query,
		"history": input.History,
		"date":    time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}
