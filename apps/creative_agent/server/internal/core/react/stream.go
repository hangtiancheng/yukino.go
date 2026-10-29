package react

import "context"

// StreamCallbackFunc is the LLM streaming callback: triggered once per chunk; isEndingChunk=true
// marks the end of the stream (including usage).
type StreamCallbackFunc func(ctx context.Context, payload *StreamedChatResponsePayload, isEndingChunk bool) error

// StreamedChatDelta is the delta of a single chunk in a streaming response.
type StreamedChatDelta struct {
	Role             string             `json:"role,omitempty"`
	Content          string             `json:"content,omitempty"`
	ReasoningContent string             `json:"reasoning_content,omitempty"` // DeepSeek, etc.
	Reasoning        string             `json:"reasoning,omitempty"`         // GLM, etc.
	ToolCalls        []StreamedToolCall `json:"tool_calls,omitempty"`
}

// ReasoningText returns the reasoning text, compatible with both field names (GLM uses reasoning, others use reasoning_content).
func (d *StreamedChatDelta) ReasoningText() string {
	if d.Reasoning != "" {
		return d.Reasoning
	}
	return d.ReasoningContent
}

// StreamedChatChoice is one candidate in a streaming response.
type StreamedChatChoice struct {
	Index        int               `json:"index,omitempty"`
	Delta        StreamedChatDelta `json:"delta,omitempty"`
	FinishReason string            `json:"finish_reason,omitempty"`
}

// StreamedChatUsage is the token statistics of a streaming response (carried in the final chunk when stream_options.include_usage=true).
type StreamedChatUsage struct {
	CompletionTokens int64 `json:"completion_tokens,omitempty"`
	PromptTokens     int64 `json:"prompt_tokens,omitempty"`
	TotalTokens      int64 `json:"total_tokens,omitempty"`
}

// StreamedChatResponsePayload is the payload of one streaming callback.
type StreamedChatResponsePayload struct {
	ID      string               `json:"id,omitempty"`
	Created int64                `json:"created,omitempty"`
	Model   string               `json:"model,omitempty"`
	Object  string               `json:"object,omitempty"`
	Choices []StreamedChatChoice `json:"choices,omitempty"`
	Usage   StreamedChatUsage    `json:"usage,omitempty"`
}

// StreamedToolCall is a streaming tool-call delta.
// Note: in OpenAI streams, id/name appear only in the first chunk; later chunks contain only index + arguments deltas,
// so they must be merged and accumulated by index.
type StreamedToolCall struct {
	Index    *int                     `json:"index,omitempty"`
	ID       string                   `json:"id,omitempty"`
	Type     string                   `json:"type,omitempty"`
	Function StreamedToolCallFunction `json:"function,omitempty"`
}

// StreamedToolCallFunction is the function delta of a streaming tool call.
type StreamedToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
