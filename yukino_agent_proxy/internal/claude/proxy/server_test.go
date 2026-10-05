package proxy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/claude/bridge"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/claude/proxy"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/config"
)

func downstream(url string) anthropic.Client {
	return anthropic.NewClient(option.WithBaseURL(url+"/"), option.WithAPIKey("downstream-placeholder"), option.WithAuthToken(""), option.WithMaxRetries(0))
}
func requestParams() anthropic.MessageNewParams {
	return anthropic.MessageNewParams{Model: "client-model", MaxTokens: 128, Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("Read two files."))}}
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func event(w io.Writer, value bridge.Object) {
	_, _ = fmt.Fprintf(w, "data: %s\n\n", bridge.JSON(value))
}
func tool(index int, id, args string) bridge.Object {
	return bridge.Object{"index": index, "id": id, "type": "function", "function": bridge.Object{"name": "Read", "arguments": args}}
}
func chatTools(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, calls := range [][]any{
		{tool(0, "call_a", `{"path":`), tool(1, "call_b", `{"path":"b.go"}`)},
		{tool(0, "", `"a.go"}`)},
	} {
		event(w, bridge.Object{"id": "chat_1", "choices": []any{bridge.Object{"delta": bridge.Object{"tool_calls": calls}}}})
	}
	event(w, bridge.Object{"choices": []any{bridge.Object{"delta": bridge.Object{}, "finish_reason": "tool_calls"}}, "usage": bridge.Object{"prompt_tokens": 8, "completion_tokens": 4}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}
func responseTools(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	event(w, bridge.Object{"type": "response.created", "response": bridge.Object{"id": "resp_1"}})
	calls := []any{}
	for i, path := range []string{"a.go", "b.go"} {
		id := fmt.Sprintf("call_%c", 'a'+i)
		call := bridge.Object{"id": "fc_" + id, "type": "function_call", "call_id": id, "name": "Read", "arguments": bridge.JSON(bridge.Object{"path": path})}
		calls = append(calls, call)
		event(w, bridge.Object{"type": "response.output_item.done", "output_index": i, "item": call})
	}
	event(w, bridge.Object{"type": "response.completed", "response": bridge.Object{"status": "completed", "output": calls, "usage": bridge.Object{"input_tokens": 8, "output_tokens": 4}}})
}
func answer(protocol string) bridge.Object {
	switch protocol {
	case config.OpenAICompat:
		return bridge.Object{"id": "chat_answer", "choices": []any{bridge.Object{"message": bridge.Object{"role": "assistant", "content": "Files checked."}, "finish_reason": "stop"}}, "usage": bridge.Object{"prompt_tokens": 8, "completion_tokens": 3}}
	case config.OpenAI:
		return bridge.Object{"id": "resp_answer", "status": "completed", "output": []any{bridge.Object{"id": "msg_answer", "type": "message", "role": "assistant", "content": []any{bridge.Object{"type": "output_text", "text": "Files checked."}}}}, "usage": bridge.Object{"input_tokens": 8, "output_tokens": 3}}
	default:
		return bridge.Object{"id": "msg_answer", "type": "message", "role": "assistant", "model": "selected-model", "content": []any{bridge.Object{"type": "text", "text": "Files checked."}}, "stop_reason": "end_turn", "stop_sequence": nil, "usage": bridge.Object{"input_tokens": 8, "output_tokens": 3}, "vendor_extension": "preserved"}
	}
}

// Exercise a real Anthropic SDK client through the HTTP proxy and back through
// an official upstream SDK, including a two-tool result turn.
func TestSDKToolLoop(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "unrelated-environment-token")
	for _, protocol := range []string{config.Anthropic, config.OpenAICompat, config.OpenAI} {
		t.Run(protocol, func(t *testing.T) {
			requests := make(chan bridge.Object, 2)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expected := "/v1/messages"
				if protocol == config.OpenAICompat {
					expected = "/v1/chat/completions"
				}
				if protocol == config.OpenAI {
					expected = "/v1/responses"
				}
				if r.URL.Path != expected {
					t.Errorf("wrong upstream path: %s", r.URL.Path)
				}
				if protocol == config.Anthropic {
					if r.Header.Get("X-Api-Key") != "upstream-test-key" || r.Header.Get("Authorization") != "" {
						t.Error("wrong native authentication")
					}
				} else if r.Header.Get("Authorization") != "Bearer upstream-test-key" {
					t.Error("downstream credentials leaked upstream")
				}
				var body bridge.Object
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["model"] != "selected-model" {
					t.Error("model was not selected")
				}
				requests <- body
				if body["stream"] != true {
					writeJSON(w, answer(protocol))
					return
				}
				if protocol == config.OpenAICompat {
					chatTools(w)
					return
				}
				if protocol == config.OpenAI {
					responseTools(w)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				msg := answer(protocol)
				msg["stop_reason"] = "tool_use"
				msg["content"] = []any{bridge.Object{"type": "tool_use", "id": "call_a", "name": "Read", "input": bridge.Object{"path": "a.go"}}, bridge.Object{"type": "tool_use", "id": "call_b", "name": "Read", "input": bridge.Object{"path": "b.go"}}}
				_ = bridge.MessageSSE(msg, func(kind string, data bridge.Object) error {
					_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, bridge.JSON(data))
					return err
				})
			}))
			defer upstream.Close()
			gateway := httptest.NewServer(proxy.New(config.Provider{Protocol: protocol, Model: "selected-model", BaseURL: upstream.URL, APIKey: "upstream-test-key"}))
			defer gateway.Close()
			client := downstream(gateway.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			params := requestParams()
			stream := client.Messages.NewStreaming(ctx, params, option.WithJSONSet("tools", []any{bridge.Object{"name": "Read", "input_schema": bridge.Object{"type": "object", "properties": bridge.Object{"path": bridge.Object{"type": "string"}}}}}))
			var accumulated anthropic.Message
			for stream.Next() {
				if err := accumulated.Accumulate(stream.Current()); err != nil {
					t.Fatal(err)
				}
			}
			if err := stream.Err(); err != nil {
				t.Fatal(err)
			}
			_ = stream.Close()
			if accumulated.StopReason != "tool_use" || len(accumulated.Content) != 2 {
				t.Fatalf("bad streamed tool calls: %+v", accumulated)
			}
			for i, block := range accumulated.Content {
				var input map[string]string
				if err := json.Unmarshal(block.Input, &input); err != nil {
					t.Fatal(err)
				}
				if input["path"] != []string{"a.go", "b.go"}[i] {
					t.Fatal("parallel tool arguments were mixed")
				}
			}
			params.Messages = append(params.Messages, accumulated.ToParam(), anthropic.NewUserMessage(anthropic.NewToolResultBlock("call_a", "first file", false), anthropic.NewToolResultBlock("call_b", "second file", false)))
			result, err := client.Messages.New(ctx, params)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Content) != 1 || result.Content[0].Text != "Files checked." || result.StopReason != "end_turn" {
				t.Fatalf("bad answer: %+v", result)
			}
			<-requests
			history := <-requests
			wire := bridge.JSON(history)
			for _, part := range []string{"call_a", "call_b", "first file", "second file"} {
				if !strings.Contains(wire, part) {
					t.Errorf("history lost %s", part)
				}
			}
		})
	}
}

func TestStreamingFallbackAndForcedSSE(t *testing.T) {
	for _, protocol := range []string{config.OpenAI, config.OpenAICompat} {
		for _, mode := range []string{"json-for-stream", "sse-for-json"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if mode == "json-for-stream" {
						writeJSON(w, answer(protocol))
						return
					}
					// Deliberately mislabeled by a gateway.
					w.Header().Set("Content-Type", "application/json")
					if protocol == config.OpenAI {
						event(w, bridge.Object{"type": "response.completed", "response": answer(protocol)})
					} else {
						event(w, bridge.Object{"choices": []any{bridge.Object{"delta": bridge.Object{"content": "Files checked."}, "finish_reason": "stop"}}})
						_, _ = io.WriteString(w, "data: [DONE]\n\n")
					}
				}))
				defer upstream.Close()
				gateway := httptest.NewServer(proxy.New(config.Provider{Protocol: protocol, BaseURL: upstream.URL, Model: "model", APIKey: "test"}))
				defer gateway.Close()
				client := downstream(gateway.URL)
				if mode == "json-for-stream" {
					stream := client.Messages.NewStreaming(context.Background(), requestParams())
					defer stream.Close()
					var message anthropic.Message
					for stream.Next() {
						if err := message.Accumulate(stream.Current()); err != nil {
							t.Fatal(err)
						}
					}
					if err := stream.Err(); err != nil {
						t.Fatal(err)
					}
					if len(message.Content) != 1 || message.Content[0].Text != "Files checked." {
						t.Fatal("JSON streaming fallback failed")
					}
				} else {
					message, err := client.Messages.New(context.Background(), requestParams())
					if err != nil || len(message.Content) != 1 || message.Content[0].Text != "Files checked." {
						t.Fatalf("SSE aggregation failed: %v", err)
					}
				}
			})
		}
	}
}

func TestErrorRedactionAndValidation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(429)
		_ = json.NewEncoder(w).Encode(bridge.Object{"error": bridge.Object{"message": "denied upstream-secret"}})
	}))
	defer upstream.Close()
	gateway := httptest.NewServer(proxy.New(config.Provider{Protocol: config.OpenAICompat, BaseURL: upstream.URL, Model: "model", APIKey: "upstream-secret"}))
	defer gateway.Close()
	for _, tc := range []struct {
		body   string
		status int
	}{{`{"max_tokens":128,"messages":[{"role":"user","content":"hi"}]}`, 429}, {`{"max_tokens":1.5,"messages":[{"role":"user","content":"hi"}]}`, 400}, {`{"max_tokens":1,"messages":[{"role":"user","content":"hi"}],"stream":"yes"}`, 400}, {`{} {}`, 400}} {
		response, err := http.Post(gateway.URL+"/v1/messages", "application/json", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != tc.status || strings.Contains(string(data), "upstream-secret") {
			t.Fatalf("unsafe or wrong error: %d %s", response.StatusCode, data)
		}
		if tc.status == 429 && response.Header.Get("Retry-After") != "3" {
			t.Fatal("Retry-After lost")
		}
	}
	response, err := http.Post(gateway.URL+"/v1/messages/count_tokens", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 501 {
		t.Fatal("OpenAI token counting should report unsupported")
	}
}

func TestDownstreamCancellationReachesUpstream(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		event(w, bridge.Object{"choices": []any{bridge.Object{"delta": bridge.Object{"content": "first"}}}})
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()
	gateway := httptest.NewServer(proxy.New(config.Provider{Protocol: config.OpenAICompat, BaseURL: upstream.URL, Model: "model", APIKey: "test"}))
	defer gateway.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := downstream(gateway.URL)
	stream := client.Messages.NewStreaming(ctx, requestParams())
	if !stream.Next() {
		t.Fatalf("stream did not start: %v", stream.Err())
	}
	<-entered
	cancel()
	_ = stream.Close()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream stream survived downstream cancellation")
	}
}

func TestResponsesReasoningThroughSDK(t *testing.T) {
	checked := make(chan bool, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body bridge.Object
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			found := false
			for _, v := range bridge.List(body["input"]) {
				item := bridge.Obj(v)
				if item["type"] == "reasoning" && item["id"] == "rs_1" && item["encrypted_content"] == "opaque-state" {
					found = true
				}
			}
			checked <- found
			writeJSON(w, answer(config.OpenAI))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		reasoning := bridge.Object{"type": "reasoning", "id": "rs_1", "summary": []any{bridge.Object{"type": "summary_text", "text": "Use the tool."}}, "encrypted_content": "opaque-state"}
		call := bridge.Object{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "Read", "arguments": `{"path":"a.go"}`}
		event(w, bridge.Object{"type": "response.created", "response": bridge.Object{"id": "resp_1"}})
		event(w, bridge.Object{"type": "response.reasoning_summary_text.delta", "item_id": "rs_1", "output_index": 0, "delta": "Use the tool."})
		event(w, bridge.Object{"type": "response.output_item.done", "output_index": 0, "item": reasoning})
		event(w, bridge.Object{"type": "response.output_item.done", "output_index": 1, "item": call})
		event(w, bridge.Object{"type": "response.completed", "response": bridge.Object{"status": "completed", "output": []any{reasoning, call}}})
	}))
	defer upstream.Close()
	gateway := httptest.NewServer(proxy.New(config.Provider{Protocol: config.OpenAI, Model: "model", BaseURL: upstream.URL, APIKey: "test"}))
	defer gateway.Close()
	client := downstream(gateway.URL)
	params := requestParams()
	stream := client.Messages.NewStreaming(context.Background(), params)
	defer stream.Close()
	var message anthropic.Message
	for stream.Next() {
		if err := message.Accumulate(stream.Current()); err != nil {
			t.Fatal(err)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 2 || message.Content[0].Thinking != "Use the tool." || message.Content[0].Signature == "" {
		t.Fatal("reasoning block was lost or duplicated")
	}
	params.Messages = append(params.Messages, message.ToParam(), anthropic.NewUserMessage(anthropic.NewToolResultBlock("call_1", "file", false)))
	if _, err := client.Messages.New(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	if !<-checked {
		t.Fatal("encrypted reasoning did not survive SDK history replay")
	}
}
