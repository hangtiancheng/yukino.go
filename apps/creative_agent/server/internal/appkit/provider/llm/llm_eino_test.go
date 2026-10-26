package appkitllm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	appkitllm "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/llm"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// emulator 是 OpenAI 兼容协议的 httptest 桩：记录最后一次请求体，按 respond 回调应答。
type emulator struct {
	mu       sync.Mutex
	lastBody map[string]any
	lastAuth string
	lastPath string
}

func (e *emulator) body() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastBody
}

func (e *emulator) auth() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastAuth
}

func (e *emulator) path() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastPath
}

func newEmulator(t *testing.T, respond func(w http.ResponseWriter)) (*httptest.Server, *emulator) {
	t.Helper()
	em := &emulator{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		em.mu.Lock()
		em.lastBody = parsed
		em.lastAuth = r.Header.Get("Authorization")
		em.lastPath = r.URL.Path
		em.mu.Unlock()
		respond(w)
	}))
	t.Cleanup(srv.Close)
	return srv, em
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

// writeSSE 按 OpenAI 流式协议输出 data: 行（含 [DONE] 终止符）。
func writeSSE(w http.ResponseWriter, lines ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	for _, line := range lines {
		_, _ = w.Write([]byte("data: " + line + "\n\n"))
		if fl != nil {
			fl.Flush()
		}
	}
}

// ---------- Chat（非流式） ----------

func TestEinoLLM_Chat(t *testing.T) {
	srv, em := newEmulator(t, func(w http.ResponseWriter) {
		writeJSON(w, `{"id":"c1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}]}`)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Model:   "test-model",
	})
	testx.NoError(t, err)
	testx.NotNil(t, llm)

	resp, err := llm.Chat(context.Background(), []react.Message{{Role: "user", Content: "hi"}}, nil)
	testx.NoError(t, err)
	testx.Equal(t, "你好", resp.Content)
	testx.Empty(t, resp.ToolCalls)

	testx.Equal(t, "Bearer test-key", em.auth())
	testx.Equal(t, "/chat/completions", em.path())
	body := em.body()
	testx.Equal(t, "test-model", body["model"])
	_, hasToolChoice := body["tool_choice"]
	testx.False(t, hasToolChoice, "ToolChoice 为空时请求体不应含 tool_choice 字段")
}

func TestEinoLLM_ChatWithToolCalls(t *testing.T) {
	srv, _ := newEmulator(t, func(w http.ResponseWriter) {
		writeJSON(w, `{"id":"c1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"query\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	testx.NoError(t, err)
	resp, err := llm.Chat(context.Background(), nil, nil)
	testx.NoError(t, err)
	testx.Len(t, resp.ToolCalls, 1)
	testx.Equal(t, "call_1", resp.ToolCalls[0].ID)
	testx.Equal(t, "search", resp.ToolCalls[0].Name)
	testx.Equal(t, `{"query":"x"}`, resp.ToolCalls[0].Args)
}

func TestEinoLLM_HTTPError(t *testing.T) {
	srv, _ := newEmulator(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	testx.NoError(t, err)
	_, err = llm.Chat(context.Background(), nil, nil)
	testx.Error(t, err)
	testx.Contains(t, err.Error(), "500")
}

func TestEinoLLM_ToolChoiceRequired_SingleTool(t *testing.T) {
	srv, em := newEmulator(t, func(w http.ResponseWriter) {
		writeJSON(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{
		BaseURL:    srv.URL,
		APIKey:     "k",
		Model:      "m",
		ToolChoice: appkitllm.ToolChoiceRequired,
	})
	testx.NoError(t, err)
	_, err = llm.Chat(context.Background(), []react.Message{{Role: "user", Content: "hi"}}, []react.FunctionDef{
		{Name: "search", Description: "搜索", Parameters: react.ParameterSpec{Type: "object"}},
	})
	testx.NoError(t, err)

	body := em.body()
	// eino 对"强制 + 单工具"序列化为对象形式 tool_choice（等价于 required）。
	tc, ok := body["tool_choice"].(map[string]any)
	testx.True(t, ok, "单工具强制调用时 tool_choice 应为对象形式，got %v", body["tool_choice"])
	testx.Equal(t, "function", tc["type"])
	fn, _ := tc["function"].(map[string]any)
	testx.Equal(t, "search", fn["name"])
	// 强制 Function Calling 时必须关闭 thinking 模式（否则 400）：
	// DeepSeek 约定 thinking=disabled，Qwen 约定 enable_thinking=false，两者同时透传。
	th, _ := body["thinking"].(map[string]any)
	testx.NotNil(t, th, "请求体应含 thinking 扩展字段")
	testx.Equal(t, "disabled", th["type"], "请求体应含 thinking=disabled")
	testx.Equal(t, false, body["enable_thinking"], "请求体应含 enable_thinking=false")
}

func TestEinoLLM_ToolChoiceRequired_MultiTools(t *testing.T) {
	srv, em := newEmulator(t, func(w http.ResponseWriter) {
		writeJSON(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{
		BaseURL:    srv.URL,
		APIKey:     "k",
		Model:      "m",
		ToolChoice: appkitllm.ToolChoiceRequired,
	})
	testx.NoError(t, err)
	_, err = llm.Chat(context.Background(), []react.Message{{Role: "user", Content: "hi"}}, []react.FunctionDef{
		{Name: "search", Parameters: react.ParameterSpec{Type: "object"}},
		{Name: "generate_response", Parameters: react.ParameterSpec{Type: "object"}},
	})
	testx.NoError(t, err)

	// 多工具时 eino 序列化为字符串 "required"。
	testx.Equal(t, "required", em.body()["tool_choice"], "多工具强制调用时 tool_choice 应为 required")
}

func TestEinoLLM_ToolChoiceOmittedWithoutTools(t *testing.T) {
	srv, em := newEmulator(t, func(w http.ResponseWriter) {
		writeJSON(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	})

	// 配置了 required，但没有声明任何 tools —— 请求体不应含 tool_choice。
	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{
		BaseURL:    srv.URL,
		APIKey:     "k",
		Model:      "m",
		ToolChoice: appkitllm.ToolChoiceRequired,
	})
	testx.NoError(t, err)
	_, err = llm.Chat(context.Background(), []react.Message{{Role: "user", Content: "hi"}}, nil)
	testx.NoError(t, err)

	_, hasToolChoice := em.body()["tool_choice"]
	testx.False(t, hasToolChoice, "无 tools 时不应发送 tool_choice，否则提供方可能报错")
}

// ---------- ChatStream（流式） ----------

func TestEinoLLM_ChatStream_TextChunks(t *testing.T) {
	srv, em := newEmulator(t, func(w http.ResponseWriter) {
		writeSSE(w,
			`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"你好"}}]}`,
			`{"id":"c1","choices":[{"index":0,"delta":{"content":"，"}}]}`,
			`{"id":"c1","choices":[{"index":0,"delta":{"content":"世界"}}]}`,
			`[DONE]`,
		)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	testx.NoError(t, err)

	var deltas []string
	var endingPayload *react.StreamedChatResponsePayload
	resp, err := llm.ChatStream(context.Background(), nil, nil, func(_ context.Context, p *react.StreamedChatResponsePayload, isEnding bool) error {
		if isEnding {
			endingPayload = p
			return nil
		}
		if len(p.Choices) > 0 && p.Choices[0].Delta.Content != "" {
			deltas = append(deltas, p.Choices[0].Delta.Content)
		}
		return nil
	})
	testx.NoError(t, err)
	testx.Equal(t, "你好，世界", resp.Content, "返回完整响应应与 Chat 一致")
	testx.Equal(t, []string{"你好", "，", "世界"}, deltas, "逐 chunk 回调增量内容")
	testx.NotNil(t, endingPayload, "应触发 ending 回调")
	testx.Empty(t, endingPayload.Choices, "ending 包为空载荷（完整内容由返回值携带）")
	testx.Equal(t, true, em.body()["stream"], "流式请求应带 stream=true")
}

func TestEinoLLM_ChatStream_Usage(t *testing.T) {
	srv, _ := newEmulator(t, func(w http.ResponseWriter) {
		writeSSE(w,
			`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`,
			`{"id":"c1","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`,
			`[DONE]`,
		)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	testx.NoError(t, err)

	var endingPayload *react.StreamedChatResponsePayload
	resp, err := llm.ChatStream(context.Background(), nil, nil, func(_ context.Context, p *react.StreamedChatResponsePayload, isEnding bool) error {
		if isEnding {
			endingPayload = p
		}
		return nil
	})
	testx.NoError(t, err)
	testx.Equal(t, "hi", resp.Content)
	testx.NotNil(t, endingPayload)
	testx.Equal(t, int64(3), endingPayload.Usage.PromptTokens, "ending 包应携带 prompt tokens")
	testx.Equal(t, int64(5), endingPayload.Usage.CompletionTokens)
	testx.Equal(t, int64(8), endingPayload.Usage.TotalTokens)
}

// TestEinoLLM_ChatStream_ToolCallFragmented 验证流式 tool_call 按 index 归并：
// id/name 只在首 chunk，后续 arguments 增量必须拼接（否则 generate_response 的
// task_arguments JSON 分片错乱 → Schema 校验失败）。
func TestEinoLLM_ChatStream_ToolCallFragmented(t *testing.T) {
	srv, _ := newEmulator(t, func(w http.ResponseWriter) {
		writeSSE(w,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"generate_response","arguments":"{\"thought\":\"分片1\","}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"friendly_reply\":\"ok\",\"task_type\":\"only_reply\""}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}`,
			`[DONE]`,
		)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	testx.NoError(t, err)

	resp, err := llm.ChatStream(context.Background(), nil, []react.FunctionDef{
		{Name: "generate_response"},
	}, func(_ context.Context, _ *react.StreamedChatResponsePayload, _ bool) error { return nil })
	testx.NoError(t, err)
	testx.Len(t, resp.ToolCalls, 1)
	testx.Equal(t, "call_1", resp.ToolCalls[0].ID)
	testx.Equal(t, "generate_response", resp.ToolCalls[0].Name)
	// 三片 arguments 拼接后的完整 JSON。
	testx.Equal(t, `{"thought":"分片1","friendly_reply":"ok","task_type":"only_reply"}`, resp.ToolCalls[0].Args)
}

// TestEinoLLM_ChatStream_Reasoning 验证 reasoning_content 被独立累积（不混入 content）。
func TestEinoLLM_ChatStream_Reasoning(t *testing.T) {
	srv, _ := newEmulator(t, func(w http.ResponseWriter) {
		writeSSE(w,
			`{"choices":[{"index":0,"delta":{"reasoning_content":"思考中"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"最终回答"}}]}`,
			`[DONE]`,
		)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	testx.NoError(t, err)

	var reasoning string
	resp, err := llm.ChatStream(context.Background(), nil, nil, func(_ context.Context, p *react.StreamedChatResponsePayload, _ bool) error {
		if len(p.Choices) > 0 {
			reasoning += p.Choices[0].Delta.ReasoningContent
		}
		return nil
	})
	testx.NoError(t, err)
	testx.Equal(t, "最终回答", resp.Content, "content 只含正文，不含 reasoning")
	testx.Equal(t, "思考中", reasoning, "reasoning_content 应独立回调")
}

// TestEinoLLM_ChatStream_StreamPlusToolChoiceRequired 验证流式与管家约束链兼容：
// stream=true 时仍带 tool_choice + thinking=disabled。
func TestEinoLLM_ChatStream_StreamPlusToolChoiceRequired(t *testing.T) {
	srv, em := newEmulator(t, func(w http.ResponseWriter) {
		writeSSE(w,
			`{"choices":[{"index":0,"delta":{"content":"ok"}}]}`,
			`[DONE]`,
		)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{
		BaseURL:    srv.URL,
		APIKey:     "k",
		Model:      "m",
		ToolChoice: appkitllm.ToolChoiceRequired,
	})
	testx.NoError(t, err)
	_, err = llm.ChatStream(context.Background(), nil, []react.FunctionDef{
		{Name: "generate_response", Parameters: react.ParameterSpec{Type: "object"}},
	}, func(_ context.Context, _ *react.StreamedChatResponsePayload, _ bool) error { return nil })
	testx.NoError(t, err)

	body := em.body()
	testx.Equal(t, true, body["stream"], "流式请求应带 stream=true")
	testx.NotNil(t, body["tool_choice"], "流式仍应带 tool_choice")
	th, _ := body["thinking"].(map[string]any)
	testx.NotNil(t, th, "流式仍应带 thinking 扩展字段")
	testx.Equal(t, "disabled", th["type"], "流式仍应带 thinking=disabled")
	testx.Equal(t, false, body["enable_thinking"], "流式仍应带 enable_thinking=false")
}

// TestEinoLLM_ChatStream_NilCallback 验证 callback==nil 时降级为同步 Chat。
func TestEinoLLM_ChatStream_NilCallback(t *testing.T) {
	srv, _ := newEmulator(t, func(w http.ResponseWriter) {
		writeJSON(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"sync"},"finish_reason":"stop"}]}`)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	testx.NoError(t, err)
	resp, err := llm.ChatStream(context.Background(), nil, nil, nil)
	testx.NoError(t, err)
	testx.Equal(t, "sync", resp.Content)
}

// TestEinoLLM_ChatStream_HTTPError 验证流式路径非 200 返回错误。
func TestEinoLLM_ChatStream_HTTPError(t *testing.T) {
	srv, _ := newEmulator(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	llm, err := appkitllm.NewEinoLLM(appkitllm.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	testx.NoError(t, err)
	_, err = llm.ChatStream(context.Background(), nil, nil, func(_ context.Context, _ *react.StreamedChatResponsePayload, _ bool) error {
		return nil
	})
	testx.Error(t, err)
	testx.True(t, strings.Contains(err.Error(), "500"), "应报 500 错误，got %v", err)
}
