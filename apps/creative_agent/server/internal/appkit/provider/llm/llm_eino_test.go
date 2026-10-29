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

// emulator is an httptest stub of the OpenAI-compatible protocol: it records the last request body and responds via the respond callback.
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

// writeSSE outputs data: lines per the OpenAI streaming protocol (including the [DONE] terminator).
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

// ---------- Chat (non-streaming) ----------

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
	testx.False(t, hasToolChoice, "the request body should not contain tool_choice when ToolChoice is empty")
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
		{Name: "search", Description: "search", Parameters: react.ParameterSpec{Type: "object"}},
	})
	testx.NoError(t, err)

	body := em.body()
	// eino serializes "forced + single tool" as the object form of tool_choice (equivalent to required).
	tc, ok := body["tool_choice"].(map[string]any)
	testx.True(t, ok, "tool_choice should be the object form when forcing a single tool, got %v", body["tool_choice"])
	testx.Equal(t, "function", tc["type"])
	fn, _ := tc["function"].(map[string]any)
	testx.Equal(t, "search", fn["name"])
	// Forced function calling must disable thinking mode (otherwise 400):
	// DeepSeek uses thinking=disabled, Qwen uses enable_thinking=false; both are passed through together.
	th, _ := body["thinking"].(map[string]any)
	testx.NotNil(t, th, "the request body should contain the thinking extension field")
	testx.Equal(t, "disabled", th["type"], "the request body should contain thinking=disabled")
	testx.Equal(t, false, body["enable_thinking"], "the request body should contain enable_thinking=false")
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

	// With multiple tools, eino serializes it as the string "required".
	testx.Equal(t, "required", em.body()["tool_choice"], "tool_choice should be required when forcing multiple tools")
}

func TestEinoLLM_ToolChoiceOmittedWithoutTools(t *testing.T) {
	srv, em := newEmulator(t, func(w http.ResponseWriter) {
		writeJSON(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	})

	// required is configured but no tools are declared — the request body should not contain tool_choice.
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
	testx.False(t, hasToolChoice, "tool_choice should not be sent without tools, otherwise the provider may error")
}

// ---------- ChatStream (streaming) ----------

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
	testx.Equal(t, "你好，世界", resp.Content, "the returned complete response should match Chat")
	testx.Equal(t, []string{"你好", "，", "世界"}, deltas, "delta content should be called back chunk by chunk")
	testx.NotNil(t, endingPayload, "the ending callback should fire")
	testx.Empty(t, endingPayload.Choices, "the ending chunk is an empty payload (the complete response is carried by the return value)")
	testx.Equal(t, true, em.body()["stream"], "streaming requests should carry stream=true")
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
	testx.Equal(t, int64(3), endingPayload.Usage.PromptTokens, "the ending chunk should carry prompt tokens")
	testx.Equal(t, int64(5), endingPayload.Usage.CompletionTokens)
	testx.Equal(t, int64(8), endingPayload.Usage.TotalTokens)
}

// TestEinoLLM_ChatStream_ToolCallFragmented verifies that streamed tool_calls are merged by index:
// id/name appear only in the first chunk; later arguments deltas must be concatenated (otherwise generate_response's
// task_arguments JSON fragments get scrambled → schema validation fails).
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
	// The complete JSON after concatenating the three arguments fragments.
	testx.Equal(t, `{"thought":"分片1","friendly_reply":"ok","task_type":"only_reply"}`, resp.ToolCalls[0].Args)
}

// TestEinoLLM_ChatStream_Reasoning verifies that reasoning_content is accumulated separately (not mixed into content).
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
	testx.Equal(t, "最终回答", resp.Content, "content holds only the answer text, not reasoning")
	testx.Equal(t, "思考中", reasoning, "reasoning_content should be called back separately")
}

// TestEinoLLM_ChatStream_StreamPlusToolChoiceRequired verifies streaming is compatible with the manager constraint chain:
// with stream=true it still carries tool_choice + thinking=disabled.
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
	testx.Equal(t, true, body["stream"], "streaming requests should carry stream=true")
	testx.NotNil(t, body["tool_choice"], "streaming should still carry tool_choice")
	th, _ := body["thinking"].(map[string]any)
	testx.NotNil(t, th, "streaming should still carry the thinking extension field")
	testx.Equal(t, "disabled", th["type"], "streaming should still carry thinking=disabled")
	testx.Equal(t, false, body["enable_thinking"], "streaming should still carry enable_thinking=false")
}

// TestEinoLLM_ChatStream_NilCallback verifies the degradation to synchronous Chat when callback==nil.
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

// TestEinoLLM_ChatStream_HTTPError verifies that a non-200 response on the streaming path returns an error.
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
	testx.True(t, strings.Contains(err.Error(), "500"), "a 500 error should be reported, got %v", err)
}
