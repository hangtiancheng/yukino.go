package appkitfake

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/llm"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// DefaultSubLLM returns the sub-agent LLM: the real LLM when configured, otherwise FakeSubLLM.
func DefaultSubLLM() react.LLM {
	if appkitllm.DefaultRealLLM != nil {
		return appkitllm.DefaultRealLLM
	}
	return &FakeSubLLM{}
}

// FakeSubLLM is the table-lookup LLM for sub-agents (the replica's fake implementation).
//
// Behavior:
//   - First round (no tool observation) → returns a generate_image tool call
//   - Second round (with tool observation) → returns Terminal text
//
// Difference from FakeLLM: sub-agents use execution tools (generate_image) and produce
// execution-class products, not the manager's only_reply.
type FakeSubLLM struct {
	turn int
}

// Chat implements the react.LLM interface.
func (l *FakeSubLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	_ = ctx
	l.turn++
	hasToolResult := false
	for _, m := range messages {
		if m.Role == "tool" {
			hasToolResult = true
			break
		}
	}
	if !hasToolResult {
		return &react.LLMResponse{
			Content:   "我来生成图片。",
			ToolCalls: []react.ToolCall{{ID: "call_gen", Name: "generate_image", Args: `{"prompt":"风景海报","image_ratio":"16:9"}`}},
		}, nil
	}
	return &react.LLMResponse{
		Content: "已生成图片并交付：https://img.example.com/gen/12345.png",
	}, nil
}

// ChatStream implements the react.LLM interface (streaming degradation).
func (l *FakeSubLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}

// FakeSubLLMPipeline is the table-lookup LLM that triggers the async pipeline.
// First round → create_video_task (isPipeline=true) → SubAgent terminates early + NeedWait=Yes.
type FakeSubLLMPipeline struct {
	turn int
}

// Chat implements the react.LLM interface.
func (l *FakeSubLLMPipeline) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	_ = ctx
	l.turn++
	for _, m := range messages {
		if m.Role == "tool" {
			// Tool result present → back to Terminal
			return &react.LLMResponse{Content: "视频合成任务已提交到产线，等待完成通知。"}, nil
		}
	}
	return &react.LLMResponse{
		Content:   "我来提交视频合成任务。",
		ToolCalls: []react.ToolCall{{ID: "call_video", Name: "create_video_task", Args: `{"prompt":"城市夜景"}`}},
	}, nil
}

// ChatStream implements the react.LLM interface (streaming degradation).
func (l *FakeSubLLMPipeline) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}
