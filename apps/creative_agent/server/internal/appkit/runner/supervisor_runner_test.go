package appkitrunner_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appkitfake "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/fake"
	appkitllm "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/llm"
	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestRunner_EmitsFullFrameSequence verifies Run emits message-start,
// text-delta, then message-complete — the existing contract.
func TestRunner_EmitsFullFrameSequence(t *testing.T) {
	r := appkitrunner.NewRunner()
	var frames []string
	_, err := r.Run(context.Background(), "你好", "msg-seq", func(f appkitrunner.SSEFrame) {
		frames = append(frames, f.Type)
	})
	testx.NoError(t, err)
	testx.Equal(t, "message-start", frames[0])
	testx.Contains(t, frames, "text-delta")
	testx.Equal(t, "message-complete", frames[len(frames)-1])
}

// TestRunner_AddFrameSuffix verifies the optional suffix callback is invoked
// after ReAct completes but before message-complete.
func TestRunner_AddFrameSuffix(t *testing.T) {
	r := appkitrunner.NewRunner()
	var order []string
	r.AddFrameSuffix(func() { order = append(order, "suffix") })

	_, err := r.Run(context.Background(), "你好", "msg-suffix", func(f appkitrunner.SSEFrame) {
		if f.Type == "message-complete" {
			order = append(order, "complete")
		}
	})
	testx.NoError(t, err)
	testx.Equal(t, []string{"suffix", "complete"}, order)
}

// TestRunner_WithoutSuffix verifies default runner behavior is unchanged.
func TestRunner_WithoutSuffix(t *testing.T) {
	r := appkitrunner.NewRunner()
	var frames []string
	_, err := r.Run(context.Background(), "hi", "msg-nosuffix", func(f appkitrunner.SSEFrame) {
		frames = append(frames, f.Type)
	})
	testx.NoError(t, err)
	testx.Contains(t, frames, "message-start")
	testx.Contains(t, frames, "message-complete")
}

// TestRunner_HILInterrupt 验证 Runner 注入 HIL strategy 后，含触发词的 query 触发 Interrupt，
// 并产出 interrupt 帧（而非普通 text-delta）。
func TestRunner_HILInterrupt(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(&appkitfake.FakeHILLLM{}, appkitfake.FakeHILStrategy{}, &appkitfake.FakeHooks{})

	var types []string
	_, err := r.Run(context.Background(), "生成一个海报脚本（中途需要确认）", "msg-hil", func(frame appkitrunner.SSEFrame) {
		types = append(types, frame.Type)
	})
	testx.NoError(t, err)
	testx.Contains(t, types, "interrupt", "含触发词应产出 interrupt 帧")
	testx.Contains(t, types, "message-complete")
	testx.NotContains(t, types, "text-delta", "中断时不应拆 text-delta")
}

// TestRunner_Resume 验证 RunResume 用 checkpoint 预置后从断点恢复（Interrupted=true → Terminal）。
// 强化点：断言 Terminal friendly_reply 内容，证明 cp.Messages/cp.Step 预载真实生效——
// 若预载被删除，strategy 会走首次 Interrupt 分支，内容不同（"我需要你确认…"），测试即失败。
func TestRunner_Resume(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(&appkitfake.FakeHILLLM{}, appkitfake.FakeHILStrategy{}, &appkitfake.FakeHooks{})

	cp := &react.Checkpoint{
		Messages: []react.Message{{Role: "user", Content: "继续"}},
		Step:     1,
		Query:    "原 query",
	}

	var types []string
	content, err := r.RunResume(context.Background(), "继续", "msg-resume", func(frame appkitrunner.SSEFrame) {
		types = append(types, frame.Type)
	}, cp)
	testx.NoError(t, err)
	testx.Equal(t, "好的，继续完成你的任务。", content, "resume 应产出 strategy 的 Terminal friendly_reply")
	testx.Contains(t, types, "text-delta")
	testx.Contains(t, types, "message-complete")
	testx.NotContains(t, types, "interrupt", "resume 轮不应再 Interrupt")
}

// TestRunner_HILInterrupt_Callback 验证 Interrupt 分支触发 onInterrupt 回调，
// 且回调携带引擎保存的完整 checkpoint（Messages 非空、Step ≥ 1），调用方可据此持久化。
func TestRunner_HILInterrupt_Callback(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(&appkitfake.FakeHILLLM{}, appkitfake.FakeHILStrategy{}, &appkitfake.FakeHooks{})
	var gotCp *react.Checkpoint
	r.SetOnInterrupt(func(cp *react.Checkpoint) { gotCp = cp })

	_, err := r.Run(context.Background(), "生成一个海报脚本（中途需要确认）", "msg-hil-cb", func(appkitrunner.SSEFrame) {})
	testx.NoError(t, err)
	testx.NotNil(t, gotCp, "Interrupt 应触发 onInterrupt 回调")
	testx.NotEmpty(t, gotCp.Messages)
	testx.GreaterOrEqual(t, gotCp.Step, 1)
}

// TestRunner_DefaultAssistantPrepare 验证未注入 SetPrepare 时，
// Run 走 DefaultAssistantPrepare 兜底 prompt（通用 assistant，无业务语义）。
func TestRunner_DefaultAssistantPrepare(t *testing.T) {
	prepare := appkitrunner.DefaultAssistantPrepare("你好")
	testx.Len(t, prepare, 2, "默认 prompt 应含 system + user 两条")
	testx.Equal(t, "system", prepare[0].Role)
	testx.Contains(t, prepare[0].Content, "AI 助手", "默认 system prompt 是通用 assistant")
	testx.Equal(t, "user", prepare[1].Role)
	testx.Equal(t, "你好", prepare[1].Content)
}

// TestRunner_SetPrepare 验证 SetPrepare 注入的 prompt 生效（覆盖默认 assistant）。
func TestRunner_SetPrepare(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(&appkitfake.FakeLLM{}, &appkitfake.FakeStrategy{}, &appkitfake.FakeHooks{})

	var gotQ string
	r.SetPrepare(func(q string) []react.Message {
		gotQ = q
		return []react.Message{
			{Role: "system", Content: "你是业务管家。"},
			{Role: "user", Content: q},
		}
	})

	_, err := r.Run(context.Background(), "帮我做个混剪", "msg-prep", func(appkitrunner.SSEFrame) {})
	testx.NoError(t, err)
	testx.Equal(t, "帮我做个混剪", gotQ, "注入的 prompt 应收到用户 query")
}

// planOutputStrategy 是本地管家策略桩：模拟 ManagerStrategy 的约束链行为——
// 对 generate_response 工具调用直接校验通过并 Terminal，把 PlanOutput 完整 JSON
// 透传到 Metadata.plan_output（引擎转入 AgentResult.Ext）。不依赖 manager 业务包，
// 仅用于覆盖 RunWithOutput 的 onOutput 回调链路（appkit/runner 自身应保持零业务依赖）。
type planOutputStrategy struct{}

func (planOutputStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return nil
}

func (planOutputStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse
	if len(resp.ToolCalls) == 0 {
		return &react.Decision{Action: react.ActionRetry}
	}
	// 取首个 generate_response 调用的 arguments，按其内容构造 plan_output
	args := resp.ToolCalls[0].Args
	var gen struct {
		FriendlyReply  string `json:"friendly_reply"`
		TaskType       string `json:"task_type"`
		AgentsPipeline string `json:"agents_pipeline"`
		TaskArguments  string `json:"task_arguments"`
	}
	_ = json.Unmarshal([]byte(args), &gen)
	if gen.TaskType != "new_agents_plan" {
		return &react.Decision{Action: react.ActionTerminal, Metadata: map[string]any{"friendly_reply": gen.FriendlyReply}}
	}
	planJSON, _ := json.Marshal(map[string]any{
		"task_type":       gen.TaskType,
		"friendly_reply":  gen.FriendlyReply,
		"agents_pipeline": gen.AgentsPipeline,
		"plan_steps": map[string]any{
			"agents": []map[string]any{{"agent_id": "gold_scriptwriter", "input": "生成海报"}},
		},
	})
	return &react.Decision{
		Action: react.ActionTerminal,
		Metadata: map[string]any{
			"friendly_reply": gen.FriendlyReply,
			"plan_output":    string(planJSON),
		},
	}
}

// streamingFakeLLM 是流式 Fake LLM：ChatStream 逐 chunk 回调 content，返回完整响应。
// 用于验证 runner EnableStreaming 后逐 chunk 实时出 text-delta 帧。
type streamingFakeLLM struct{}

func (streamingFakeLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	return &react.LLMResponse{Content: "流式回答"}, nil
}

func (streamingFakeLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	if cb != nil {
		for _, c := range []string{"流", "式", "回", "答"} {
			_ = cb(ctx, &react.StreamedChatResponsePayload{
				Choices: []react.StreamedChatChoice{{Delta: react.StreamedChatDelta{Content: c}}},
			}, false)
		}
		_ = cb(ctx, &react.StreamedChatResponsePayload{
			Choices: []react.StreamedChatChoice{{Delta: react.StreamedChatDelta{Content: "流式回答"}}},
		}, true)
	}
	return &react.LLMResponse{Content: "流式回答"}, nil
}

// streamingStrategy 直接 Terminal（单轮，不调工具）。
type streamingStrategy struct{}

func (streamingStrategy) GetTools(c *react.AgentContext) []react.FunctionDef { return nil }

func (streamingStrategy) Decide(c *react.AgentContext) *react.Decision {
	return &react.Decision{Action: react.ActionTerminal, Metadata: map[string]any{"friendly_reply": "流式回答"}}
}

// TestRunner_Streaming_EmitsTextDeltaPerChunk 验证 EnableStreaming 开启后：
// 流式 chunk 逐个实时转 text-delta 帧（4 帧），而非终态后一次性切块。
func TestRunner_Streaming_EmitsTextDeltaPerChunk(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(streamingFakeLLM{}, streamingStrategy{}, &appkitfake.FakeHooks{})
	r.EnableStreaming()

	var frames []appkitrunner.SSEFrame
	content, err := r.Run(context.Background(), "hi", "msg-stream", func(f appkitrunner.SSEFrame) {
		frames = append(frames, f)
	})
	testx.NoError(t, err)
	testx.Equal(t, "流式回答", content)

	// 收集 text-delta 帧内容
	var deltas []string
	for _, f := range frames {
		if f.Type == "text-delta" {
			deltas = append(deltas, f.Content)
		}
	}
	// 流式：4 个 chunk 各自成帧，不合并成 10-rune 切块
	testx.Equal(t, []string{"流", "式", "回", "答"}, deltas, "流式 chunk 应逐个成 text-delta 帧")
}

// TestRunner_Streaming_Off_DefaultBehavior 验证 EnableStreaming 默认关闭时：
// 走终态 10-rune 切块（行为与改造前一致）。
func TestRunner_Streaming_Off_DefaultBehavior(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(streamingFakeLLM{}, streamingStrategy{}, &appkitfake.FakeHooks{})

	var frames []appkitrunner.SSEFrame
	_, err := r.Run(context.Background(), "hi", "msg-nostream", func(f appkitrunner.SSEFrame) {
		frames = append(frames, f)
	})
	testx.NoError(t, err)

	var deltas []string
	for _, f := range frames {
		if f.Type == "text-delta" {
			deltas = append(deltas, f.Content)
		}
	}
	// "流式回答" 4 个 rune → 10-rune 切块 → 单帧 "流式回答"
	testx.Equal(t, []string{"流式回答"}, deltas, "非流式应保留终态切块")
}

// TestRunner_Streaming_DefaultOff_ExistingTests 验证默认 runner 不带 streaming 时
// 原测试桩（fakeLLM）仍走 Chat（FakeLLM 降级 ChatStream 转发 Chat），帧序列不变。
func TestRunner_Streaming_DefaultOff_ExistingTests(t *testing.T) {
	r := appkitrunner.NewRunner()
	var types []string
	_, err := r.Run(context.Background(), "你好", "msg-default", func(f appkitrunner.SSEFrame) {
		types = append(types, f.Type)
	})
	testx.NoError(t, err)
	testx.Equal(t, "message-start", types[0])
	testx.Contains(t, types, "text-delta")
	testx.Equal(t, "message-complete", types[len(types)-1])
}

// TestRunner_Streaming_RunWithOutput_OnOutputStillFires 验证流式开启 + RunWithOutput：
// 流式 tool_calls 后 onOutput 仍触发（终态结构化输出不受流式旁路影响）。
// mock server 返回 SSE 流式 tool_call（id/name 首包 + arguments 分片）。
func TestRunner_Streaming_RunWithOutput_OnOutputStillFires(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, line := range []string{
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"generate_response","arguments":"{\"thought\":\"t\",\"friendly_reply\":\"已派发\",\"task_type\":\"new_agents_plan\",\"agents_pipeline\":\"mix_video\","}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"task_arguments\":\"{\\\"agents\\\":[{\\\"agent_id\\\":\\\"gold_scriptwriter\\\",\\\"input\\\":\\\"生成海报\\\"}]}\""}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}`,
			`data: [DONE]`,
		} {
			_, _ = w.Write([]byte(line + "\n"))
			fl.Flush()
		}
	}))
	defer srv.Close()
	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m", ToolChoice: appkitllm.ToolChoiceRequired})
	testx.NoError(t, err)

	r := appkitrunner.NewRunner()
	r.SetProviders(llm, planOutputStrategy{}, &appkitfake.FakeHooks{})
	r.EnableStreaming()

	var gotPlan *appkitrunner.PlanOutput
	var frames []appkitrunner.SSEFrame
	_, err = r.RunWithOutput(context.Background(), "生成海报", "msg-mgr-stream", func(f appkitrunner.SSEFrame) {
		frames = append(frames, f)
	}, func(plan *appkitrunner.PlanOutput) {
		gotPlan = plan
	})
	testx.NoError(t, err)
	testx.NotNil(t, gotPlan, "流式开启时 onOutput 仍应触发")
	testx.Len(t, gotPlan.PlanSteps.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", gotPlan.PlanSteps.Agents[0].AgentID)

	// 终态 friendly_reply 帧仍发（new_agents_plan 分支）
	var hasFriendly bool
	for _, f := range frames {
		if f.Type == "text-delta" && f.Content == "已派发" {
			hasFriendly = true
		}
	}
	testx.True(t, hasFriendly, "new_agents_plan 的 friendly_reply 帧应发送")
}

// TestRunner_Streaming_RunResume 验证流式开启 + RunResume 帧序列正常。
func TestRunner_Streaming_RunResume(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(streamingFakeLLM{}, streamingStrategy{}, &appkitfake.FakeHooks{})
	r.EnableStreaming()

	cp := &react.Checkpoint{
		Messages: []react.Message{{Role: "user", Content: "继续"}},
		Step:     1,
		Query:    "原 query",
	}

	var types []string
	content, err := r.RunResume(context.Background(), "继续", "msg-resume-stream", func(f appkitrunner.SSEFrame) {
		types = append(types, f.Type)
	}, cp)
	testx.NoError(t, err)
	testx.Equal(t, "流式回答", content)
	testx.Contains(t, types, "text-delta")
	testx.Contains(t, types, "message-complete")
}

// TestRunner_RunWithOutput_ManagerStrategy 验证：注入 mock LLM（返回 generate_response
// 协议调用）+ 管家策略桩后，RunWithOutput 解析出 plan 并触发 onOutput 回调。
func TestRunner_RunWithOutput_ManagerStrategy(t *testing.T) {
	// mock LLM：返回合法 new_agents_plan 的 generate_response 调用（§8.4.4 顶层 agents_pipeline 协议）
	mockResp := `{"choices":[{"message":{"content":"","role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"generate_response","arguments":"{\"thought\":\"t\",\"friendly_reply\":\"已派发\",\"task_type\":\"new_agents_plan\",\"agents_pipeline\":\"mix_video\",\"task_arguments\":\"{\\\"agents\\\":[{\\\"agent_id\\\":\\\"gold_scriptwriter\\\",\\\"input\\\":\\\"生成海报\\\"}]}\"}"}}]}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockResp))
	}))
	defer srv.Close()
	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m", ToolChoice: appkitllm.ToolChoiceRequired})
	testx.NoError(t, err)

	r := appkitrunner.NewRunner()
	r.SetProviders(llm, planOutputStrategy{}, &appkitfake.FakeHooks{})

	var gotPlan *appkitrunner.PlanOutput
	_, err = r.RunWithOutput(context.Background(), "生成海报", "msg-mgr", func(appkitrunner.SSEFrame) {}, func(plan *appkitrunner.PlanOutput) {
		gotPlan = plan
	})
	testx.NoError(t, err)
	testx.NotNil(t, gotPlan, "应触发 onOutput 回调")
	testx.Len(t, gotPlan.PlanSteps.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", gotPlan.PlanSteps.Agents[0].AgentID)
}

// toolCallOnlyStreamingLLM 模拟强制 tool_choice=required 的流式场景：
// ChatStream 不回调任何 content（LLM 只返回 generate_response 工具调用），
// 但完整响应含 friendly_reply。用于回归测试：流式开启但无 content 帧时，
// 终态必须补发文本，否则前端零内容。
type toolCallOnlyStreamingLLM struct{}

func (toolCallOnlyStreamingLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	return &react.LLMResponse{
		Content: `{"task_type":"only_reply","friendly_reply":"功能列表：1.混剪成片 2.文章生成"}`,
	}, nil
}

func (toolCallOnlyStreamingLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	// 不回调任何 content（模拟 tool_choice=required 返回 tool_call，content 空）
	_ = cb(ctx, &react.StreamedChatResponsePayload{
		Choices: []react.StreamedChatChoice{{Delta: react.StreamedChatDelta{Role: "assistant", Content: ""}}},
	}, true)
	return toolCallOnlyStreamingLLM{}.Chat(ctx, messages, tools)
}

// toolCallOnlyStrategy 把终态 JSON 里的 friendly_reply 透传给 Metadata（模拟 ManagerStrategy）。
type toolCallOnlyStrategy struct{}

func (toolCallOnlyStrategy) GetTools(c *react.AgentContext) []react.FunctionDef { return nil }

func (toolCallOnlyStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse
	var out struct {
		FriendlyReply string `json:"friendly_reply"`
	}
	_ = json.Unmarshal([]byte(resp.Content), &out)
	return &react.Decision{Action: react.ActionTerminal, Metadata: map[string]any{"friendly_reply": out.FriendlyReply}}
}

// TestRunner_Streaming_NoContent_FallbackToTerminalText 回归测试：
// 流式开启但 LLM 不回调 content（强制 tool_choice=required 场景）时，
// 终态必须补发 friendly_reply 文本——否则前端收到零 text-delta 卡住。
// 修复前：普通回复分支被 `else if !r.streaming` 跳过，零帧。
func TestRunner_Streaming_NoContent_FallbackToTerminalText(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(toolCallOnlyStreamingLLM{}, toolCallOnlyStrategy{}, &appkitfake.FakeHooks{})
	r.EnableStreaming()

	var frames []appkitrunner.SSEFrame
	content, err := r.Run(context.Background(), "你有哪些功能", "msg-nocontent", func(f appkitrunner.SSEFrame) {
		frames = append(frames, f)
	})
	testx.NoError(t, err)
	// strategy 把 friendly_reply 从终态 JSON 透传到 result.Content
	testx.Equal(t, "功能列表：1.混剪成片 2.文章生成", content)

	var deltas []string
	for _, f := range frames {
		if f.Type == "text-delta" {
			deltas = append(deltas, f.Content)
		}
	}
	// 流式无 content 帧 → 终态必须补发 friendly_reply（整体拼接后含该文本）
	joined := strings.Join(deltas, "")
	testx.Contains(t, joined, "功能列表：1.混剪成片 2.文章生成",
		"流式无 content 时终态应补发 friendly_reply，不能零帧")
}

// hotCardStrategy 直接 Terminal + 透传 hot_card（模拟 ManagerStrategy 直出热点卡）。
type hotCardStrategy struct{}

func (hotCardStrategy) GetTools(c *react.AgentContext) []react.FunctionDef { return nil }

func (hotCardStrategy) Decide(c *react.AgentContext) *react.Decision {
	return &react.Decision{
		Action: react.ActionTerminal,
		Metadata: map[string]any{
			"friendly_reply": "",
			"hot_card": &appkitrunner.HotCardData{
				CardType:    "热点及选题卡",
				HotspotData: "## 城市夜景热点",
				TitleData: []ideaking.TitleItem{
					{Title: "选题一", Content: "描述一", Eid: "eid-1"},
					{Title: "选题二", Content: "描述二", Eid: "eid-2"},
				},
			},
		},
	}
}

// TestRunner_HotCard_EmitsUIBlockFrames 验证 RunWithOutput 产出热点卡帧序：
// message-start → text-delta(hotspot) → ui-block-start → ui-block-complete → message-complete。
// ui-block-start 的 promptItems 带 eid（用户点卡回传定位热点）。
func TestRunner_HotCard_EmitsUIBlockFrames(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(streamingFakeLLM{}, hotCardStrategy{}, &appkitfake.FakeHooks{})

	var frames []appkitrunner.SSEFrame
	_, err := r.Run(context.Background(), "有什么热点", "msg-hot", func(f appkitrunner.SSEFrame) {
		frames = append(frames, f)
	})
	testx.NoError(t, err)

	var types []string
	for _, f := range frames {
		types = append(types, f.Type)
	}
	testx.Equal(t, []string{"message-start", "text-delta", "ui-block-start", "ui-block-complete", "message-complete"}, types,
		"热点卡应产出 message-start → text-delta(hotspot) → ui-block-start → ui-block-complete → message-complete")

	// ui-block-start 帧的 promptItems 带 eid
	var start *appkitrunner.SSEFrame
	for i := range frames {
		if frames[i].Type == "ui-block-start" {
			start = &frames[i]
			break
		}
	}
	testx.NotNil(t, start, "应有 ui-block-start 帧")
	testx.NotNil(t, start.UIData, "ui-block-start 应有 uiData")
	uiData, ok := start.UIData.(appkitrunner.PromptOptionGroupUIData)
	testx.True(t, ok, "uiData 应为 PromptOptionGroupUIData")
	testx.Len(t, uiData.PromptItems, 2)
	testx.Equal(t, "选题一", uiData.PromptItems[0].Title)
	testx.Equal(t, "eid-1", uiData.PromptItems[0].Eid)
	testx.Equal(t, "eid-2", uiData.PromptItems[1].Eid)
	testx.Equal(t, -1, uiData.SelectedIndex)
	testx.NotEmpty(t, start.BlockID, "ui-block-start 应有 blockId")
}

// creationCardStrategy 直接 Terminal + 透传 image-text 创作功能卡（模拟 push_card）。
type creationCardStrategy struct{}

func (creationCardStrategy) GetTools(c *react.AgentContext) []react.FunctionDef { return nil }

func (creationCardStrategy) Decide(c *react.AgentContext) *react.Decision {
	return &react.Decision{
		Action: react.ActionTerminal,
		Metadata: map[string]any{
			"friendly_reply": "请选择创作功能",
			"hot_card": &appkitrunner.HotCardData{
				CardType: "创作功能选择卡",
				Layout:   "image-text",
				Label:    "老板想用哪个创作功能实现这个选题呢？",
				PromptItems: []appkitrunner.PromptItem{
					{Title: "AI成片", Text: "海量素材一键智能成片", Pipeline: "mix_video"},
					{Title: "AI笔记图", Text: "一句话做爆款图文笔记", Pipeline: "article"},
				},
			},
		},
	}
}

// TestRunner_CreationCard_ImageTextFrames 验证创作功能卡产出 image-text 布局的
// ui-block-start 帧（promptItems 带 pipeline）。
func TestRunner_CreationCard_ImageTextFrames(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(streamingFakeLLM{}, creationCardStrategy{}, &appkitfake.FakeHooks{})

	var frames []appkitrunner.SSEFrame
	_, err := r.Run(context.Background(), "根据xxx开始创作", "msg-creation", func(f appkitrunner.SSEFrame) {
		frames = append(frames, f)
	})
	testx.NoError(t, err)

	var types []string
	for _, f := range frames {
		types = append(types, f.Type)
	}
	testx.Contains(t, types, "ui-block-start", "创作功能卡应产出 ui-block-start")

	var start *appkitrunner.SSEFrame
	for i := range frames {
		if frames[i].Type == "ui-block-start" {
			start = &frames[i]
			break
		}
	}
	testx.NotNil(t, start, "应有 ui-block-start 帧")
	uiData, ok := start.UIData.(appkitrunner.PromptOptionGroupUIData)
	testx.True(t, ok)
	testx.Equal(t, "image-text", uiData.Layout, "创作功能卡应是 image-text 布局")
	testx.Len(t, uiData.PromptItems, 2)
	testx.Equal(t, "AI成片", uiData.PromptItems[0].Title)
}
