package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/config"
)

func Response(body Object, protocol, model string) (Object, error) {
	if protocol == config.Anthropic {
		return body, nil
	}
	if e := Obj(body["error"]); len(e) > 0 {
		return nil, fmt.Errorf("upstream generation failed: %s", Str(e["message"]))
	}
	content := make([]any, 0)
	hasTools := false
	reason := ""
	if protocol == config.OpenAICompat {
		choices := List(body["choices"])
		if len(choices) == 0 {
			return nil, fmt.Errorf("upstream Chat Completions response has no choices")
		}
		choice := Obj(choices[0])
		message := Obj(choice["message"])
		if message == nil {
			return nil, fmt.Errorf("upstream Chat Completions response has no message")
		}
		if thinking := Str(message["reasoning_content"]); thinking != "" {
			content = append(content, Object{"type": "thinking", "thinking": thinking})
		}
		if text := Str(message["content"]); text != "" {
			content = append(content, Object{"type": "text", "text": text})
		}
		for _, part := range List(message["content"]) {
			b := Obj(part)
			if text := Str(b["text"]); text != "" {
				content = append(content, Object{"type": "text", "text": text})
			}
		}
		if refusal := Str(message["refusal"]); refusal != "" {
			content = append(content, Object{"type": "text", "text": refusal})
		}
		calls := List(message["tool_calls"])
		if legacy := Obj(message["function_call"]); len(calls) == 0 && legacy != nil {
			calls = []any{Object{"id": ID("call_"), "function": legacy}}
		}
		for _, value := range calls {
			call := Obj(value)
			f := Obj(call["function"])
			block, err := toolBlock(Str(call["id"]), Str(f["name"]), Str(f["arguments"]))
			if err != nil {
				return nil, err
			}
			content = append(content, block)
			hasTools = true
		}
		reason = Str(choice["finish_reason"])
	} else {
		status := Str(body["status"])
		if status == "failed" || status == "cancelled" {
			return nil, fmt.Errorf("upstream Responses generation %s", status)
		}
		if _, ok := body["output"].([]any); !ok {
			return nil, fmt.Errorf("upstream Responses response has no output array")
		}
		for _, value := range List(body["output"]) {
			item := Obj(value)
			switch Str(item["type"]) {
			case "message":
				for _, value := range List(item["content"]) {
					b := Obj(value)
					text := Str(b["text"])
					if b["type"] == "refusal" {
						text = Str(b["refusal"])
					}
					if text != "" {
						content = append(content, Object{"type": "text", "text": text})
					}
				}
			case "function_call":
				b, err := toolBlock(Str(item["call_id"]), Str(item["name"]), Str(item["arguments"]))
				if err != nil {
					return nil, err
				}
				content = append(content, b)
				hasTools = true
			case "reasoning":
				if block := ReasoningBlock(item); block != nil {
					content = append(content, block)
				}
			default:
				return nil, fmt.Errorf("unsupported Responses output item %q", Str(item["type"]))
			}
		}
		if status == "incomplete" {
			reason = Str(Obj(body["incomplete_details"])["reason"])
			if reason == "" {
				reason = "max_output_tokens"
			}
		}
	}
	id := Str(body["id"])
	if id == "" {
		id = ID("msg_")
	}
	return Object{"id": id, "type": "message", "role": "assistant", "model": model, "content": content, "stop_reason": stopReason(reason, hasTools), "stop_sequence": nil, "usage": Usage(Obj(body["usage"]))}, nil
}

func toolBlock(id, name, arguments string) (Object, error) {
	if id == "" || name == "" {
		return nil, fmt.Errorf("upstream tool call requires an id and name")
	}
	if arguments == "" {
		arguments = "{}"
	}
	var input Object
	if json.Unmarshal([]byte(arguments), &input) != nil || input == nil {
		return nil, fmt.Errorf("upstream tool arguments must be a JSON object")
	}
	return Object{"type": "tool_use", "id": id, "name": name, "input": input}, nil
}
