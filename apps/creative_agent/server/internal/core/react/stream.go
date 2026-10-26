package react

import "context"

// StreamCallbackFunc 是 LLM 流式回调：每个 chunk 触发一次，isEndingChunk=true
// 表示本次流结束（含 usage）。
type StreamCallbackFunc func(ctx context.Context, payload *StreamedChatResponsePayload, isEndingChunk bool) error

// StreamedChatDelta 是流式响应中单个 chunk 的增量。
type StreamedChatDelta struct {
	Role             string             `json:"role,omitempty"`
	Content          string             `json:"content,omitempty"`
	ReasoningContent string             `json:"reasoning_content,omitempty"` // DeepSeek 等
	Reasoning        string             `json:"reasoning,omitempty"`         // GLM 等
	ToolCalls        []StreamedToolCall `json:"tool_calls,omitempty"`
}

// ReasoningText 兼容返回思考文本（GLM 用 reasoning，其余用 reasoning_content）。
func (d *StreamedChatDelta) ReasoningText() string {
	if d.Reasoning != "" {
		return d.Reasoning
	}
	return d.ReasoningContent
}

// StreamedChatChoice 是流式响应中的一个候选。
type StreamedChatChoice struct {
	Index        int               `json:"index,omitempty"`
	Delta        StreamedChatDelta `json:"delta,omitempty"`
	FinishReason string            `json:"finish_reason,omitempty"`
}

// StreamedChatUsage 是流式响应的 token 统计（stream_options.include_usage=true 时末包携带）。
type StreamedChatUsage struct {
	CompletionTokens int64 `json:"completion_tokens,omitempty"`
	PromptTokens     int64 `json:"prompt_tokens,omitempty"`
	TotalTokens      int64 `json:"total_tokens,omitempty"`
}

// StreamedChatResponsePayload 是一次流式回调的载荷。
type StreamedChatResponsePayload struct {
	ID      string               `json:"id,omitempty"`
	Created int64                `json:"created,omitempty"`
	Model   string               `json:"model,omitempty"`
	Object  string               `json:"object,omitempty"`
	Choices []StreamedChatChoice `json:"choices,omitempty"`
	Usage   StreamedChatUsage    `json:"usage,omitempty"`
}

// StreamedToolCall 是流式工具调用增量。
// 注意：OpenAI 流式中 id/name 只在首 chunk 出现，后续 chunk 仅含 index + arguments 增量，
// 必须按 index 归并累积。
type StreamedToolCall struct {
	Index    *int                     `json:"index,omitempty"`
	ID       string                   `json:"id,omitempty"`
	Type     string                   `json:"type,omitempty"`
	Function StreamedToolCallFunction `json:"function,omitempty"`
}

// StreamedToolCallFunction 是流式工具调用的函数增量。
type StreamedToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
