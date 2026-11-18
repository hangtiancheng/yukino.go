package models_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hangtiancheng/yukino-code/apps/yukino/conversation"
	"github.com/hangtiancheng/yukino-code/apps/yukino/tools"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/runtime"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func TestAnthropicStreamingUsesYukinoProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["stream"] != true || body["max_tokens"] != float64(2048) || body["model"] != "test" {
			t.Errorf("body: %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		events := []struct{ name, body string }{
			{"message_start", `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"test","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from Yukino"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":3}}`},
			{"message_stop", `{"type":"message_stop"}`},
		}
		for _, event := range events {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, event.body)
		}
	}))
	defer server.Close()
	client, protocol, err := models.New(&config.Config{ModelProvider: "anthropic"}, config.ChatModelConfig{APIKey: "test", BaseURL: server.URL, Model: "test", MaxTokens: 2048}, "Be helpful")
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry()
	defer registry.Dispose()
	conv := conversation.NewManager()
	conv.AddUserMessage("hello")
	answer, err := runtime.Run(context.Background(), client, protocol, registry, conv, 1, nil)
	if err != nil || answer != "Hello from Yukino" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
}
func TestUnknownProviderFails(t *testing.T) {
	if _, _, err := models.New(&config.Config{ModelProvider: "unknown"}, config.ChatModelConfig{}, ""); err == nil {
		t.Fatal("accepted unknown provider")
	}
}
