package appkitfake

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// FakeHILLLM is the LLM implementation of the HIL (Human-in-the-Loop) breakpoint chain (Phase 4).
//
// Behavior: without tools it returns text containing the "确认" trigger, working with FakeHILStrategy —
//   - First run (query contains the trigger word) → ActionInterrupt (push a card and wait for user confirmation)
//   - Resume round (Runtime.Interrupted=true) → terminate directly
//
// Currently a table-lookup simulation; can later be replaced with a real LLM (see llm_http.go for the OpenAI-compatible HTTP adapter).
type FakeHILLLM struct {
	turn int
}

func (l *FakeHILLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	_ = ctx
	l.turn++
	return &react.LLMResponse{Content: "hil_keyword 需要确认"}, nil
}

// ChatStream implements the react.LLM interface (streaming degradation).
func (l *FakeHILLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}
