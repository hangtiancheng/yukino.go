package chat_pipeline

import "github.com/cloudwego/eino/schema"

// UserMessage represents the input to the chat agent pipeline.
// It carries the user's query along with conversation history for context.
type UserMessage struct {
	// ID is the unique session identifier used for memory management.
	ID string `json:"id"`

	// Query is the user's current question or message.
	Query string `json:"query"`

	// History contains previous messages in the conversation for context.
	History []*schema.Message `json:"history"`
}
