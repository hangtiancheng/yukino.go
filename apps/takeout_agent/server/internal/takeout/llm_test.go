package takeout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

func newTestChatModel(t *testing.T, baseURL string) *model.DashScopeChatModel {
	t.Helper()
	chatModel, err := model.NewDashScopeChatModel(model.DashScopeConfig{
		APIKey:  "test-key",
		BaseURL: baseURL,
		Model:   "test-model",
	})
	if err != nil {
		t.Fatalf("NewDashScopeChatModel: %v", err)
	}
	return chatModel
}

// TestAgentCallTimesOut guards the per-call timeout: a hung provider must
// not hold the orchestrator's per-session lock forever.
func TestAgentCallTimesOut(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	// Defers run LIFO: closing release first unblocks the hung handler so
	// server.Close does not wait for an outstanding request forever.
	defer server.Close()
	defer close(release)

	agent := &Agent{
		Name:       "timeout_agent",
		ModelName:  "test-model",
		systemText: "system",
		chatModel:  newTestChatModel(t, server.URL),
		timeout:    50 * time.Millisecond,
	}
	start := time.Now()
	if _, err := agent.Call(context.Background(), "hello"); err == nil {
		t.Fatal("expected the call to fail with a timeout")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("call took %v, want it bounded by the 50ms timeout", elapsed)
	}
}

// TestAgentCallReturnsTextAndUsage verifies the timeout wrapper does not
// disturb a normal successful call.
func TestAgentCallReturnsTextAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmpl-1","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello back"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	defer server.Close()

	agent := &Agent{
		Name:       "ok_agent",
		ModelName:  "test-model",
		systemText: "system",
		chatModel:  newTestChatModel(t, server.URL),
		timeout:    5 * time.Second,
	}
	reply, err := agent.Call(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(reply.Text, "hello back") {
		t.Fatalf("reply.Text = %q, want it to contain the assistant content", reply.Text)
	}
	if !reply.HasUsage {
		t.Fatal("expected usage to be present")
	}
	if total := reply.TotalTokens(); total == nil || *total != 8 {
		t.Fatalf("TotalTokens = %v, want 8", total)
	}
}
