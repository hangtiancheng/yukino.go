package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func ResponsesStream(r io.Reader, model string, sink Sink) error {
	e := newEmitter(sink, model)
	aliases := make(map[string]string)
	textSeen := make(map[string]bool)
	reasoningText := make(map[string]string)
	reasoningDone := make(map[string]bool)
	keyFor := func(data, item Object) string {
		id := Str(data["item_id"])
		if id == "" {
			id = Str(item["id"])
		}
		index := fmt.Sprint(Number(data["output_index"]))
		if key := aliases[id]; key != "" {
			return key
		}
		if key := aliases["index:"+index]; key != "" {
			if id != "" {
				aliases[id] = key
			}
			return key
		}
		key := "index:" + index
		if id != "" {
			key = id
			aliases[id] = key
		}
		aliases["index:"+index] = key
		return key
	}
	textKey := func(data, item Object, index int) string {
		return fmt.Sprintf("%s:content:%d", keyFor(data, item), index)
	}
	emitMessage := func(data, item Object) error {
		for i, part := range List(item["content"]) {
			key := textKey(data, item, i)
			if textSeen[key] {
				continue
			}
			b := Obj(part)
			text := Str(b["text"])
			if b["type"] == "refusal" {
				text = Str(b["refusal"])
			}
			if err := e.text("text", text); err != nil {
				return err
			}
			textSeen[key] = true
		}
		return nil
	}
	emitReasoning := func(key string, item Object) error {
		if reasoningDone[key] {
			return nil
		}
		reasoningDone[key] = true
		if len(List(item["summary"])) == 0 && reasoningText[key] != "" {
			item["summary"] = []any{Object{"type": "summary_text", "text": reasoningText[key]}}
		}
		return e.thinkingItem(item)
	}
	err := ReadSSE(r, func(event, data string) (bool, error) {
		if data == "[DONE]" {
			return false, nil
		}
		var value Object
		if json.Unmarshal([]byte(data), &value) != nil {
			return false, fmt.Errorf("upstream Responses stream contains invalid JSON")
		}
		if event == "" {
			event = Str(value["type"])
		}
		response := Obj(value["response"])
		switch event {
		case "response.created":
			if id := Str(response["id"]); !e.started && id != "" {
				e.id = id
			}
			return true, e.start()
		case "response.output_text.delta", "response.refusal.delta":
			textSeen[textKey(value, nil, int(Number(value["content_index"])))] = true
			return true, e.text("text", Str(value["delta"]))
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			reasoningText[keyFor(value, nil)] += Str(value["delta"])
			return true, nil
		case "response.output_item.added":
			item := Obj(value["item"])
			if item["type"] == "function_call" {
				key := keyFor(value, item)
				return true, e.tool(key, Str(item["call_id"]), Str(item["name"]), Str(item["arguments"]), false)
			}
		case "response.function_call_arguments.delta":
			key := keyFor(value, nil)
			return true, e.tool(key, Str(value["call_id"]), Str(value["name"]), Str(value["delta"]), false)
		case "response.function_call_arguments.done":
			key := keyFor(value, nil)
			// Some providers supply the name and call_id only in output_item.done.
			if t := e.tools[key]; t != nil && t.id != "" && t.name != "" {
				return true, e.tool(key, "", "", Str(value["arguments"]), true)
			}
		case "response.output_item.done":
			item := Obj(value["item"])
			key := keyFor(value, item)
			switch Str(item["type"]) {
			case "function_call":
				return true, e.tool(key, Str(item["call_id"]), Str(item["name"]), Str(item["arguments"]), true)
			case "reasoning":
				return true, emitReasoning(key, item)
			case "message":
				return true, emitMessage(value, item)
			}

		case "response.failed", "response.cancelled", "error":
			upstreamError := Obj(value["error"])
			if upstreamError == nil {
				upstreamError = Obj(response["error"])
			}
			return false, fmt.Errorf("upstream Responses stream failed: %s", Str(upstreamError["message"]))
		case "response.completed", "response.incomplete":
			if response["status"] == "failed" || response["status"] == "cancelled" || Obj(response["error"]) != nil {
				return false, fmt.Errorf("upstream Responses generation failed")
			}
			for i, v := range List(response["output"]) {
				item := Obj(v)
				data := Object{"output_index": int64(i)}
				key := keyFor(data, item)
				switch Str(item["type"]) {
				case "function_call":
					if err := e.tool(key, Str(item["call_id"]), Str(item["name"]), Str(item["arguments"]), true); err != nil {
						return false, err
					}
				case "reasoning":
					if err := emitReasoning(key, item); err != nil {
						return false, err
					}
				case "message":
					if err := emitMessage(data, item); err != nil {
						return false, err
					}

				default:
					return false, fmt.Errorf("unsupported Responses output item %q", Str(item["type"]))
				}
			}
			e.usage = Usage(Obj(response["usage"]))
			reason := ""
			if event == "response.incomplete" {
				reason = Str(Obj(response["incomplete_details"])["reason"])
				if reason == "" {
					reason = "max_output_tokens"
				}
			}
			return false, e.finish(reason)
		case "response.output_text.done":
			key := textKey(value, nil, int(Number(value["content_index"])))
			if !textSeen[key] {
				textSeen[key] = true
				return true, e.text("text", Str(value["text"]))
			}
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	if !e.finished {
		return fmt.Errorf("upstream Responses stream was truncated before a terminal response")
	}
	return nil
}

// Stream also accepts gateways that return one JSON document for stream:true.
func Stream(r io.Reader, protocol, model string, sink Sink) error {
	reader := bufio.NewReader(r)
	for {
		b, err := reader.Peek(1)
		if err != nil {
			return fmt.Errorf("empty upstream response: %w", err)
		}
		if strings.ContainsRune(" \t\r\n", rune(b[0])) {
			_, _ = reader.ReadByte()
			continue
		}
		break
	}
	first, _ := reader.Peek(1)
	if first[0] == '{' {
		var body Object
		if err := json.NewDecoder(io.LimitReader(reader, 32*1024*1024)).Decode(&body); err != nil {
			return fmt.Errorf("invalid upstream JSON response")
		}
		message, err := Response(body, protocol, model)
		if err != nil {
			return err
		}
		return MessageSSE(message, sink)
	}
	if protocol == "openai" {
		return ResponsesStream(reader, model, sink)
	}
	if protocol == "openai-compat" {
		return ChatStream(reader, model, sink)
	}
	stopped := false
	err := ReadSSE(reader, func(event, data string) (bool, error) {
		var value Object
		if json.Unmarshal([]byte(data), &value) != nil {
			return false, fmt.Errorf("invalid Anthropic stream event")
		}
		if event == "" {
			event = Str(value["type"])
		}
		if event == "error" {
			return false, fmt.Errorf("upstream Anthropic stream failed: %s", Str(Obj(value["error"])["message"]))
		}
		stopped = event == "message_stop"
		return !stopped, sink(event, value)
	})
	if err == nil && !stopped {
		return fmt.Errorf("upstream Anthropic stream was truncated before message_stop")
	}
	return err
}
