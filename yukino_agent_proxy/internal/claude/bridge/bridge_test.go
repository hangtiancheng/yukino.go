package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/config"
)

func requestFixture() Object {
	return Object{"model": "claude-client-model", "max_tokens": int64(1000), "system": []any{Object{"type": "text", "text": "x-anthropic-billing-header: unstable;"}, Object{"type": "text", "text": "Be helpful.", "cache_control": Object{"type": "ephemeral"}}}, "messages": []any{
		Object{"role": "user", "content": "Find a file."},
		Object{"role": "assistant", "content": []any{Object{"type": "text", "text": "Checking."}, Object{"type": "tool_use", "id": "call_one", "name": "Read", "input": Object{"path": "a.go"}}}},
		Object{"role": "user", "content": []any{Object{"type": "tool_result", "tool_use_id": "call_one", "content": []any{Object{"type": "text", "text": "file content"}}}, Object{"type": "text", "text": "Explain it."}}},
	}, "tools": []any{Object{"name": "Read", "input_schema": Object{"type": "object", "properties": Object{"path": Object{"type": "string"}}}}}, "tool_choice": Object{"type": "any", "disable_parallel_tool_use": true}}
}

func TestToolLoopRequestTranslation(t *testing.T) {
	for _, protocol := range []string{config.OpenAI, config.OpenAICompat} {
		t.Run(protocol, func(t *testing.T) {
			p := config.Provider{Protocol: protocol, Model: "upstream-model", MaxOutputTokens: 200}
			result, err := Request(requestFixture(), p)
			if err != nil {
				t.Fatal(err)
			}
			if result["model"] != p.Model || result["tool_choice"] != "required" || result["parallel_tool_calls"] != false {
				t.Fatal("model or tool selection was not translated")
			}
			if strings.Contains(JSON(result), "cache_control") || strings.Contains(JSON(result), "unstable") {
				t.Fatal("Anthropic-only metadata leaked")
			}
			if protocol == config.OpenAI {
				input := List(result["input"])
				call := Obj(input[2])
				output := Obj(input[3])
				if call["type"] != "function_call" || call["call_id"] != "call_one" || output["type"] != "function_call_output" || output["call_id"] != "call_one" {
					t.Fatalf("wrong Responses tool pairing: %s", JSON(input))
				}
				if Number(result["max_output_tokens"]) != 200 {
					t.Fatal("provider output cap ignored")
				}
			} else {
				messages := List(result["messages"])
				call := Obj(List(Obj(messages[2])["tool_calls"])[0])
				output := Obj(messages[3])
				if call["id"] != "call_one" || output["role"] != "tool" || output["tool_call_id"] != "call_one" || output["content"] != "file content" {
					t.Fatalf("wrong Chat tool pairing: %s", JSON(messages))
				}
			}
		})
	}
}

func TestReasoningRoundTrip(t *testing.T) {
	item := Object{"id": "rs_1", "type": "reasoning", "summary": []any{Object{"type": "summary_text", "text": "Need a tool."}}, "encrypted_content": "opaque-encrypted-state"}
	body := Object{"id": "resp_1", "status": "completed", "output": []any{item, Object{"type": "function_call", "call_id": "call_1", "name": "Read", "arguments": `{"path":"a.go"}`}}, "usage": Object{"input_tokens": float64(100), "input_tokens_details": Object{"cached_tokens": float64(80)}, "output_tokens": float64(10)}}
	message, err := Response(body, config.OpenAI, "model")
	if err != nil {
		t.Fatal(err)
	}
	if Number(Obj(message["usage"])["input_tokens"]) != 20 || message["stop_reason"] != "tool_use" {
		t.Fatal("usage or stop reason is incorrect")
	}
	request := Object{"max_tokens": int64(50), "messages": []any{Object{"role": "assistant", "content": message["content"]}}}
	result, err := Request(request, config.Provider{Protocol: config.OpenAI, Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	restored := Obj(List(result["input"])[0])
	if JSON(restored) != JSON(item) {
		t.Fatalf("reasoning item did not round trip: %s", JSON(restored))
	}
	request["messages"] = []any{Object{"role": "assistant", "content": []any{ReasoningBlock(item)}}}
	result, err = Request(request, config.Provider{Protocol: config.OpenAI, Model: "model"})
	if err != nil || len(List(result["input"])) != 0 {
		t.Fatal("orphan reasoning item was sent upstream")
	}
}

func sse(data Object) string { return "data: " + JSON(data) + "\r\n\r\n" }

func TestChatStreamParallelToolsAndLateUsage(t *testing.T) {
	chunk := func(delta Object, finish any) string {
		return sse(Object{"id": "chat_1", "choices": []any{Object{"delta": delta, "finish_reason": finish}}})
	}
	call := func(index int, id, name, args string) Object {
		return Object{"index": index, "id": id, "function": Object{"name": name, "arguments": args}}
	}
	stream := chunk(Object{"content": "\u4f60\u597d"}, nil) + chunk(Object{"tool_calls": []any{call(0, "", "", `{"path":`), call(1, "call_b", "Read", `{"path":"b"}`)}}, nil) + chunk(Object{"tool_calls": []any{call(0, "call_a", "Read", `"a"}`)}}, nil) + chunk(Object{}, "tool_calls") + chunk(Object{}, "tool_calls") + sse(Object{"choices": []any{}, "usage": Object{"prompt_tokens": 100, "completion_tokens": 8, "prompt_tokens_details": Object{"cached_tokens": 70}}}) + "data: [DONE]\n\n"
	var collector Collector
	deltas := 0
	err := ChatStream(strings.NewReader(stream), "model", func(event string, data Object) error {
		if event == "message_delta" {
			deltas++
		}
		return collector.Sink(event, data)
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := collector.Result()
	if err != nil {
		t.Fatal(err)
	}
	content := List(message["content"])
	if deltas != 1 || len(content) != 3 || Obj(content[0])["text"] != "\u4f60\u597d" || Obj(content[1])["id"] != "call_a" || Obj(Obj(content[2])["input"])["path"] != "b" {
		t.Fatalf("corrupt parallel tool stream: %s", JSON(message))
	}
	if Number(Obj(message["usage"])["input_tokens"]) != 30 {
		t.Fatal("late usage was lost")
	}
}

func TestResponsesStreamToolAndReasoningReplay(t *testing.T) {
	event := func(kind string, data Object) string { data["type"] = kind; return sse(data) }
	reasoning := Object{"id": "rs_1", "type": "reasoning", "summary": []any{Object{"text": "Think first."}}, "encrypted_content": "opaque"}
	call := Object{"id": "fc_1", "type": "function_call", "call_id": "call_1", "name": "Read", "arguments": `{"path":"a"}`}
	stream := event("response.created", Object{"response": Object{"id": "resp_1"}}) + event("response.reasoning_summary_text.delta", Object{"delta": "Think first."}) + event("response.output_item.done", Object{"output_index": 0, "item": reasoning}) + event("response.output_item.added", Object{"output_index": 1, "item": Object{"id": "fc_1", "type": "function_call", "call_id": "call_1", "name": "Read"}}) + event("response.function_call_arguments.delta", Object{"item_id": "fc_1", "output_index": 1, "delta": `{"path":`}) + event("response.function_call_arguments.done", Object{"item_id": "fc_1", "output_index": 1, "arguments": `{"path":"a"}`}) + event("response.output_item.done", Object{"output_index": 1, "item": call}) + event("response.completed", Object{"response": Object{"status": "completed", "output": []any{reasoning, call}, "usage": Object{"input_tokens": 5, "output_tokens": 3}}})
	var collector Collector
	if err := ResponsesStream(strings.NewReader(stream), "model", collector.Sink); err != nil {
		t.Fatal(err)
	}
	message, err := collector.Result()
	if err != nil {
		t.Fatal(err)
	}
	content := List(message["content"])
	if len(content) != 2 || JSON(replayReasoning(Obj(content[0]))) != JSON(reasoning) || Obj(Obj(content[1])["input"])["path"] != "a" {
		t.Fatalf("Responses stream did not preserve tool/reasoning state: %s", JSON(message))
	}
}

func TestTruncatedStreamsDoNotEmitSuccess(t *testing.T) {
	for _, tc := range []struct{ protocol, stream string }{{config.OpenAICompat, sse(Object{"choices": []any{Object{"delta": Object{"content": "partial"}}}})}, {config.OpenAI, sse(Object{"type": "response.output_text.delta", "delta": "partial"})}, {config.OpenAI, sse(Object{"type": "response.failed", "response": Object{"error": Object{"message": "failure"}}})}} {
		t.Run(tc.protocol+fmt.Sprint(len(tc.stream)), func(t *testing.T) {
			stopped := false
			err := Stream(strings.NewReader(tc.stream), tc.protocol, "model", func(event string, _ Object) error { stopped = stopped || event == "message_stop"; return nil })
			if err == nil || stopped {
				t.Fatal("failed stream was reported as successful")
			}
		})
	}
}

func TestResponsesJSONForStreamingRequest(t *testing.T) {
	body := Object{"status": "completed", "output": []any{Object{"type": "message", "content": []any{Object{"type": "output_text", "text": "answer"}}}}}
	var collector Collector
	if err := Stream(strings.NewReader(JSON(body)), config.OpenAI, "model", collector.Sink); err != nil {
		t.Fatal(err)
	}
	message, err := collector.Result()
	if err != nil || Obj(List(message["content"])[0])["text"] != "answer" {
		t.Fatal("JSON fallback failed")
	}
}

func TestSSEMultilineAndFinalEvent(t *testing.T) {
	var value Object
	err := ReadSSE(strings.NewReader("event: example\r\ndata: {\r\ndata: \"text\": \"\u4f60\u597d\"}\r\n"), func(event, data string) (bool, error) {
		if event != "example" {
			t.Fatal("wrong event name")
		}
		return true, json.Unmarshal([]byte(data), &value)
	})
	if err != nil || value["text"] != "\u4f60\u597d" {
		t.Fatal("multiline/EOF SSE parsing failed")
	}
}

func TestProviderThinkingAndForcedTools(t *testing.T) {
	for _, tc := range []struct{ base, model, field string }{{"https://api.deepseek.com", "deepseek-flash", "thinking"}, {"https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "qwen3.8-flash", "enable_thinking"}} {
		p := config.Provider{Protocol: config.OpenAICompat, BaseURL: tc.base, Model: tc.model, Thinking: "high"}
		body := requestFixture()
		delete(body, "tool_choice")
		result, err := Request(body, p)
		if err != nil {
			t.Fatal(err)
		}
		if tc.field == "thinking" {
			if Obj(result[tc.field])["type"] != "enabled" {
				t.Fatal("DeepSeek thinking was not enabled")
			}
		} else if result[tc.field] != true {
			t.Fatal("Qwen thinking was not enabled")
		}
		body = requestFixture()
		result, err = Request(body, p)
		if err != nil {
			t.Fatal(err)
		}
		if tc.field == "thinking" {
			if Obj(result[tc.field])["type"] != "disabled" || result["reasoning_effort"] != nil {
				t.Fatal("forced tool request retained DeepSeek thinking")
			}
		} else if result[tc.field] != false {
			t.Fatal("forced tool request retained Qwen thinking")
		}
	}
}

func TestResponsesMultipleMessageParts(t *testing.T) {
	first := Object{"id": "msg_1", "type": "message", "content": []any{Object{"type": "output_text", "text": "one"}, Object{"type": "output_text", "text": "two"}}}
	second := Object{"id": "msg_2", "type": "message", "content": []any{Object{"type": "output_text", "text": "three"}}}
	stream := sse(Object{"type": "response.output_text.delta", "item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": "one"}) + sse(Object{"type": "response.output_item.done", "output_index": 0, "item": first}) + sse(Object{"type": "response.output_item.done", "output_index": 1, "item": second}) + sse(Object{"type": "response.completed", "response": Object{"status": "completed", "output": []any{first, second}}})
	var collector Collector
	if err := ResponsesStream(strings.NewReader(stream), "model", collector.Sink); err != nil {
		t.Fatal(err)
	}
	message, err := collector.Result()
	if err != nil || Text(message["content"]) != "onetwothree" {
		t.Fatalf("message parts were lost or duplicated: %s %v", JSON(message), err)
	}
}

func TestTruncatedAnthropicStream(t *testing.T) {
	stream := sse(Object{"type": "message_start", "message": Object{"type": "message", "content": []any{}}})
	if err := Stream(strings.NewReader(stream), config.Anthropic, "model", func(string, Object) error { return nil }); err == nil {
		t.Fatal("truncated native stream was accepted")
	}
}
