package bridge

import (
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
)

func TestNamespacedCustomAndToolSearchRoundTrip(t *testing.T) {
	body := Object{"instructions": "instructions", "input": []any{Object{"role": "developer", "content": "developer instructions"}, Object{"role": "user", "content": "do work"}}, "tools": []any{Object{"type": "namespace", "name": "mcp", "tools": []any{Object{"type": "function", "name": "echo", "parameters": Object{"type": "object"}}}}, Object{"type": "custom", "name": "apply_patch", "format": Object{"type": "grammar", "definition": "patch grammar"}}, Object{"type": "tool_search"}}, "reasoning": Object{"effort": "none"}}
	for _, protocol := range []string{config.OpenAICompat, config.Anthropic} {
		request, tools, err := Request(body, config.Provider{Protocol: protocol, Model: "selected"})
		if err != nil {
			t.Fatal(err)
		}
		if request["model"] != "selected" || len(List(request["tools"])) != 3 {
			t.Fatal("tool declarations lost")
		}
		call := tools.Call("call1", "mcp__echo", `{"text":"hello"}`, "completed")
		if call["name"] != "echo" || call["namespace"] != "mcp" {
			t.Fatal("namespace was not restored")
		}
		custom := tools.Call("call2", "apply_patch", `{"input":"*** Begin Patch\n*** End Patch"}`, "completed")
		if custom["type"] != "custom_tool_call" || !strings.Contains(Str(custom["input"]), "\n") {
			t.Fatal("custom tool raw input lost")
		}
		search := tools.Call("call3", "tool_search", `{"query":"tools"}`, "completed")
		if search["type"] != "tool_search_call" {
			t.Fatal("tool search type lost")
		}
	}
}

func TestSignedThinkingToolReplay(t *testing.T) {
	tools := &Tools{Specs: map[string]ToolSpec{"echo": {Name: "echo", Kind: "function"}}}
	response, err := Response(Object{"type": "message", "id": "msg_1", "stop_reason": "tool_use", "content": []any{Object{"type": "thinking", "thinking": "plan", "signature": "signed"}, Object{"type": "tool_use", "id": "call1", "name": "echo", "input": Object{"text": "hello"}}}}, config.Anthropic, "claude-sonnet-4-6", tools)
	if err != nil {
		t.Fatal(err)
	}
	input := append(List(response["output"]), Object{"type": "function_call_output", "call_id": "call1", "output": "hello"})
	request, _, err := Request(Object{"input": input}, config.Provider{Protocol: config.Anthropic, Model: "claude-sonnet-4-6"})
	if err != nil {
		t.Fatal(err)
	}
	messages := List(request["messages"])
	block := Obj(List(Obj(messages[1])["content"])[0])
	if block["signature"] != "signed" {
		t.Fatal("signed thinking not replayed")
	}
	usage := Usage(Object{"input_tokens": 10, "cache_read_input_tokens": 20, "cache_creation_input_tokens": 5, "output_tokens": 3}, true)
	if Number(usage["input_tokens"]) != 35 {
		t.Fatal("Anthropic cache subsets not added to input total")
	}
}

func TestIncrementalChatStreamAndToolIdentity(t *testing.T) {
	tools := &Tools{Specs: map[string]ToolSpec{"echo": {Name: "echo", Kind: "function"}}}
	var collector Collector
	var events []string
	chunks := []Object{
		{"choices": []any{Object{"delta": Object{"reasoning_content": "plan"}}}},
		{"choices": []any{Object{"delta": Object{"tool_calls": []any{Object{"index": 0, "id": "call1", "function": Object{"name": "echo", "arguments": `{"text":`}}}}}}},
		{"choices": []any{Object{"delta": Object{"tool_calls": []any{Object{"index": 0, "function": Object{"arguments": `"hello"}`}}}}, "finish_reason": "tool_calls"}}, "usage": Object{"prompt_tokens": 4, "completion_tokens": 2}},
	}
	var input strings.Builder
	for _, chunk := range chunks {
		input.WriteString("data: " + JSON(chunk) + "\r\n\r\n")
	}
	stream := input.String() + "data: [DONE]\n\n"
	err := Stream(strings.NewReader(stream), config.OpenAICompat, "model", tools, func(event string, data Object) error {
		events = append(events, event)
		return collector.Sink(event, data)
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := collector.Result()
	if err != nil {
		t.Fatal(err)
	}
	calls := List(response["output"])
	call := Obj(calls[len(calls)-1])
	if call["call_id"] != "call1" || call["arguments"] != `{"text":"hello"}` {
		t.Fatal("fragmented call arguments lost")
	}
	if !strings.Contains(strings.Join(events, ","), "response.function_call_arguments.delta") {
		t.Fatal("tool arguments were not streamed")
	}
	if Number(Obj(response["usage"])["input_tokens"]) != 4 {
		t.Fatal("stream usage lost")
	}
	if err := Stream(strings.NewReader(strings.Split(stream, "data: [DONE]")[0][:20]), config.OpenAICompat, "model", tools, collector.Sink); err == nil {
		t.Fatal("truncated stream reported successful completion")
	}
}

func TestAnthropicStreamPreservesThinkingSignature(t *testing.T) {
	events := []Object{Object{"type": "message_start", "message": Object{"id": "m", "usage": Object{"input_tokens": 2}}}, Object{"type": "content_block_start", "index": 0, "content_block": Object{"type": "thinking", "thinking": ""}}, Object{"type": "content_block_delta", "index": 0, "delta": Object{"type": "thinking_delta", "thinking": "plan"}}, Object{"type": "content_block_delta", "index": 0, "delta": Object{"type": "signature_delta", "signature": "sig"}}, Object{"type": "content_block_stop", "index": 0}, Object{"type": "content_block_start", "index": 1, "content_block": Object{"type": "text", "text": ""}}, Object{"type": "content_block_delta", "index": 1, "delta": Object{"type": "text_delta", "text": "OK"}}, Object{"type": "content_block_stop", "index": 1}, Object{"type": "message_delta", "delta": Object{"stop_reason": "end_turn"}, "usage": Object{"output_tokens": 3}}, Object{"type": "message_stop"}}
	var input strings.Builder
	for _, e := range events {
		input.WriteString("event: " + Str(e["type"]) + "\ndata: " + JSON(e) + "\n\n")
	}
	var collector Collector
	if err := Stream(strings.NewReader(input.String()), config.Anthropic, "model", &Tools{Specs: map[string]ToolSpec{}}, collector.Sink); err != nil {
		t.Fatal(err)
	}
	response, _ := collector.Result()
	reasoning := Obj(List(response["output"])[0])
	block := Obj(DecodeEnvelope(ThinkingPrefix, Str(reasoning["encrypted_content"])))
	if block["signature"] != "sig" || block["thinking"] != "plan" {
		t.Fatal("thinking payload lost in stream")
	}
}

func TestContinuationAndCompaction(t *testing.T) {
	var history History
	request := Object{"input": "start"}
	response := Object{"id": "resp1", "output": []any{Object{"type": "function_call", "call_id": "call1", "name": "echo", "arguments": "{}"}}}
	history.Record(request, response)
	next := Object{"previous_response_id": "resp1", "input": []any{Object{"type": "function_call_output", "call_id": "call1", "output": "ok"}}}
	if history.Enrich(next) != nil || len(List(next["input"])) != 3 {
		t.Fatal("continuation history not recovered")
	}
	unknown := Object{"previous_response_id": "missing", "input": "next"}
	if history.Enrich(unknown) == nil {
		t.Fatal("missing continuation silently accepted")
	}
	tools := &Tools{Specs: map[string]ToolSpec{}, Compaction: true}
	compact, err := Response(Object{"choices": []any{Object{"message": Object{"content": "handoff"}, "finish_reason": "stop"}}}, config.OpenAICompat, "model", tools)
	if err != nil {
		t.Fatal(err)
	}
	items := List(compact["output"])
	if len(items) != 1 || Obj(items[0])["type"] != "compaction" {
		t.Fatal("compaction must yield exactly one summary item")
	}
	if DecodeEnvelope(CompactionPrefix, Str(Obj(items[0])["encrypted_content"])) != "handoff" {
		t.Fatal("summary cannot be replayed")
	}
}

func TestResultOnlyHistoryRepairPreservesMessageOrder(t *testing.T) {
	var history History
	thinking := chatReasoning("plan")
	history.Record(Object{"input": "start"}, Object{"id": "resp1", "output": []any{thinking, Object{"type": "function_call", "call_id": "call1", "name": "echo", "arguments": "{}"}, Object{"type": "function_call", "call_id": "call2", "name": "echo", "arguments": "{}"}}})
	body := Object{"input": []any{Object{"role": "user", "content": "original user message"}, thinking, Object{"type": "function_call_output", "call_id": "call1", "output": "one"}, Object{"type": "function_call_output", "call_id": "call2", "output": "two"}}}
	if err := history.Enrich(body); err != nil {
		t.Fatal(err)
	}
	items := List(body["input"])
	if len(items) != 6 || Obj(items[0])["role"] != "user" || Obj(items[2])["call_id"] != "call1" || Obj(items[3])["call_id"] != "call2" || Obj(items[4])["output"] != "one" {
		t.Fatal("history repair moved calls before the user message or duplicated reasoning")
	}
}

func TestNativeEncryptedCompactionIsPreserved(t *testing.T) {
	body := Object{"input": []any{Object{"type": "compaction", "encrypted_content": "native-state"}, Object{"role": "user", "content": "next"}}}
	request, _, err := Request(body, config.Provider{Protocol: config.OpenAI, Model: "model"})
	if err != nil || Obj(List(request["input"])[0])["encrypted_content"] != "native-state" {
		t.Fatal("native encrypted state was discarded")
	}
}

func TestAnthropicRequiresTerminalMessageStop(t *testing.T) {
	stream := "data: " + JSON(Object{"type": "message_start", "message": Object{"id": "m"}}) + "\n\n" +
		"data: " + JSON(Object{"type": "content_block_start", "index": 0, "content_block": Object{"type": "text", "text": "OK"}}) + "\n\n" +
		"data: " + JSON(Object{"type": "message_delta", "delta": Object{"stop_reason": "end_turn"}}) + "\n\n"
	if err := Stream(strings.NewReader(stream), config.Anthropic, "model", &Tools{}, func(string, Object) error { return nil }); err == nil {
		t.Fatal("truncated Messages stream was accepted")
	}
}

func TestAnthropicThinkingModelFamilies(t *testing.T) {
	for _, tc := range []struct{ model, effort, kind, mapped string }{
		{"claude-sonnet-4-5", "high", "enabled", ""},
		{"claude-haiku-4-5", "high", "enabled", ""},
		{"claude-sonnet-4-6", "xhigh", "adaptive", "max"},
		{"claude-opus-4-7", "xhigh", "adaptive", "xhigh"},
		{"claude-opus-5-5", "none", "adaptive", "low"},
		{"claude-sonnet-5-5", "none", "adaptive", "low"},
		{"claude-sonnet-5", "none", "disabled", ""},
		{"deepseek-flash", "none", "disabled", ""},
	} {
		request, _, err := Request(Object{"input": "hello", "reasoning": Object{"effort": tc.effort}}, config.Provider{Protocol: config.Anthropic, Model: tc.model})
		if err != nil || Obj(request["thinking"])["type"] != tc.kind || Str(Obj(request["output_config"])["effort"]) != tc.mapped {
			t.Fatalf("incorrect thinking for %s: %v", tc.model, err)
		}
	}
	body := Object{"input": "hello", "tools": []any{Object{"type": "function", "name": "echo"}}, "tool_choice": "required"}
	if _, _, err := Request(body, config.Provider{Protocol: config.Anthropic, Model: "claude-opus-5-5"}); err == nil {
		t.Fatal("forced choice was silently weakened on an always-thinking model")
	}
}
