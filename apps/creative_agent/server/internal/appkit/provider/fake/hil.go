package appkitfake

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// FakeHILLLM 是 HIL（Human-in-the-Loop）断点链路的 LLM 实现（Phase 4）。
//
// 行为：无 tool 时返回含"确认"触发的文本，配合 FakeHILStrategy——
//   - 首次（query 含触发词）→ ActionInterrupt（推卡片等用户确认）
//   - resume 恢复轮次（Runtime.Interrupted=true）→ 直接 Terminal
//
// 当前为查表模拟，可后续替换为真实 LLM（OpenAI 兼容 HTTP 适配器见 llm_http.go）。
type FakeHILLLM struct {
	turn int
}

func (l *FakeHILLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	_ = ctx
	l.turn++
	return &react.LLMResponse{Content: "hil_keyword 需要确认"}, nil
}

// ChatStream 实现 react.LLM 接口（流式降级）。
func (l *FakeHILLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamResponseDegraded(ctx, resp, err, cb)
}
