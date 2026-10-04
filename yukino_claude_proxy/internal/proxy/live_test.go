package proxy_test

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/bridge"
	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/proxy"
	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/upstream"
)

// Opt in explicitly: this test sends small, billable requests to configured
// providers. It never changes Claude Code settings or logs request credentials.
func TestLiveProviders(t *testing.T) {
	if os.Getenv("YUKINO_PROXY_LIVE_TEST") != "1" {
		t.Skip("set YUKINO_PROXY_LIVE_TEST=1 to test configured endpoints")
	}
	path := os.Getenv("YUKINO_PROXY_LIVE_CONFIG")
	if path == "" {
		var err error
		path, err = config.DefaultPath()
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, configured := range cfg.Providers {
		if !config.ValidProtocol(configured.Protocol) {
			continue
		}
		t.Run(configured.Name, func(t *testing.T) {
			p, err := cfg.Select(configured.Protocol, configured.Name)
			if err != nil {
				t.Fatal(err)
			}
			gateway := httptest.NewServer(proxy.New(p))
			defer gateway.Close()
			client := downstream(gateway.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := upstream.New(p).Check(ctx); err != nil {
				t.Fatal(err)
			}
			params := anthropic.MessageNewParams{Model: "client-alias", MaxTokens: 2048, Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("Reply with the single word OK."))}}
			message, err := client.Messages.New(ctx, params)
			if err != nil {
				t.Fatal("nonstreaming live request failed")
			}
			if message.Model != anthropic.Model(p.Model) || len(message.Content) == 0 {
				t.Fatal("nonstreaming live message is empty or has the wrong model")
			}
			stream := client.Messages.NewStreaming(ctx, params)
			var accumulated anthropic.Message
			for stream.Next() {
				if err := accumulated.Accumulate(stream.Current()); err != nil {
					t.Fatal("live stream accumulation failed")
				}
			}
			if err := stream.Err(); err != nil {
				t.Fatal("streaming live request failed")
			}
			_ = stream.Close()
			if accumulated.StopReason == "" || len(accumulated.Content) == 0 {
				t.Fatal("live stream is incomplete")
			}
			params.Messages = []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("Call echo exactly once with text hello. The tool result is required before answering."))}
			tool := bridge.Object{"name": "echo", "description": "Return the supplied text.", "input_schema": bridge.Object{"type": "object", "properties": bridge.Object{"text": bridge.Object{"type": "string"}}, "required": []string{"text"}}}
			called, err := client.Messages.New(ctx, params, option.WithJSONSet("tools", []any{tool}), option.WithJSONSet("tool_choice", bridge.Object{"type": "any"}), option.WithJSONSet("thinking", bridge.Object{"type": "disabled"}))
			if err != nil {
				t.Fatalf("live tool-call request failed: %s", strings.ReplaceAll(err.Error(), p.APIKey, "[redacted]"))
			}
			var results []anthropic.ContentBlockParamUnion
			for _, block := range called.Content {
				if block.Type == "tool_use" {
					results = append(results, anthropic.NewToolResultBlock(block.ID, "hello", false))
				}
			}
			if len(results) == 0 || called.StopReason != "tool_use" {
				t.Fatal("live provider did not return a tool call")
			}
			params.Messages = append(params.Messages, called.ToParam(), anthropic.NewUserMessage(results...))
			final, err := client.Messages.New(ctx, params, option.WithJSONSet("tools", []any{tool}), option.WithJSONSet("tool_choice", bridge.Object{"type": "none"}), option.WithJSONSet("thinking", bridge.Object{"type": "disabled"}))
			if err != nil || final.StopReason == "" {
				t.Fatal("live tool-result round trip failed")
			}
			t.Logf("%s: nonstreaming, streaming, and tool round trip passed", p.Protocol)
		})
	}
}
