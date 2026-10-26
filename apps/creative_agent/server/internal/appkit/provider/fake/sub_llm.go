package appkitfake

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/llm"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// DefaultSubLLM 返回子智能体 LLM：配置了真实 LLM 用真实，否则 FakeSubLLM。
func DefaultSubLLM() react.LLM {
	if appkitllm.DefaultRealLLM != nil {
		return appkitllm.DefaultRealLLM
	}
	return &FakeSubLLM{}
}

// FakeSubLLM 是子智能体（SubAgent）用的查表 LLM（复刻的假实现）。
//
// 行为：
//   - 第一轮（无 tool observation）→ 返回 generate_image 工具调用
//   - 第二轮（有 tool observation）→ 返回 Terminal 文本
//
// 与 FakeLLM 的区别：子智能体走产线工具（generate_image），产出的是
// 执行类产物，而不是管家的 only_reply。
type FakeSubLLM struct {
	turn int
}

// Chat 实现 react.LLM 接口。
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

// ChatStream 实现 react.LLM 接口（流式降级）。
func (l *FakeSubLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}

// FakeSubLLMPipeline 是触发异步产线的查表 LLM。
// 第一轮 → create_video_task（isPipeline=true）→ SubAgent 提前终止 + NeedWait=Yes。
type FakeSubLLMPipeline struct {
	turn int
}

// Chat 实现 react.LLM 接口。
func (l *FakeSubLLMPipeline) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	_ = ctx
	l.turn++
	for _, m := range messages {
		if m.Role == "tool" {
			// 有工具结果 → 回到 Terminal
			return &react.LLMResponse{Content: "视频合成任务已提交到产线，等待完成通知。"}, nil
		}
	}
	return &react.LLMResponse{
		Content:   "我来提交视频合成任务。",
		ToolCalls: []react.ToolCall{{ID: "call_video", Name: "create_video_task", Args: `{"prompt":"城市夜景"}`}},
	}, nil
}

// ChatStream 实现 react.LLM 接口（流式降级）。
func (l *FakeSubLLMPipeline) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}
