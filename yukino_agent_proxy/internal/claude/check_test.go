package claude

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

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/claude/bridge"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/upstream"
)

func checkResponse(protocol string, stream bool) bridge.Object {
	switch protocol {
	case config.Anthropic:
		message := bridge.Object{"type": "message", "id": "msg_1", "role": "assistant", "model": "selected-model", "content": []any{}, "stop_reason": "end_turn", "usage": bridge.Object{"input_tokens": 1, "output_tokens": 0}}
		if stream {
			return bridge.Object{"type": "message_start", "message": message}
		}
		return message
	case config.OpenAICompat:
		if stream {
			return bridge.Object{"choices": []any{bridge.Object{"delta": bridge.Object{"role": "assistant", "content": ""}}}}
		}
		return bridge.Object{"choices": []any{bridge.Object{"message": bridge.Object{"role": "assistant", "content": "OK"}, "finish_reason": "stop"}}}
	default:
		response := bridge.Object{"id": "resp_1", "status": "completed", "output": []any{}}
		if stream {
			return bridge.Object{"type": "response.created", "response": response}
		}
		return response
	}
}

func TestConnectionCheckUsesSelectedInferenceRoute(t *testing.T) {
	for _, protocol := range []string{config.Anthropic, config.OpenAICompat, config.OpenAI} {
		for _, stream := range []bool{false, true} {
			t.Run(protocol+fmt.Sprint(stream), func(t *testing.T) {
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					expected := "/v1/messages"
					if protocol == config.OpenAICompat {
						expected = "/v1/chat/completions"
					}
					if protocol == config.OpenAI {
						expected = "/v1/responses"
					}
					if r.Method != "POST" || r.URL.Path != expected {
						t.Errorf("wrong inference route: %s %s", r.Method, r.URL.Path)
					}
					var body bridge.Object
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["model"] != "selected-model" || body["stream"] != true || body["tools"] != nil {
						t.Error("connection check did not use the selected model and a streaming request without tools")
					}
					if protocol == config.Anthropic {
						if r.Header.Get("X-Api-Key") != "private-key" {
							t.Error("wrong native credentials")
						}
					} else if r.Header.Get("Authorization") != "Bearer private-key" {
						t.Error("wrong OpenAI credentials")
					}
					value := checkResponse(protocol, stream)
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: %s\n\n", bridge.JSON(value))
					} else {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(value)
					}
				}))
				defer up.Close()
				client := upstream.New(config.Provider{Name: "provider", Protocol: protocol, BaseURL: up.URL, Model: "selected-model", APIKey: "private-key"})
				if err := Check(context.Background(), client); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestFailedConnectionChecksDoNotExposeCredentials(t *testing.T) {
	for _, protocol := range []string{config.Anthropic, config.OpenAICompat, config.OpenAI} {
		for _, mode := range []string{"authentication", "wrong-model", "malformed", "stream-error", "failed-json", "timeout", "unreachable"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					switch mode {
					case "authentication":
						w.WriteHeader(401)
						_, _ = fmt.Fprint(w, `{"error":{"message":"private-key"}}`)
					case "wrong-model":
						w.WriteHeader(404)
					case "malformed":
						_, _ = fmt.Fprint(w, "invalid private-key")
					case "stream-error":
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprint(w, "data: {\"type\":\"error\",\"error\":{\"message\":\"private-key\"}}\n\n")
					case "failed-json":
						_, _ = fmt.Fprint(w, `{"type":"error","status":"failed","error":{"message":"private-key"}}`)
					case "timeout":
						<-r.Context().Done()
					}
				}))
				defer up.Close()
				if mode == "unreachable" {
					up.Close()
				}
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				defer cancel()
				client := upstream.New(config.Provider{Name: "provider", Protocol: protocol, BaseURL: up.URL, Model: "model", APIKey: "private-key"})
				err := Check(ctx, client)
				if err == nil || strings.Contains(err.Error(), "private-key") {
					t.Fatalf("expected a safe failure: %v", err)
				}
			})
		}
	}
}

func TestConnectionCheckCancelsAfterFirstValidEvent(t *testing.T) {
	cancelled := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", bridge.JSON(checkResponse(config.OpenAICompat, true)))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer up.Close()
	client := upstream.New(config.Provider{Name: "provider", Protocol: config.OpenAICompat, BaseURL: up.URL, Model: "model", APIKey: "private-key"})
	if err := Check(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("connection check left inference running")
	}
}
