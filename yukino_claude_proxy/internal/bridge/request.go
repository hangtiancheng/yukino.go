package bridge

import (
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/config"
)

func Request(body Object, p config.Provider) (Object, error) {
	if len(List(body["messages"])) == 0 {
		return nil, fmt.Errorf("messages must be a nonempty array")
	}
	if Number(body["max_tokens"]) < 1 {
		return nil, fmt.Errorf("max_tokens must be a positive integer")
	}
	if n, ok := body["max_tokens"].(float64); ok && (math.IsInf(n, 0) || n != math.Trunc(n)) {
		return nil, fmt.Errorf("max_tokens must be a positive integer")
	}
	for _, message := range List(body["messages"]) {
		m := Obj(message)
		// Claude Code also appends mid-conversation system instructions. Keep
		// their role and position when converting to either OpenAI protocol.
		if m["role"] != "user" && m["role"] != "assistant" && m["role"] != "system" {
			return nil, fmt.Errorf("message role must be user, assistant, or system")
		}
		if _, ok := m["content"].(string); !ok && List(m["content"]) == nil {
			return nil, fmt.Errorf("message content must be a string or block array")
		}
	}
	body["model"] = p.Model
	if p.MaxOutputTokens > 0 && Number(body["max_tokens"]) > p.MaxOutputTokens {
		body["max_tokens"] = p.MaxOutputTokens
	}
	if p.Protocol == config.Anthropic {
		return body, nil
	}
	if p.Protocol == config.OpenAI {
		return responsesRequest(body, p)
	}
	return chatRequest(body, p)
}

func systemText(system any) string {
	text := Text(system)
	// Claude Code's changing billing header is not an instruction for the model.
	if strings.HasPrefix(text, "x-anthropic-billing-header:") {
		if index := strings.IndexByte(text, '\n'); index >= 0 {
			text = text[index+1:]
		} else {
			text = ""
		}
	}
	return text
}

func params(body Object) Object {
	result := Object{"model": body["model"]}
	for _, key := range []string{"stream", "temperature", "top_p"} {
		if v, ok := body[key]; ok {
			result[key] = v
		}
	}
	return result
}

func tools(body Object, responses bool) ([]any, error) {
	var result []any
	for _, value := range List(body["tools"]) {
		t := Obj(value)
		kind := Str(t["type"])
		if kind == "BatchTool" {
			continue
		}
		if kind != "" && kind != "custom" {
			return nil, fmt.Errorf("hosted Anthropic tool %q is not supported by this protocol bridge", kind)
		}
		if Str(t["name"]) == "" {
			return nil, fmt.Errorf("tool name must be nonempty")
		}
		schema := t["input_schema"]
		if schema == nil {
			schema = Object{"type": "object", "properties": Object{}}
		}
		function := Object{"name": t["name"], "parameters": schema}
		if description, ok := t["description"].(string); ok {
			function["description"] = description
		}
		if responses {
			function["type"] = "function"
			result = append(result, function)
		} else {
			result = append(result, Object{"type": "function", "function": function})
		}
	}
	return result, nil
}

func toolChoice(body, result Object, responses bool) {
	choice, exists := body["tool_choice"]
	if !exists {
		return
	}
	t := Obj(choice)
	kind := Str(t["type"])
	if kind == "" {
		kind = Str(choice)
	}
	switch kind {
	case "auto", "none":
		result["tool_choice"] = kind
	case "any":
		result["tool_choice"] = "required"
	case "tool":
		if responses {
			result["tool_choice"] = Object{"type": "function", "name": t["name"]}
		} else {
			result["tool_choice"] = Object{"type": "function", "function": Object{"name": t["name"]}}
		}
	}
	if disable, ok := t["disable_parallel_tool_use"].(bool); ok {
		result["parallel_tool_calls"] = !disable
	}
}

func reasoningEffort(body Object) string {
	if effort := Str(Obj(body["output_config"])["effort"]); effort != "" {
		switch effort {
		case "low", "medium", "high", "xhigh":
			return effort
		case "max":
			return "high"
		}
	}
	thinking := Obj(body["thinking"])
	if thinking["type"] == "enabled" || thinking["type"] == "adaptive" {
		budget := Number(thinking["budget_tokens"])
		if budget > 0 && budget < 4000 {
			return "low"
		}
		if budget > 0 && budget < 10000 {
			return "medium"
		}
		return "high"
	}
	return ""
}

func supportsEffort(model string) bool {
	model = strings.ToLower(model)
	return strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") || strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "gpt-6")
}

func effortFor(body Object, p config.Provider) string {
	if body["thinking"] != nil || Str(Obj(body["output_config"])["effort"]) != "" {
		return reasoningEffort(body)
	}
	switch strings.ToLower(p.Thinking) {
	case "low", "medium", "high", "xhigh":
		return strings.ToLower(p.Thinking)
	case "true", "enabled", "adaptive", "max":
		return "high"
	}
	return ""
}

func chatRequest(body Object, p config.Provider) (Object, error) {
	result := params(body)
	result["max_tokens"] = body["max_tokens"]
	if supportsEffort(p.Model) {
		delete(result, "max_tokens")
		result["max_completion_tokens"] = body["max_tokens"]
	}
	if stops := body["stop_sequences"]; stops != nil {
		result["stop"] = stops
	}
	if body["stream"] == true {
		result["stream_options"] = Object{"include_usage": true}
	}
	if effort := effortFor(body, p); effort != "" && supportsEffort(p.Model) {
		result["reasoning_effort"] = effort
	}
	var messages []any
	if system := systemText(body["system"]); system != "" {
		messages = append(messages, Object{"role": "system", "content": system})
	}
	preserveThinking := strings.Contains(strings.ToLower(p.BaseURL+" "+p.Model), "deepseek") || strings.Contains(strings.ToLower(p.BaseURL+" "+p.Model), "mimo")
	for _, value := range List(body["messages"]) {
		m := Obj(value)
		var content, calls, media []any
		var thinking []string
		for _, value := range Blocks(m["content"]) {
			b := Obj(value)
			switch Str(b["type"]) {
			case "text":
				content = append(content, Object{"type": "text", "text": b["text"]})
			case "image":
				u, err := imageURL(b)
				if err != nil {
					return nil, err
				}
				content = append(content, Object{"type": "image_url", "image_url": Object{"url": u}})
			case "tool_use":
				if Str(b["id"]) == "" || Str(b["name"]) == "" {
					return nil, fmt.Errorf("tool_use requires id and name")
				}
				input := b["input"]
				if input == nil {
					input = Object{}
				}
				calls = append(calls, Object{"id": b["id"], "type": "function", "function": Object{"name": b["name"], "arguments": JSON(input)}})
			case "tool_result":
				if Str(b["tool_use_id"]) == "" {
					return nil, fmt.Errorf("tool_result requires tool_use_id")
				}
				output := Text(b["content"])
				for _, part := range List(b["content"]) {
					if block := Obj(part); block["type"] == "image" {
						u, err := imageURL(block)
						if err != nil {
							return nil, err
						}
						media = append(media, Object{"type": "text", "text": "Image from tool " + Str(b["tool_use_id"])}, Object{"type": "image_url", "image_url": Object{"url": u}})
					}
				}
				if output == "" && b["content"] != nil && len(media) == 0 {
					output = JSON(b["content"])
				}
				messages = append(messages, Object{"role": "tool", "tool_call_id": b["tool_use_id"], "content": output})
			case "thinking":
				thinking = append(thinking, Str(b["thinking"]))
			case "redacted_thinking":
				if preserveThinking {
					thinking = append(thinking, "[redacted thinking]")
				}
			case "document":
				source := Obj(b["source"])
				if source["type"] != "text" {
					return nil, fmt.Errorf("Chat Completions document inputs require a text source; use the openai protocol for PDF inputs")
				}
				content = append(content, Object{"type": "text", "text": source["data"]})
			default:
				return nil, fmt.Errorf("unsupported Anthropic content block %q", Str(b["type"]))
			}
		}
		if len(media) > 0 {
			messages = append(messages, Object{"role": "user", "content": media})
		}
		if len(content) > 0 || len(calls) > 0 || (preserveThinking && len(thinking) > 0) {
			msg := Object{"role": m["role"], "content": nil}
			if len(content) == 1 && Obj(content[0])["type"] == "text" {
				msg["content"] = Obj(content[0])["text"]
			} else if len(content) > 0 {
				msg["content"] = content
			}
			if len(calls) > 0 {
				msg["tool_calls"] = calls
			}
			if preserveThinking && m["role"] == "assistant" {
				text := strings.Join(thinking, "\n")
				if text == "" && len(calls) > 0 {
					text = "tool call"
				}
				msg["reasoning_content"] = text
			}
			messages = append(messages, msg)
		}
	}
	result["messages"] = messages
	ts, err := tools(body, false)
	if err != nil {
		return nil, err
	}
	if len(ts) > 0 {
		result["tools"] = ts
	}
	toolChoice(body, result, false)
	chatThinking(body, result, p)
	return result, nil
}

func thinkingMode(body Object, p config.Provider) (enabled, specified bool) {
	switch Str(Obj(body["thinking"])["type"]) {
	case "enabled", "adaptive":
		return true, true
	case "disabled":
		return false, true
	}
	switch strings.ToLower(p.Thinking) {
	case "true", "enabled", "adaptive", "low", "medium", "high", "xhigh", "max":
		return true, true
	case "false", "disabled", "off", "none":
		return false, true
	}
	return false, false
}

func chatThinking(body, result Object, p config.Provider) {
	u, _ := url.Parse(p.BaseURL)
	if u == nil {
		return
	}
	host := strings.ToLower(u.Hostname())
	deepseek := host == "api.deepseek.com" || strings.HasSuffix(host, ".deepseek.com")
	qwen := strings.HasPrefix(strings.ToLower(p.Model), "qwen") && (strings.HasSuffix(host, ".aliyuncs.com") || strings.HasSuffix(host, ".qwencloud.com"))
	if !deepseek && !qwen {
		return
	}
	enabled, specified := thinkingMode(body, p)
	// These providers reject forced tool choices while thinking is enabled.
	// Preserve the caller's tool selection by disabling thinking for this request.
	if result["tool_choice"] == "required" || Obj(result["tool_choice"]) != nil {
		enabled, specified = false, true
	}
	if deepseek {
		if specified {
			mode := "disabled"
			if enabled {
				mode = "enabled"
			}
			result["thinking"] = Object{"type": mode}
		}
		if enabled {
			if effort := effortFor(body, p); effort != "" {
				result["reasoning_effort"] = effort
			}
		} else {
			delete(result, "reasoning_effort")
		}
	} else if specified {
		result["enable_thinking"] = enabled
	}
}

func responsesRequest(body Object, p config.Provider) (Object, error) {
	result := params(body)
	result["max_output_tokens"] = max(int64(16), Number(body["max_tokens"]))
	result["store"] = false
	result["include"] = []string{"reasoning.encrypted_content"}
	if system := systemText(body["system"]); system != "" {
		result["instructions"] = system
	}
	if effort := effortFor(body, p); effort != "" && supportsEffort(p.Model) {
		result["reasoning"] = Object{"effort": effort, "summary": "auto"}
	}
	var input []any
	for _, value := range List(body["messages"]) {
		m := Obj(value)
		var content []any
		start := len(input)
		flush := func() {
			if len(content) > 0 {
				input = append(input, Object{"role": m["role"], "content": content})
				content = nil
			}
		}
		for _, value := range Blocks(m["content"]) {
			b := Obj(value)
			switch Str(b["type"]) {
			case "text":
				kind := "input_text"
				if m["role"] == "assistant" {
					kind = "output_text"
				}
				content = append(content, Object{"type": kind, "text": b["text"]})
			case "image":
				u, err := imageURL(b)
				if err != nil {
					return nil, err
				}
				content = append(content, Object{"type": "input_image", "image_url": u})
			case "document":
				s := Obj(b["source"])
				switch Str(s["type"]) {
				case "text":
					content = append(content, Object{"type": "input_text", "text": s["data"]})
				case "url":
					content = append(content, Object{"type": "input_file", "file_url": s["url"]})
				case "base64":
					content = append(content, Object{"type": "input_file", "filename": "document.pdf", "file_data": "data:" + Str(s["media_type"]) + ";base64," + Str(s["data"])})
				default:
					return nil, fmt.Errorf("unsupported document source")
				}
			case "tool_use":
				flush()
				if Str(b["id"]) == "" || Str(b["name"]) == "" {
					return nil, fmt.Errorf("tool_use requires id and name")
				}
				args := b["input"]
				if args == nil {
					args = Object{}
				}
				input = append(input, Object{"type": "function_call", "call_id": b["id"], "name": b["name"], "arguments": JSON(args)})
			case "tool_result":
				flush()
				if Str(b["tool_use_id"]) == "" {
					return nil, fmt.Errorf("tool_result requires tool_use_id")
				}
				output := b["content"]
				if output == nil {
					output = ""
				}
				if parts := List(output); parts != nil {
					var converted []any
					for _, part := range parts {
						block := Obj(part)
						switch Str(block["type"]) {
						case "text":
							converted = append(converted, Object{"type": "input_text", "text": block["text"]})
						case "image":
							u, err := imageURL(block)
							if err != nil {
								return nil, err
							}
							converted = append(converted, Object{"type": "input_image", "image_url": u})
						default:
							return nil, fmt.Errorf("unsupported Responses tool output block %q", Str(block["type"]))
						}
					}
					output = converted
				}
				input = append(input, Object{"type": "function_call_output", "call_id": b["tool_use_id"], "output": output})
			case "thinking", "redacted_thinking":
				if item := replayReasoning(b); item != nil {
					flush()
					input = append(input, item)
				}
			default:
				return nil, fmt.Errorf("unsupported Anthropic content block %q", Str(b["type"]))
			}
		}
		flush()
		// A reasoning item must have a following assistant message or function call.
		if m["role"] == "assistant" {
			follower := false
			for i := len(input) - 1; i >= start; i-- {
				item := Obj(input[i])
				if item["type"] == "reasoning" && !follower {
					input = append(input[:i], input[i+1:]...)
				} else if item["role"] == "assistant" || item["type"] == "function_call" {
					follower = true
				}
			}
		}
	}
	result["input"] = input
	ts, err := tools(body, true)
	if err != nil {
		return nil, err
	}
	if len(ts) > 0 {
		result["tools"] = ts
	}
	toolChoice(body, result, true)
	return result, nil
}
