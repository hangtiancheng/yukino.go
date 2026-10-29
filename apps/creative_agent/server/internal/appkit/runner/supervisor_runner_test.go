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

// TestRunner_HILInterrupt verifies that after injecting the HIL strategy, a query containing the trigger word fires Interrupt
// and produces an interrupt frame (instead of ordinary text-deltas).
func TestRunner_HILInterrupt(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(&appkitfake.FakeHILLLM{}, appkitfake.FakeHILStrategy{}, &appkitfake.FakeHooks{})

	var types []string
	_, err := r.Run(context.Background(), "生成一个海报脚本（中途需要确认）", "msg-hil", func(frame appkitrunner.SSEFrame) {
		types = append(types, frame.Type)
	})
	testx.NoError(t, err)
	testx.Contains(t, types, "interrupt", "a trigger word should produce an interrupt frame")
	testx.Contains(t, types, "message-complete")
	testx.NotContains(t, types, "text-delta", "no text-delta should be split out on interrupt")
}

// TestRunner_Resume verifies that RunResume pre-loads the checkpoint and resumes from the breakpoint (Interrupted=true → Terminal).
// Strengthened: asserting the Terminal friendly_reply content proves the cp.Messages/cp.Step preload really takes effect —
// if the preload were removed, the strategy would take the first-run Interrupt branch with different content ("我需要你确认…") and the test would fail.
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
	testx.Equal(t, "好的，继续完成你的任务。", content, "resume should produce the strategy's Terminal friendly_reply")
	testx.Contains(t, types, "text-delta")
	testx.Contains(t, types, "message-complete")
	testx.NotContains(t, types, "interrupt", "the resume round should not Interrupt again")
}

// TestRunner_HILInterrupt_Callback verifies that the Interrupt branch triggers the onInterrupt callback,
// and the callback carries the full checkpoint saved by the engine (non-empty Messages, Step ≥ 1) for the caller to persist.
func TestRunner_HILInterrupt_Callback(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(&appkitfake.FakeHILLLM{}, appkitfake.FakeHILStrategy{}, &appkitfake.FakeHooks{})
	var gotCp *react.Checkpoint
	r.SetOnInterrupt(func(cp *react.Checkpoint) { gotCp = cp })

	_, err := r.Run(context.Background(), "生成一个海报脚本（中途需要确认）", "msg-hil-cb", func(appkitrunner.SSEFrame) {})
	testx.NoError(t, err)
	testx.NotNil(t, gotCp, "Interrupt should trigger the onInterrupt callback")
	testx.NotEmpty(t, gotCp.Messages)
	testx.GreaterOrEqual(t, gotCp.Step, 1)
}

// TestRunner_DefaultAssistantPrepare verifies that without SetPrepare injection,
// Run falls back to the DefaultAssistantPrepare prompt (generic assistant, no business semantics).
func TestRunner_DefaultAssistantPrepare(t *testing.T) {
	prepare := appkitrunner.DefaultAssistantPrepare("你好")
	testx.Len(t, prepare, 2, "the default prompt should contain system + user messages")
	testx.Equal(t, "system", prepare[0].Role)
	testx.Contains(t, prepare[0].Content, "AI 助手", "the default system prompt is the generic assistant")
	testx.Equal(t, "user", prepare[1].Role)
	testx.Equal(t, "你好", prepare[1].Content)
}

// TestRunner_SetPrepare verifies that the prompt injected via SetPrepare takes effect (overriding the default assistant).
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
	testx.Equal(t, "帮我做个混剪", gotQ, "the injected prompt should receive the user query")
}

// planOutputStrategy is a local manager-strategy stub: it simulates ManagerStrategy's constraint-chain behavior —
// a generate_response tool call passes validation directly and terminates, with the full PlanOutput JSON
// passed through to Metadata.plan_output (the engine moves it into AgentResult.Ext). It does not depend on the manager business package;
// it only covers RunWithOutput's onOutput callback chain (appkit/runner itself must stay free of business dependencies).
type planOutputStrategy struct{}

func (planOutputStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return nil
}

func (planOutputStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse
	if len(resp.ToolCalls) == 0 {
		return &react.Decision{Action: react.ActionRetry}
	}
	// Take the arguments of the first generate_response call and build plan_output from them
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

// streamingFakeLLM is a streaming fake LLM: ChatStream calls back content chunk by chunk and returns the complete response.
// Used to verify that after EnableStreaming the runner emits text-delta frames per chunk in realtime.
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

// streamingStrategy terminates directly (single round, no tool calls).
type streamingStrategy struct{}

func (streamingStrategy) GetTools(c *react.AgentContext) []react.FunctionDef { return nil }

func (streamingStrategy) Decide(c *react.AgentContext) *react.Decision {
	return &react.Decision{Action: react.ActionTerminal, Metadata: map[string]any{"friendly_reply": "流式回答"}}
}

// TestRunner_Streaming_EmitsTextDeltaPerChunk verifies that with EnableStreaming on:
// streaming chunks are converted into text-delta frames one by one in realtime (4 frames), instead of one-shot chunking after the terminal state.
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

	// Collect text-delta frame contents
	var deltas []string
	for _, f := range frames {
		if f.Type == "text-delta" {
			deltas = append(deltas, f.Content)
		}
	}
	// Streaming: the 4 chunks each become a frame, not merged into 10-rune chunks
	testx.Equal(t, []string{"流", "式", "回", "答"}, deltas, "streaming chunks should each become a text-delta frame")
}

// TestRunner_Streaming_Off_DefaultBehavior verifies that with EnableStreaming off by default:
// terminal 10-rune chunking is used (behavior identical to before the change).
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
	// "流式回答" is 4 runes → 10-rune chunking → a single frame "流式回答"
	testx.Equal(t, []string{"流式回答"}, deltas, "non-streaming should keep terminal chunking")
}

// TestRunner_Streaming_DefaultOff_ExistingTests verifies that a default runner without streaming
// still routes the original test stub (fakeLLM) through Chat (FakeLLM degrades ChatStream to Chat), keeping the frame sequence unchanged.
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

// TestRunner_Streaming_RunWithOutput_OnOutputStillFires verifies streaming on + RunWithOutput:
// onOutput still fires after streamed tool_calls (terminal structured output is unaffected by the streaming side channel).
// The mock server returns an SSE-streamed tool_call (id/name in the first chunk + arguments fragments).
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
	testx.NotNil(t, gotPlan, "onOutput should still fire with streaming enabled")
	testx.Len(t, gotPlan.PlanSteps.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", gotPlan.PlanSteps.Agents[0].AgentID)

	// The terminal friendly_reply frame is still sent (new_agents_plan branch)
	var hasFriendly bool
	for _, f := range frames {
		if f.Type == "text-delta" && f.Content == "已派发" {
			hasFriendly = true
		}
	}
	testx.True(t, hasFriendly, "the friendly_reply frame of new_agents_plan should be sent")
}

// TestRunner_Streaming_RunResume verifies the frame sequence of streaming-on + RunResume.
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

// TestRunner_RunWithOutput_ManagerStrategy verifies: after injecting a mock LLM (returning a generate_response
// protocol call) + the manager-strategy stub, RunWithOutput parses the plan and triggers the onOutput callback.
func TestRunner_RunWithOutput_ManagerStrategy(t *testing.T) {
	// mock LLM: returns a generate_response call with a valid new_agents_plan (§8.4.4 top-level agents_pipeline protocol)
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
	testx.NotNil(t, gotPlan, "the onOutput callback should fire")
	testx.Len(t, gotPlan.PlanSteps.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", gotPlan.PlanSteps.Agents[0].AgentID)
}

// toolCallOnlyStreamingLLM simulates the forced tool_choice=required streaming scenario:
// ChatStream calls back no content (the LLM returns only the generate_response tool call),
// but the complete response contains friendly_reply. Used for regression: when streaming is on but no content frames arrive,
// the terminal text must still be emitted, otherwise the frontend gets nothing.
type toolCallOnlyStreamingLLM struct{}

func (toolCallOnlyStreamingLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	return &react.LLMResponse{
		Content: `{"task_type":"only_reply","friendly_reply":"功能列表：1.混剪成片 2.文章生成"}`,
	}, nil
}

func (toolCallOnlyStreamingLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	// No content callbacks (simulating tool_choice=required returning a tool_call with empty content)
	_ = cb(ctx, &react.StreamedChatResponsePayload{
		Choices: []react.StreamedChatChoice{{Delta: react.StreamedChatDelta{Role: "assistant", Content: ""}}},
	}, true)
	return toolCallOnlyStreamingLLM{}.Chat(ctx, messages, tools)
}

// toolCallOnlyStrategy passes the friendly_reply from the terminal JSON through to Metadata (simulating ManagerStrategy).
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

// TestRunner_Streaming_NoContent_FallbackToTerminalText regression test:
// when streaming is on but the LLM calls back no content (forced tool_choice=required scenario),
// the terminal friendly_reply text must still be emitted — otherwise the frontend receives zero text-deltas and stalls.
// Before the fix: the plain-reply branch was skipped by `else if !r.streaming`, producing zero frames.
func TestRunner_Streaming_NoContent_FallbackToTerminalText(t *testing.T) {
	r := appkitrunner.NewRunner()
	r.SetProviders(toolCallOnlyStreamingLLM{}, toolCallOnlyStrategy{}, &appkitfake.FakeHooks{})
	r.EnableStreaming()

	var frames []appkitrunner.SSEFrame
	content, err := r.Run(context.Background(), "你有哪些功能", "msg-nocontent", func(f appkitrunner.SSEFrame) {
		frames = append(frames, f)
	})
	testx.NoError(t, err)
	// The strategy passes friendly_reply from the terminal JSON through to result.Content
	testx.Equal(t, "功能列表：1.混剪成片 2.文章生成", content)

	var deltas []string
	for _, f := range frames {
		if f.Type == "text-delta" {
			deltas = append(deltas, f.Content)
		}
	}
	// No streamed content frames → the terminal friendly_reply must be emitted (the joined text contains it)
	joined := strings.Join(deltas, "")
	testx.Contains(t, joined, "功能列表：1.混剪成片 2.文章生成",
		"the terminal friendly_reply must be emitted when streaming produced no content; zero frames is not acceptable")
}

// hotCardStrategy terminates directly + passes hot_card through (simulating ManagerStrategy emitting a hot-topic card).
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

// TestRunner_HotCard_EmitsUIBlockFrames verifies that RunWithOutput produces the hot-card frame sequence:
// message-start → text-delta(hotspot) → ui-block-start → ui-block-complete → message-complete.
// ui-block-start's promptItems carry eid (echoed on card click to locate the hotspot).
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
		"the hot card should produce message-start → text-delta(hotspot) → ui-block-start → ui-block-complete → message-complete")

	// The ui-block-start frame's promptItems carry eid
	var start *appkitrunner.SSEFrame
	for i := range frames {
		if frames[i].Type == "ui-block-start" {
			start = &frames[i]
			break
		}
	}
	testx.NotNil(t, start, "a ui-block-start frame should exist")
	testx.NotNil(t, start.UIData, "ui-block-start should carry uiData")
	uiData, ok := start.UIData.(appkitrunner.PromptOptionGroupUIData)
	testx.True(t, ok, "uiData should be PromptOptionGroupUIData")
	testx.Len(t, uiData.PromptItems, 2)
	testx.Equal(t, "选题一", uiData.PromptItems[0].Title)
	testx.Equal(t, "eid-1", uiData.PromptItems[0].Eid)
	testx.Equal(t, "eid-2", uiData.PromptItems[1].Eid)
	testx.Equal(t, -1, uiData.SelectedIndex)
	testx.NotEmpty(t, start.BlockID, "ui-block-start should carry a blockId")
}

// creationCardStrategy terminates directly + passes through the image-text creation-feature card (simulating push_card).
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

// TestRunner_CreationCard_ImageTextFrames verifies that the creation-feature card produces a ui-block-start
// frame with the image-text layout (promptItems carrying pipeline).
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
	testx.Contains(t, types, "ui-block-start", "the creation-feature card should produce ui-block-start")

	var start *appkitrunner.SSEFrame
	for i := range frames {
		if frames[i].Type == "ui-block-start" {
			start = &frames[i]
			break
		}
	}
	testx.NotNil(t, start, "a ui-block-start frame should exist")
	uiData, ok := start.UIData.(appkitrunner.PromptOptionGroupUIData)
	testx.True(t, ok)
	testx.Equal(t, "image-text", uiData.Layout, "the creation-feature card should use the image-text layout")
	testx.Len(t, uiData.PromptItems, 2)
	testx.Equal(t, "AI成片", uiData.PromptItems[0].Title)
}
