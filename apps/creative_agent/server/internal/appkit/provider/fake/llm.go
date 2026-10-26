package appkitfake

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// streamResponseDegraded 是 Fake LLM 的流式降级：同步结果 + 触发一次 isEndingChunk=true 回调。
// 对齐 react.LLM 接口注释："不支持流式的实现降级为同步调用后触发一次 isEndingChunk=true"。
func streamResponseDegraded(ctx context.Context, resp *react.LLMResponse, err error, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	if err != nil {
		return resp, err
	}
	if cb != nil && resp != nil {
		_ = cb(ctx, &react.StreamedChatResponsePayload{
			Choices: []react.StreamedChatChoice{{Delta: react.StreamedChatDelta{Role: "assistant", Content: resp.Content}}},
		}, true)
	}
	return resp, nil
}

// FakeLLM 是 Phase 1 同步测试用的查表 LLM。
// 第一轮返回 tool_call，第二轮返回 only_reply 文本。
type FakeLLM struct{ turn int }

func (l *FakeLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
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
			Content:   "Let me search for that.",
			ToolCalls: []react.ToolCall{{ID: "call_1", Name: "search", Args: `{"query":"user question"}`}},
		}, nil
	}
	return &react.LLMResponse{
		Content: `{"task_type":"only_reply","friendly_reply":"这是 agent 模拟生成的回复。"}`,
	}, nil
}

// ChatStream 实现 react.LLM 接口（流式降级）。
func (l *FakeLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}

// FakePlanLLM 是产出 new_agents_plan 的查表 LLM（Phase 3 管家链路用）。
//
// 第一轮返回 dispatch_agent 工具调用（模拟管家派子任务），
// 第二轮返回只含 new_agents_plan 的终态 JSON——SupervisorRunner 解析后
// 触发 Submitter 投递子任务。
type FakePlanLLM struct {
	turn int
}

// Chat 实现 react.LLM 接口。
func (l *FakePlanLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
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
			Content:   "我需要拆分任务给子智能体。",
			ToolCalls: []react.ToolCall{{ID: "call_dispatch", Name: "dispatch_agent", Args: `{"agent_id":"gold_scriptwriter","input":"生成一张海报"}`}},
		}, nil
	}
	// 终态：带 new_agents_plan
	return &react.LLMResponse{
		Content: `{"task_type":"new_agents_plan","friendly_reply":"已为你派发子任务，请稍候。","plan_steps":{"agents":[{"agent_id":"gold_scriptwriter","input":"生成一张海报"}]}}`,
	}, nil
}

// ChatStream 实现 react.LLM 接口（流式降级）。
func (l *FakePlanLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}
