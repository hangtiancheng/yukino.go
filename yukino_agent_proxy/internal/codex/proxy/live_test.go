package proxy

import (
	"context"
	"flag"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/codex"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/codex/bridge"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/upstream"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
)

var live = flag.Bool("live", false, "Run inference against real providers from ~/.yukino/config.yaml")

// Opt-in because these requests consume upstream inference quota.
func TestLiveProviders(t *testing.T) {
	if !*live {
		t.Skip("pass -args -live to enable real endpoint tests")
	}
	path, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{config.OpenAI, config.OpenAICompat, config.Anthropic} {
		t.Run(protocol, func(t *testing.T) {
			exists := false
			for _, p := range cfg.Providers {
				if p.Protocol == protocol {
					exists = true
					break
				}
			}
			if !exists {
				t.Skip("no provider configured for this protocol")
			}
			p, err := cfg.Select(protocol, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("provider=%s model=%s", p.Name, p.Model)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if err := codex.Check(ctx, upstream.New(p)); err != nil {
				t.Fatal(err)
			}
			gateway := httptest.NewServer(New(p))
			defer gateway.Close()
			client := openai.NewClient(option.WithBaseURL(gateway.URL+"/v1/"), option.WithAPIKey("local-placeholder"), option.WithMaxRetries(0))
			params := responses.ResponseNewParams{Model: shared.ResponsesModel(p.Model), Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("Reply with the single word OK.")}, MaxOutputTokens: openai.Int(2048), Reasoning: shared.ReasoningParam{Effort: shared.ReasoningEffortLow}}
			result, err := client.Responses.New(ctx, params)
			if err != nil || result.OutputText() == "" {
				t.Fatalf("non-streaming request failed: %v", safeError(err, p.APIKey))
			}
			stream := client.Responses.NewStreaming(ctx, params)
			defer stream.Close()
			terminal := false
			for stream.Next() {
				event := stream.Current()
				if event.Type == "response.completed" || event.Type == "response.incomplete" {
					terminal = true
				}
			}
			if stream.Err() != nil || !terminal {
				t.Fatalf("streaming request failed: %v", safeError(stream.Err(), p.APIKey))
			}
			body := bridge.Object{
				"input":             "Call the echo tool with text hello. Do not answer before calling it.",
				"max_output_tokens": 2048, "reasoning": bridge.Object{"effort": "none"},
				"tools":       []any{bridge.Object{"type": "function", "name": "echo", "description": "Echo the given text", "parameters": bridge.Object{"type": "object", "properties": bridge.Object{"text": bridge.Object{"type": "string"}}, "required": []string{"text"}, "additionalProperties": false}}},
				"tool_choice": bridge.Object{"type": "function", "name": "echo"},
			}
			var first bridge.Object
			if err := client.Post(ctx, "responses", body, &first); err != nil {
				t.Fatal(safeError(err, p.APIKey))
			}
			outputs := bridge.List(first["output"])
			var call bridge.Object
			for _, item := range outputs {
				if bridge.Obj(item)["type"] == "function_call" {
					call = bridge.Obj(item)
					break
				}
			}
			if call == nil {
				t.Fatal("provider did not return the requested tool call")
			}
			body["input"] = append(append(bridge.InputItems(body["input"]), outputs...), bridge.Object{"type": "function_call_output", "call_id": call["call_id"], "output": "hello"})
			body["tool_choice"] = "none"
			var final bridge.Object
			if err := client.Post(ctx, "responses", body, &final); err != nil {
				t.Fatal(safeError(err, p.APIKey))
			}
			if final["status"] != "completed" {
				t.Fatal("tool result continuation did not complete")
			}
		})
	}
}

func safeError(err error, key string) string {
	if err == nil {
		return ""
	}
	return redact(err.Error(), key)
}
