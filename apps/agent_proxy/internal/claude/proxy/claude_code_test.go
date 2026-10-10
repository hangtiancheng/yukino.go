package proxy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/claude"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/claude/bridge"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/claude/proxy"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
)

func TestClaudeCodeSystemMessages(t *testing.T) {
	for _, protocol := range []string{config.Anthropic, config.OpenAICompat, config.OpenAI} {
		for _, stream := range []bool{false, true} {
			mode := "json"
			if stream {
				mode = "sse"
			}
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				requests := make(chan bridge.Object, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request bridge.Object
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					requests <- request
					if !stream {
						writeJSON(w, answer(protocol))
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					switch protocol {
					case config.OpenAICompat:
						event(w, bridge.Object{"choices": []any{bridge.Object{"delta": bridge.Object{"content": "Files checked."}, "finish_reason": "stop"}}})
						_, _ = io.WriteString(w, "data: [DONE]\n\n")
					case config.OpenAI:
						event(w, bridge.Object{"type": "response.completed", "response": answer(protocol)})
					default:
						if err := bridge.MessageSSE(answer(protocol), func(kind string, data bridge.Object) error {
							_, err := io.WriteString(w, "event: "+kind+"\ndata: "+bridge.JSON(data)+"\n\n")
							return err
						}); err != nil {
							t.Error(err)
						}
					}
				}))
				defer upstream.Close()
				gateway := httptest.NewServer(proxy.New(config.Provider{Protocol: protocol, BaseURL: upstream.URL, Model: "selected-model", APIKey: "test"}))
				defer gateway.Close()
				client := downstream(gateway.URL)
				messages := []any{
					bridge.Object{"role": "user", "content": []any{bridge.Object{"type": "text", "text": "Hello"}}},
					bridge.Object{"role": "system", "content": []any{bridge.Object{"type": "text", "text": "Current context."}}},
				}
				opts := []option.RequestOption{option.WithJSONSet("messages", messages), option.WithJSONSet("system", "Global context.")}
				var message anthropic.Message
				if stream {
					response := client.Messages.NewStreaming(context.Background(), requestParams(), opts...)
					defer response.Close()
					for response.Next() {
						if err := message.Accumulate(response.Current()); err != nil {
							t.Fatal(err)
						}
					}
					if err := response.Err(); err != nil {
						t.Fatal(err)
					}
				} else {
					response, err := client.Messages.New(context.Background(), requestParams(), opts...)
					if err != nil {
						t.Fatal(err)
					}
					message = *response
				}
				if len(message.Content) != 1 || message.Content[0].Text != "Files checked." || message.StopReason != "end_turn" {
					t.Fatalf("incomplete response: %+v", message)
				}
				request := <-requests
				key, offset := "messages", 0
				if protocol == config.OpenAI {
					key = "input"
				} else if protocol == config.OpenAICompat {
					offset = 1
				}
				converted := bridge.List(request[key])
				if len(converted) != 2+offset || bridge.Obj(converted[offset])["role"] != "user" || bridge.Obj(converted[1+offset])["role"] != "system" {
					t.Fatalf("mid-conversation system role or position changed: %s", bridge.JSON(converted))
				}
			})
		}
	}
}

func TestLiveClaudeCode(t *testing.T) {
	if os.Getenv("YUKINO_PROXY_CLAUDE_TEST") != "1" {
		t.Skip("set YUKINO_PROXY_CLAUDE_TEST=1 to test the installed Claude Code with a real provider")
	}
	executable, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal("Claude Code executable is required for this opt-in test")
	}
	path := os.Getenv("YUKINO_PROXY_LIVE_CONFIG")
	if path == "" {
		path, err = config.DefaultPath()
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := cfg.Select("", os.Getenv("YUKINO_PROXY_LIVE_NAME"))
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxOutputTokens == 0 || p.MaxOutputTokens > 2048 {
		p.MaxOutputTokens = 2048
	}
	gateway := httptest.NewServer(proxy.New(p))
	defer gateway.Close()
	dir := t.TempDir()
	settingsProvider := config.Provider{Protocol: config.OpenAICompat, Model: p.Model, ContextWindow: p.ContextWindow}
	if _, err := claude.Configure(dir, settingsProvider, gateway.URL); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-p", "Reply with the single word OK. Do not use tools.", "--model", p.Model, "--no-session-persistence", "--setting-sources", "", "--settings", filepath.Join(dir, "settings.json"))
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CLAUDECODE=") && !strings.HasPrefix(entry, "CLAUDE_CODE_USE_") && !strings.HasPrefix(entry, "CLAUDE_CODE_MAX_CONTEXT_TOKENS=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env,
		"CLAUDE_CONFIG_DIR="+dir,
		"ANTHROPIC_BASE_URL="+gateway.URL,
		"ANTHROPIC_AUTH_TOKEN=local-test-placeholder",
		"ANTHROPIC_API_KEY=",
		"DISABLE_CLAUDE_CODE_NONESSENTIAL_TRAFFIC=1",
	)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Claude Code request failed: %v; response: %s", err, strings.ReplaceAll(string(output)+diagnostics.String(), p.APIKey, "[redacted]"))
	}
	if strings.TrimSpace(string(output)) != "OK" {
		t.Fatalf("expected Claude Code to return OK, got: %s", strings.ReplaceAll(string(output), p.APIKey, "[redacted]"))
	}
	if p.ContextWindow > 0 && strings.Contains(diagnostics.String(), "isn't described by this version's model catalog") {
		t.Fatal("Claude Code still warns about an unknown context window despite the provider declaration")
	}
	t.Logf("installed Claude Code returned OK through %s provider %s", p.Protocol, p.Name)
}
