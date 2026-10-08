package bridge

import (
	"fmt"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
)

func Response(body Object, protocol, model string, tools *Tools) (Object, error) {
	if body["error"] != nil || body["type"] == "error" {
		return nil, fmt.Errorf("upstream returned an error envelope")
	}
	if protocol == config.OpenAI {
		if body["object"] != "response" || List(body["output"]) == nil {
			return nil, fmt.Errorf("upstream did not return a Responses object")
		}
		if body["status"] != "completed" && body["status"] != "incomplete" {
			return nil, fmt.Errorf("upstream did not return a terminal Responses object")
		}
		if tools.Compaction {
			return compactResponse(body)
		}
		return body, nil
	}
	output := []any{}
	reason := ""
	usage := Object{}
	if protocol == config.Anthropic {
		if body["type"] != "message" || List(body["content"]) == nil {
			return nil, fmt.Errorf("upstream did not return an Anthropic message")
		}
		reason = Str(body["stop_reason"])
		usage = Usage(Obj(body["usage"]), true)
		for _, raw := range List(body["content"]) {
			block := Obj(raw)
			switch block["type"] {
			case "text":
				output = append(output, messageItem(Str(block["text"])))
			case "tool_use":
				if Str(block["id"]) == "" || Str(block["name"]) == "" {
					return nil, fmt.Errorf("upstream tool call is missing its identity")
				}
				output = append(output, tools.Call(Str(block["id"]), Str(block["name"]), JSON(block["input"]), "completed"))
			case "thinking", "redacted_thinking":
				if item := ReasoningItem(block); item != nil {
					output = append(output, item)
				}
			}
		}
	} else {
		choices := List(body["choices"])
		if len(choices) == 0 {
			return nil, fmt.Errorf("upstream returned no Chat Completions choices")
		}
		choice := Obj(choices[0])
		message := Obj(choice["message"])
		if message == nil {
			return nil, fmt.Errorf("upstream choice has no message")
		}
		reason = Str(choice["finish_reason"])
		usage = Usage(Obj(body["usage"]), false)
		reasoning := Str(message["reasoning_content"])
		if reasoning == "" {
			reasoning = Str(message["reasoning"])
		}
		if reasoning != "" {
			output = append(output, chatReasoning(reasoning))
		}
		if text := Text(message["content"]); text != "" {
			output = append(output, messageItem(text))
		}
		if text := Str(message["refusal"]); text != "" {
			output = append(output, Object{"id": ID("msg_"), "type": "message", "role": "assistant", "status": "completed", "content": []any{Object{"type": "refusal", "refusal": text}}})
		}
		calls := List(message["tool_calls"])
		if legacy := Obj(message["function_call"]); legacy != nil {
			calls = append(calls, Object{"id": ID("call_"), "function": legacy})
		}
		for _, raw := range calls {
			call := Obj(raw)
			f := Obj(call["function"])
			args := Str(f["arguments"])
			if args == "" {
				args = "{}"
			}
			if Str(call["id"]) == "" || Str(f["name"]) == "" {
				return nil, fmt.Errorf("upstream tool call is missing its identity")
			}
			if _, err := Decode([]byte(args)); err != nil {
				return nil, fmt.Errorf("upstream returned invalid tool arguments")
			}
			item := tools.Call(Str(call["id"]), Str(f["name"]), args, "completed")
			if reasoning != "" {
				item["reasoning_content"] = reasoning
			}
			output = append(output, item)
		}
	}
	response := Object{"id": ID("resp_"), "object": "response", "created_at": time.Now().Unix(), "model": model, "status": "completed", "output": output, "usage": usage}
	if id := Str(body["id"]); id != "" {
		response["id"] = "resp_" + strings.TrimPrefix(id, "resp_")
	}
	if reason == "length" || reason == "max_tokens" || reason == "model_context_window_exceeded" {
		response["status"] = "incomplete"
		response["incomplete_details"] = Object{"reason": "max_output_tokens"}
	}
	if reason == "refusal" || reason == "content_filter" {
		response["status"] = "incomplete"
		response["incomplete_details"] = Object{"reason": "content_filter"}
	}
	if len(output) == 0 {
		return nil, fmt.Errorf("upstream returned no usable output")
	}
	if tools.Compaction {
		return compactResponse(response)
	}
	return response, nil
}
func messageItem(text string) Object {
	return Object{"id": ID("msg_"), "type": "message", "status": "completed", "role": "assistant", "content": []any{Object{"type": "output_text", "text": text, "annotations": []any{}}}}
}
func chatReasoning(text string) Object {
	return Object{"id": ID("rs_"), "type": "reasoning", "summary": []any{Object{"type": "summary_text", "text": text}}, "encrypted_content": EncodeEnvelope(ChatReasoningPrefix, text)}
}
func compactResponse(response Object) (Object, error) {
	if response["status"] != "completed" {
		return response, nil
	}
	var text []string
	for _, raw := range List(response["output"]) {
		item := Obj(raw)
		if item["type"] == "message" {
			text = append(text, Text(item["content"]))
		}
	}
	summary := strings.TrimSpace(strings.Join(text, "\n"))
	if summary == "" {
		return nil, fmt.Errorf("upstream returned an empty compaction summary")
	}
	response["output"] = []any{Object{"type": "compaction", "id": ID("cmp_"), "encrypted_content": EncodeEnvelope(CompactionPrefix, summary)}}
	return response, nil
}
