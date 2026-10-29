package appkitfake

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// streamResponseDegraded is the Fake LLM's streaming degradation: synchronous result + one isEndingChunk=true callback.
// Aligned with the react.LLM interface note: "implementations without streaming support degrade to a synchronous call followed by one isEndingChunk=true callback".
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

// FakeLLM is the table-lookup LLM for Phase 1 synchronous tests.
// The first round returns a tool_call; the second returns only_reply text.
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

// ChatStream implements the react.LLM interface (streaming degradation).
func (l *FakeLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}

// FakePlanLLM is the table-lookup LLM that produces new_agents_plan (for the Phase 3 manager chain).
//
// The first round returns a dispatch_agent tool call (simulating the manager dispatching subtasks);
// the second round returns the terminal JSON containing only new_agents_plan — after SupervisorRunner parses it,
// the Submitter is triggered to dispatch subtasks.
type FakePlanLLM struct {
	turn int
}

// Chat implements the react.LLM interface.
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
	// Terminal state: carries new_agents_plan
	return &react.LLMResponse{
		Content: `{"task_type":"new_agents_plan","friendly_reply":"已为你派发子任务，请稍候。","plan_steps":{"agents":[{"agent_id":"gold_scriptwriter","input":"生成一张海报"}]}}`,
	}, nil
}

// ChatStream implements the react.LLM interface (streaming degradation).
func (l *FakePlanLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}
