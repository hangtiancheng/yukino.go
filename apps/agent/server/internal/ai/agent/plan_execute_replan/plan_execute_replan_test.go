package plan_execute_replan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func TestPlanExecuteReplanWithSDKToolLoop(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/chat/completions" {
			t.Errorf("request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["stream"] != true {
			t.Error("expected streaming provider")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		text := ""
		finish := "stop"
		switch calls {
		case 1:
			text = `{"steps":["Check the current time"]}`
		case 2:
			fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_time\",\"type\":\"function\",\"function\":{\"name\":\"get_current_time\",\"arguments\":\"{}\"}}]},\"finish_reason\":null}]}\n\n")
			finish = "tool_calls"
		case 3:
			raw, _ := json.Marshal(body["messages"])
			if !strings.Contains(string(raw), "Current time retrieved successfully") {
				t.Errorf("missing native tool result: %s", raw)
			}
			text = "Current time verified."
		case 4:
			raw, _ := json.Marshal(body["messages"])
			if !strings.Contains(string(raw), "Current time verified.") {
				t.Error("replanner missing execution evidence")
			}
			text = `{"done":false,"remaining":["Summarize findings"],"summary":""}`
		case 5:
			raw, _ := json.Marshal(body["messages"])
			if !strings.Contains(string(raw), "Current time verified.") || !strings.Contains(string(raw), "Summarize findings") {
				t.Error("second step missing accumulated evidence or updated plan")
			}
			text = "Findings summarized."
		case 6:
			text = `{"done":true,"remaining":[],"summary":"Completed report"}`
		default:
			t.Errorf("unexpected provider call %d", calls)
		}
		delta, _ := json.Marshal(map[string]any{"content": text})
		fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]}\n\n", delta)
		fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", finish)
	}))
	defer server.Close()
	cfg := &config.Config{ModelProvider: "openai", ThinkChatModel: config.ChatModelConfig{APIKey: "test", BaseURL: server.URL, Model: "test", MaxTokens: 2048}, QuickChatModel: config.ChatModelConfig{APIKey: "test", BaseURL: server.URL, Model: "test", MaxTokens: 2048}}
	answer, details, err := BuildPlanAgent(context.Background(), cfg, "Check current time")
	if err != nil || answer != "Completed report" || len(details) != 2 || details[0] != "Current time verified." || details[1] != "Findings summarized." || calls != 6 {
		t.Fatalf("answer=%q details=%v err=%v calls=%d", answer, details, err, calls)
	}
}
func TestPlanValidationAndJSON(t *testing.T) {
	for _, steps := range [][]string{nil, {}, {" "}} {
		if validSteps(steps) == nil {
			t.Errorf("accepted steps %v", steps)
		}
	}
	if validSteps([]string{"do work"}) != nil {
		t.Fatal("valid steps rejected")
	}
	raw, err := extractJSONObject("text before {\"text\":\"} \\\" {\"} trailing")
	if err != nil || !json.Valid([]byte(raw)) {
		t.Fatalf("extraction: %q %v", raw, err)
	}
	if _, err := extractJSONObject("{\"unfinished\":"); err == nil {
		t.Fatal("accepted incomplete JSON")
	}
}
