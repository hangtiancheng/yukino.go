package chat_pipeline

import (
	"context"
	"time"
)

// newInputToRagLambda extracts the query string from the UserMessage for use
// as the search input to the Redis retriever.
func newInputToRagLambda(ctx context.Context, input *UserMessage, opts ...any) (string, error) {
	return input.Query, nil
}

// newInputToChatLambda transforms the UserMessage into a map of template variables
// for the chat prompt template. It includes the query content, conversation history,
// and the current date.
func newInputToChatLambda(ctx context.Context, input *UserMessage, opts ...any) (map[string]any, error) {
	return map[string]any{
		"content": input.Query,
		"history": input.History,
		"date":    time.Now().Format("1/2/2006, 3:04:05 PM"),
	}, nil
}
