package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_codex_proxy/internal/bridge"
	"github.com/hangtiancheng/yukino.go/yukino_codex_proxy/internal/config"
	"github.com/klauspost/compress/zstd"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
)

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func fixture(protocol string, call bool) bridge.Object {
	if protocol == config.Anthropic {
		content := []any{bridge.Object{"type": "text", "text": "OK"}}
		reason := "end_turn"
		if call {
			content = []any{bridge.Object{"type": "tool_use", "id": "call1", "name": "echo", "input": bridge.Object{"text": "hello"}}}
			reason = "tool_use"
		}
		return bridge.Object{"id": "msg1", "type": "message", "content": content, "stop_reason": reason, "usage": bridge.Object{"input_tokens": 2, "output_tokens": 3}}
	}
	if protocol == config.OpenAICompat {
		message := bridge.Object{"content": "OK"}
		reason := "stop"
		if call {
			message = bridge.Object{"tool_calls": []any{bridge.Object{"id": "call1", "type": "function", "function": bridge.Object{"name": "echo", "arguments": `{"text":"hello"}`}}}}
			reason = "tool_calls"
		}
		return bridge.Object{"id": "chat1", "choices": []any{bridge.Object{"message": message, "finish_reason": reason}}, "usage": bridge.Object{"prompt_tokens": 2, "completion_tokens": 3}}
	}
	output := []any{bridge.Object{"id": "msg1", "type": "message", "role": "assistant", "status": "completed", "content": []any{bridge.Object{"type": "output_text", "text": "OK", "annotations": []any{}}}}}
	if call {
		output = []any{bridge.Object{"id": "fc_call1", "type": "function_call", "call_id": "call1", "name": "echo", "arguments": `{"text":"hello"}`, "status": "completed"}}
	}
	return bridge.Object{"id": "resp1", "object": "response", "status": "completed", "model": "selected", "output": output, "usage": bridge.Object{"input_tokens": 2, "output_tokens": 3, "total_tokens": 5}}
}

func TestOfficialResponsesClientAndToolContinuation(t *testing.T) {
	for _, protocol := range []string{config.OpenAI, config.OpenAICompat, config.Anthropic} {
		t.Run(protocol, func(t *testing.T) {
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				paths := map[string]string{config.OpenAI: "/v1/responses", config.OpenAICompat: "/v1/chat/completions", config.Anthropic: "/v1/messages"}
				if r.URL.Path != paths[protocol] {
					t.Errorf("wrong endpoint: %s", r.URL.Path)
				}
				if protocol == config.Anthropic {
					if r.Header.Get("X-Api-Key") != "upstream-secret" || r.Header.Get("Authorization") != "" {
						t.Error("incorrect Anthropic authentication")
					}
				} else if r.Header.Get("Authorization") != "Bearer upstream-secret" {
					t.Error("incorrect OpenAI authentication")
				}
				var request bridge.Object
				_ = json.NewDecoder(r.Body).Decode(&request)
				if request["model"] != "selected" {
					t.Error("selected model was not used")
				}
				if calls == 4 && protocol != config.OpenAI && !strings.Contains(bridge.JSON(request["messages"]), "call1") {
					t.Error("tool continuation lost call history")
				}
				writeJSON(w, fixture(protocol, calls == 3))
			}))
			defer up.Close()
			gateway := httptest.NewServer(New(config.Provider{Protocol: protocol, BaseURL: up.URL, Model: "selected", APIKey: "upstream-secret", Thinking: "none"}))
			defer gateway.Close()
			client := openai.NewClient(option.WithBaseURL(gateway.URL+"/v1/"), option.WithAPIKey("local-placeholder"), option.WithMaxRetries(0))
			params := responses.ResponseNewParams{Model: shared.ResponsesModel("ignored"), Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("hello")}}
			result, err := client.Responses.New(context.Background(), params)
			if err != nil || result.OutputText() != "OK" {
				t.Fatalf("SDK non-streaming response: %v", err)
			}
			stream := client.Responses.NewStreaming(context.Background(), params)
			defer stream.Close()
			completed, text := false, ""
			for stream.Next() {
				event := stream.Current()
				if event.Type == "response.output_text.delta" {
					text += event.AsResponseOutputTextDelta().Delta
				}
				if event.Type == "response.completed" {
					completed = true
				}
			}
			if stream.Err() != nil || !completed || text != "OK" {
				t.Fatalf("SDK stream failed: %v, completed=%v, text=%q", stream.Err(), completed, text)
			}
			request := bridge.Object{"input": "echo hello", "tools": []any{bridge.Object{"type": "function", "name": "echo", "parameters": bridge.Object{"type": "object", "properties": bridge.Object{"text": bridge.Object{"type": "string"}}}}}}
			var first bridge.Object
			if err := client.Post(context.Background(), "responses", request, &first); err != nil {
				t.Fatal(err)
			}
			input := []any{bridge.Object{"type": "function_call_output", "call_id": "call1", "output": "hello"}}
			if protocol == config.OpenAI {
				input = append(bridge.List(first["output"]), input...)
			}
			var final bridge.Object
			request["input"] = input
			if protocol != config.OpenAI {
				request["previous_response_id"] = first["id"]
			}
			if err := client.Post(context.Background(), "responses", request, &final); err != nil {
				t.Fatal(err)
			}
			if final["status"] != "completed" || calls != 4 {
				t.Fatal("tool continuation did not complete")
			}
		})
	}
}

func TestCompressionAndUpstreamErrors(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(429)
		writeJSON(w, bridge.Object{"error": bridge.Object{"message": "failed secret-key"}})
	}))
	defer up.Close()
	gateway := httptest.NewServer(New(config.Provider{Protocol: config.OpenAICompat, BaseURL: up.URL, Model: "model", APIKey: "secret-key"}))
	defer gateway.Close()
	for _, encoding := range []string{"identity", "gzip", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			var body bytes.Buffer
			input := []byte(`{"input":"hello"}`)
			switch encoding {
			case "identity":
				body.Write(input)
			case "gzip":
				writer := gzip.NewWriter(&body)
				_, _ = writer.Write(input)
				_ = writer.Close()
			case "zstd":
				writer, _ := zstd.NewWriter(&body)
				_, _ = writer.Write(input)
				_ = writer.Close()
			}
			request, _ := http.NewRequest("POST", gateway.URL+"/responses", &body)
			request.Header.Set("Content-Encoding", encoding)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			data, _ := io.ReadAll(response.Body)
			if response.StatusCode != 429 || response.Header.Get("Retry-After") != "2" || strings.Contains(string(data), "secret-key") || !strings.Contains(string(data), "[redacted]") {
				t.Fatalf("error forwarding failed: %s", data)
			}
		})
	}
	for _, input := range []string{`{"input":"x"} {}`, `{"input":"x","stream":"true"}`, `[]`} {
		response, err := http.Post(gateway.URL+"/responses", "application/json", strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatal("invalid request was accepted")
		}
	}
}

func TestStreamingCancellationReachesUpstream(t *testing.T) {
	canceled := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	}))
	defer up.Close()
	gateway := httptest.NewServer(New(config.Provider{Protocol: config.OpenAICompat, BaseURL: up.URL, Model: "model", APIKey: "key"}))
	defer gateway.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "POST", gateway.URL+"/responses", strings.NewReader(`{"input":"hello","stream":true}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	buf := make([]byte, 128)
	if _, err := response.Body.Read(buf); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("client cancellation did not reach upstream")
	}
}

func TestNativeResponsesStreamingFieldsAndCompactEndpoint(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body bridge.Object
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path == "/gateway/v1/responses/compact" {
			writeJSON(w, bridge.Object{"id": "cmp1", "object": "response.compaction", "output": []any{bridge.Object{"type": "compaction", "encrypted_content": "native-state"}}})
			return
		}
		if r.URL.Path != "/gateway/v1/responses" {
			t.Errorf("full endpoint was not retained: %s", r.URL.Path)
		}
		if body["model"] != "selected" || bridge.Obj(body["metadata"])["tag"] != "keep" || body["store"] != false || body["prompt_cache_key"] != "cache-key" {
			t.Error("native request fields were lost")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_ = bridge.ResponseSSE(fixture(config.OpenAI, false), func(event string, data bridge.Object) error {
			_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, bridge.JSON(data))
			w.(http.Flusher).Flush()
			return err
		})
	}))
	defer up.Close()
	gateway := httptest.NewServer(New(config.Provider{Protocol: config.OpenAI, BaseURL: up.URL + "/gateway/v1/responses", Model: "selected", APIKey: "secret"}))
	defer gateway.Close()
	client := openai.NewClient(option.WithBaseURL(gateway.URL+"/v1/"), option.WithAPIKey("local"), option.WithMaxRetries(0))
	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel("ignored"), Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("hello")},
		Metadata: shared.Metadata{"tag": "keep"}, Store: openai.Bool(false), PromptCacheKey: openai.String("cache-key"),
	}
	response, err := client.Responses.New(context.Background(), params)
	if err != nil || response.OutputText() != "OK" {
		t.Fatalf("native SSE fallback failed: %v", err)
	}
	stream := client.Responses.NewStreaming(context.Background(), params)
	defer stream.Close()
	completed := false
	for stream.Next() {
		if stream.Current().Type == "response.completed" {
			completed = true
		}
	}
	if stream.Err() != nil || !completed {
		t.Fatalf("native SSE forwarding failed: %v", stream.Err())
	}
	var compact bridge.Object
	if err := client.Post(context.Background(), "responses/compact", bridge.Object{"input": "history"}, &compact); err != nil {
		t.Fatal(err)
	}
	if compact["object"] != "response.compaction" || bridge.Obj(bridge.List(compact["output"])[0])["encrypted_content"] != "native-state" {
		t.Fatal("native compaction state was lost")
	}
}

func TestTruncatedGatewayStreamEmitsFailure(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	}))
	defer up.Close()
	gateway := httptest.NewServer(New(config.Provider{Protocol: config.OpenAICompat, BaseURL: up.URL, Model: "model", APIKey: "key"}))
	defer gateway.Close()
	response, err := http.Post(gateway.URL+"/responses", "application/json", strings.NewReader(`{"input":"hello","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var events []string
	if err := bridge.ReadSSE(response.Body, func(event, _ string) (bool, error) { events = append(events, event); return true, nil }); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(events, ",")
	if !strings.Contains(joined, "response.failed") || strings.Contains(joined, "response.completed") {
		t.Fatal("truncated stream became a successful turn")
	}
}
