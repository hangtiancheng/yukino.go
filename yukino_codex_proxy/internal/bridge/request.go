package bridge

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/hangtiancheng/yukino.go/yukino_codex_proxy/internal/config"
)

const CompactPrompt = "Create a concise context checkpoint for another coding assistant to resume this task. Include progress, decisions, user constraints, remaining work, and critical references. Return only the handoff summary."

func Request(body Object, p config.Provider) (Object, *Tools, error) {
	if body["input"] == nil {
		return nil, nil, fmt.Errorf("input must be a string, message, or item array")
	}
	if _, ok := body["input"].(string); !ok && Obj(body["input"]) == nil && List(body["input"]) == nil {
		return nil, nil, fmt.Errorf("input must be a string, message, or item array")
	}
	if value, exists := body["max_output_tokens"]; exists && Number(value) <= 0 {
		return nil, nil, fmt.Errorf("max_output_tokens must be a positive integer")
	}
	body = Clone(body)
	body["model"] = p.Model
	tools, err := BuildTools(body)
	if err != nil && p.Protocol != config.OpenAI {
		return nil, nil, err
	}
	if tools == nil {
		tools = &Tools{Specs: map[string]ToolSpec{}}
		for _, raw := range InputItems(body["input"]) {
			if Obj(raw)["type"] == "compaction_trigger" {
				tools.Compaction = true
			}
		}
	}
	maxTokens := Number(body["max_output_tokens"])
	if maxTokens <= 0 {
		maxTokens = p.MaxOutputTokens
	}
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	if p.MaxOutputTokens > 0 {
		maxTokens = min(maxTokens, p.MaxOutputTokens)
	}
	if p.ContextWindow > 0 {
		maxTokens = min(maxTokens, p.ContextWindow)
	}
	effort := Str(Obj(body["reasoning"])["effort"])
	if effort == "" {
		effort = p.Thinking
	}
	if effort == "" {
		effort = "high"
	}
	if effort == "true" {
		effort = "high"
	}
	if effort == "off" || effort == "false" {
		effort = "none"
	}
	if p.Protocol == config.OpenAI {
		if p.MaxOutputTokens > 0 || body["max_output_tokens"] != nil {
			body["max_output_tokens"] = maxTokens
		}
		if body["reasoning"] == nil && p.Thinking != "" {
			body["reasoning"] = Object{"effort": effort}
		}
		items := InputItems(body["input"])
		out := []any{}
		for _, raw := range items {
			item := Obj(raw)
			if item["type"] == "compaction_trigger" {
				out = append(out, Object{"role": "user", "content": CompactPrompt})
				continue
			}
			if isCompaction(item) && strings.HasPrefix(Str(item["encrypted_content"]), CompactionPrefix) {
				if text := compactionText(item); text != "" {
					out = append(out, Object{"role": "user", "content": text})
					continue
				}
			}
			if item["type"] == "reasoning" && strings.HasPrefix(Str(item["encrypted_content"]), "yukino-") {
				if text := ReasoningText(item); text != "" {
					out = append(out, Object{"role": "assistant", "content": text})
				}
				continue
			}
			out = append(out, raw)
		}
		if tools.Compaction {
			delete(body, "tools")
			delete(body, "tool_choice")
			delete(body, "parallel_tool_calls")
			delete(body, "text")
		}
		if _, ok := body["input"].(string); !ok {
			body["input"] = out
		}
		return body, tools, nil
	}
	messages, systems, err := messagesFromInput(body, tools, p.Protocol == config.Anthropic)
	if err != nil {
		return nil, nil, err
	}
	result := Object{"model": p.Model, "messages": messages}
	for _, key := range []string{"stream", "temperature", "top_p"} {
		if value, ok := body[key]; ok {
			result[key] = value
		}
	}
	if p.Protocol == config.Anthropic {
		result["max_tokens"] = maxTokens
		if len(systems) > 0 {
			result["system"] = strings.Join(systems, "\n\n")
		}
		if effort != "none" || requiresThinking(p.Model) {
			budget := int64(8192)
			switch effort {
			case "minimal", "low":
				budget = 2048
			case "high":
				budget = 16384
			case "xhigh", "max":
				budget = 24576
			}
			if adaptiveThinking(p.Model) {
				result["thinking"] = Object{"type": "adaptive"}
				result["output_config"] = Object{"effort": anthropicEffort(p.Model, effort)}
			} else if budget = min(budget, maxTokens/2); budget >= 1024 {
				result["thinking"] = Object{"type": "enabled", "budget_tokens": budget}
			}
			if result["thinking"] != nil {
				delete(result, "temperature")
				delete(result, "top_p")
			}
		} else {
			result["thinking"] = Object{"type": "disabled"}
		}
		definitions := []any{}
		if !tools.Compaction {
			for _, raw := range tools.Definitions {
				tool := Obj(raw)
				definitions = append(definitions, Object{"name": tool["name"], "description": tool["description"], "input_schema": tool["parameters"]})
			}
		}
		if len(definitions) > 0 {
			result["tools"] = definitions
			if choice := toolChoice(body["tool_choice"], tools, true); choice != nil {
				result["tool_choice"] = choice
				if choice["type"] == "any" || choice["type"] == "tool" {
					if requiresThinking(p.Model) {
						return nil, nil, fmt.Errorf("this Anthropic model requires thinking and cannot honor forced tool selection; use auto")
					}
					result["thinking"] = Object{"type": "disabled"}
					delete(result, "output_config")
				}
			}
			if body["parallel_tool_calls"] == false {
				choice := Obj(result["tool_choice"])
				if choice == nil {
					choice = Object{"type": "auto"}
					result["tool_choice"] = choice
				}
				choice["disable_parallel_tool_use"] = true
			}
		}
	} else {
		if strings.HasPrefix(p.Model, "o") || strings.HasPrefix(p.Model, "gpt-5") {
			result["max_completion_tokens"] = maxTokens
		} else {
			result["max_tokens"] = maxTokens
		}
		if effort != "none" {
			result["reasoning_effort"] = effort
		}
		if strings.Contains(strings.ToLower(p.Model), "deepseek") {
			result["thinking"] = Object{"type": "disabled"}
			if effort != "none" {
				result["thinking"] = Object{"type": "enabled"}
			}
		}
		definitions := []any{}
		if !tools.Compaction {
			for _, raw := range tools.Definitions {
				tool := Clone(Obj(raw))
				delete(tool, "type")
				definitions = append(definitions, Object{"type": "function", "function": tool})
			}
		}
		if len(definitions) > 0 {
			result["tools"] = definitions
			if choice := toolChoice(body["tool_choice"], tools, false); choice != nil {
				if choice["type"] == "auto" || choice["type"] == "none" || choice["type"] == "required" {
					result["tool_choice"] = choice["type"]
				} else {
					result["tool_choice"] = choice
				}
			}
			if value, ok := body["parallel_tool_calls"]; ok {
				result["parallel_tool_calls"] = value
			}
		}
		if result["stream"] == true {
			result["stream_options"] = Object{"include_usage": true}
		}
		if format := Obj(Obj(body["text"])["format"]); format != nil && !tools.Compaction {
			f := Clone(format)
			if f["type"] == "json_schema" {
				delete(f, "type")
				result["response_format"] = Object{"type": "json_schema", "json_schema": f}
			} else {
				result["response_format"] = f
			}
		}
	}
	return result, tools, nil
}

func toolChoice(value any, tools *Tools, anthropic bool) Object {
	if value == nil {
		return nil
	}
	kind := Str(value)
	obj := Obj(value)
	if obj != nil {
		kind = Str(obj["type"])
	}
	if kind == "auto" || kind == "none" {
		return Object{"type": kind}
	}
	if kind == "required" {
		if anthropic {
			return Object{"type": "any"}
		}
		return Object{"type": "required"}
	}
	name := FlatName(Str(obj["namespace"]), Str(obj["name"]))
	if kind == "tool_search" {
		name = "tool_search"
	}
	if anthropic {
		return Object{"type": "tool", "name": name}
	}
	return Object{"type": "function", "function": Object{"name": name}}
}

func messagesFromInput(body Object, tools *Tools, anthropic bool) ([]any, []string, error) {
	messages := []any{}
	systems := []string{}
	if text := Str(body["instructions"]); text != "" {
		systems = append(systems, text)
		if !anthropic {
			messages = append(messages, Object{"role": "system", "content": text})
		}
	}
	var reasoning string
	appendBlock := func(role string, block Object) {
		if len(messages) > 0 {
			last := Obj(messages[len(messages)-1])
			if last["role"] == role {
				last["content"] = append(List(last["content"]), block)
				return
			}
		}
		messages = append(messages, Object{"role": role, "content": []any{block}})
	}
	for _, raw := range InputItems(body["input"]) {
		item := Obj(raw)
		if item == nil {
			return nil, nil, fmt.Errorf("input items must be objects")
		}
		kind := Str(item["type"])
		switch kind {
		case "reasoning":
			if anthropic {
				block := Obj(DecodeEnvelope(ThinkingPrefix, Str(item["encrypted_content"])))
				if block != nil {
					appendBlock("assistant", block)
				}
			} else {
				reasoning += ReasoningText(item)
			}
		case "function_call", "custom_tool_call", "tool_search_call":
			id := Str(item["call_id"])
			if id == "" {
				id = Str(item["id"])
			}
			name := FlatName(Str(item["namespace"]), Str(item["name"]))
			args := Str(item["arguments"])
			if kind == "custom_tool_call" {
				args = JSON(Object{"input": item["input"]})
			}
			if kind == "tool_search_call" {
				name = "tool_search"
				args = JSON(item["arguments"])
			}
			if args == "" {
				args = "{}"
			}
			input, err := Decode([]byte(args))
			if err != nil {
				return nil, nil, fmt.Errorf("tool call %q has invalid JSON object arguments", name)
			}
			if id == "" || name == "" {
				return nil, nil, fmt.Errorf("tool calls require call_id and name")
			}
			if anthropic {
				appendBlock("assistant", Object{"type": "tool_use", "id": id, "name": name, "input": input})
			} else {
				var assistant Object
				if len(messages) > 0 {
					last := Obj(messages[len(messages)-1])
					if last["role"] == "assistant" {
						assistant = last
					}
				}
				if assistant == nil {
					assistant = Object{"role": "assistant", "content": nil}
					messages = append(messages, assistant)
				}
				assistant["tool_calls"] = append(List(assistant["tool_calls"]), Object{"id": id, "type": "function", "function": Object{"name": name, "arguments": args}})
				if reasoning != "" {
					assistant["reasoning_content"] = reasoning
					reasoning = ""
				}
			}
		case "function_call_output", "custom_tool_call_output", "tool_search_output":
			id := Str(item["call_id"])
			if id == "" {
				return nil, nil, fmt.Errorf("tool outputs require call_id")
			}
			content := item["output"]
			if kind == "tool_search_output" && content == nil {
				content = JSON(Object{"tools": item["tools"]})
			}
			if content == nil {
				content = ""
			}
			if anthropic {
				if parts := List(content); parts != nil {
					converted, err := contentParts(parts, true)
					if err != nil {
						return nil, nil, err
					}
					content = converted
				} else if _, ok := content.(string); !ok {
					content = JSON(content)
				}
				block := Object{"type": "tool_result", "tool_use_id": id, "content": content}
				if item["is_error"] == true {
					block["is_error"] = true
				}
				appendBlock("user", block)
			} else {
				if _, ok := content.(string); !ok {
					content = JSON(content)
				}
				messages = append(messages, Object{"role": "tool", "tool_call_id": id, "content": content})
			}
		case "additional_tools":
		case "compaction_trigger", "compaction", "compaction_summary", "context_compaction":
			text := CompactPrompt
			if kind != "compaction_trigger" {
				text = compactionText(item)
			}
			if text != "" {
				if anthropic {
					appendBlock("user", Object{"type": "text", "text": text})
				} else {
					messages = append(messages, Object{"role": "user", "content": text})
				}
			}
		default:
			role := Str(item["role"])
			if role == "" {
				role = "user"
			}
			if role == "developer" {
				role = "system"
			}
			if role != "user" && role != "assistant" && role != "system" {
				return nil, nil, fmt.Errorf("unsupported message role %q", role)
			}
			content := item["content"]
			if kind == "input_text" || kind == "input_image" || kind == "input_file" {
				content = []any{item}
			}
			if content == nil {
				return nil, nil, fmt.Errorf("message content must be a string or content array")
			}
			if anthropic && role == "system" {
				systems = append(systems, Text(content))
				continue
			}
			if text, ok := content.(string); ok {
				if anthropic {
					appendBlock(role, Object{"type": "text", "text": text})
				} else {
					message := Object{"role": role, "content": text}
					if role == "assistant" && reasoning != "" {
						message["reasoning_content"] = reasoning
						reasoning = ""
					}
					messages = append(messages, message)
				}
				continue
			}
			parts, err := contentParts(List(content), anthropic)
			if err != nil {
				return nil, nil, err
			}
			if anthropic {
				for _, raw := range parts {
					appendBlock(role, Obj(raw))
				}
			} else {
				message := Object{"role": role, "content": parts}
				if role == "assistant" && reasoning != "" {
					message["reasoning_content"] = reasoning
					reasoning = ""
				}
				messages = append(messages, message)
			}
		}
	}
	if len(messages) == 0 {
		return nil, nil, fmt.Errorf("converted request has no messages")
	}
	if anthropic && Obj(messages[0])["role"] != "user" {
		messages = append([]any{Object{"role": "user", "content": []any{Object{"type": "text", "text": "Continue the conversation."}}}}, messages...)
	}
	return messages, systems, nil
}

func contentParts(parts []any, anthropic bool) ([]any, error) {
	out := []any{}
	for _, raw := range parts {
		part := Obj(raw)
		kind := Str(part["type"])
		switch kind {
		case "input_text", "output_text", "text":
			out = append(out, Object{"type": "text", "text": part["text"]})
		case "refusal":
			out = append(out, Object{"type": "text", "text": part["refusal"]})
		case "input_image":
			image := Str(part["image_url"])
			if image == "" {
				return nil, fmt.Errorf("images require image_url")
			}
			if anthropic {
				source, err := mediaSource(image)
				if err != nil {
					return nil, err
				}
				out = append(out, Object{"type": "image", "source": source})
			} else {
				out = append(out, Object{"type": "image_url", "image_url": Object{"url": image}})
			}
		case "input_file":
			if anthropic {
				source, err := mediaSource(Str(part["file_data"]))
				if err != nil {
					return nil, fmt.Errorf("Anthropic files require a data URL")
				}
				out = append(out, Object{"type": "document", "source": source})
			} else {
				file := Clone(part)
				delete(file, "type")
				out = append(out, Object{"type": "file", "file": file})
			}
		case "image", "document":
			if !anthropic {
				return nil, fmt.Errorf("unexpected Anthropic media in Responses input")
			}
			out = append(out, part)
		default:
			return nil, fmt.Errorf("unsupported content type %q", kind)
		}
	}
	return out, nil
}
func mediaSource(value string) (Object, error) {
	if strings.HasPrefix(value, "data:") {
		head, data, ok := strings.Cut(strings.TrimPrefix(value, "data:"), ",")
		if !ok || !strings.HasSuffix(head, ";base64") {
			return nil, fmt.Errorf("media data URL must use base64")
		}
		return Object{"type": "base64", "media_type": strings.TrimSuffix(head, ";base64"), "data": data}, nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("media URL must use HTTP(S) or base64 data")
	}
	return Object{"type": "url", "url": value}, nil
}
func isCompaction(item Object) bool {
	switch item["type"] {
	case "compaction", "compaction_summary", "context_compaction":
		return true
	}
	return false
}
func compactionText(item Object) string {
	if text := Str(DecodeEnvelope(CompactionPrefix, Str(item["encrypted_content"]))); text != "" {
		return "Previous conversation summary:\n" + text
	}
	return "[Previous conversation was compacted in a format this provider cannot read.]"
}
