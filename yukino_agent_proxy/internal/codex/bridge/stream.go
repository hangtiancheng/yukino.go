package bridge

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/config"
)

type streamItem struct {
	index                int
	item                 Object
	text, args, name, id string
	opened, closed       bool
	source               Object
}
type emitter struct {
	sink              Sink
	tools             *Tools
	id, model         string
	sequence          int
	started, finished bool
	items             []*streamItem
	text, reasoning   *streamItem
	calls             map[int]*streamItem
	usage             Object
}

func newEmitter(sink Sink, tools *Tools, model string) *emitter {
	return &emitter{sink: sink, tools: tools, id: ID("resp_"), model: model, calls: map[int]*streamItem{}, usage: Usage(nil, false)}
}
func (e *emitter) send(event string, data Object) error {
	data["type"] = event
	data["sequence_number"] = e.sequence
	e.sequence++
	return e.sink(event, data)
}
func (e *emitter) response(status string) Object {
	output := []any{}
	for _, item := range e.items {
		output = append(output, item.item)
	}
	return Object{"id": e.id, "object": "response", "created_at": time.Now().Unix(), "status": status, "model": e.model, "output": output, "usage": e.usage}
}
func (e *emitter) start() error {
	if e.started {
		return nil
	}
	e.started = true
	if err := e.send("response.created", Object{"response": e.response("in_progress")}); err != nil {
		return err
	}
	return e.send("response.in_progress", Object{"response": e.response("in_progress")})
}
func (e *emitter) add(item Object) (*streamItem, error) {
	if err := e.start(); err != nil {
		return nil, err
	}
	s := &streamItem{index: len(e.items), item: item, opened: true}
	e.items = append(e.items, s)
	return s, e.send("response.output_item.added", Object{"output_index": s.index, "item": Clone(item)})
}
func (e *emitter) textDelta(reasoning bool, text string) error {
	if text == "" {
		return nil
	}
	slot := &e.text
	if reasoning {
		slot = &e.reasoning
	}
	if *slot == nil {
		item := messageItem("")
		item["status"] = "in_progress"
		if reasoning {
			item = Object{"id": ID("rs_"), "type": "reasoning", "summary": []any{Object{"type": "summary_text", "text": ""}}}
		}
		s, err := e.add(item)
		if err != nil {
			return err
		}
		*slot = s
		if !reasoning {
			if err := e.send("response.content_part.added", Object{"item_id": item["id"], "output_index": s.index, "content_index": 0, "part": Object{"type": "output_text", "text": "", "annotations": []any{}}}); err != nil {
				return err
			}
		} else {
			if err := e.send("response.reasoning_summary_part.added", Object{"item_id": item["id"], "output_index": s.index, "summary_index": 0, "part": Object{"type": "summary_text", "text": ""}}); err != nil {
				return err
			}
		}
	}
	s := *slot
	s.text += text
	event := "response.output_text.delta"
	indexKey := "content_index"
	if reasoning {
		event = "response.reasoning_summary_text.delta"
		indexKey = "summary_index"
	}
	return e.send(event, Object{"item_id": s.item["id"], "output_index": s.index, indexKey: 0, "delta": text})
}
func (e *emitter) toolDelta(key int, id, name, args string) error {
	s := e.calls[key]
	if s == nil {
		s = &streamItem{index: -1}
		e.calls[key] = s
	}
	if id != "" {
		s.id = id
	}
	if name != "" {
		s.name += name
	}
	s.args += args
	if !s.opened && s.id != "" && s.name != "" {
		item := e.tools.Call(s.id, s.name, "{}", "in_progress")
		if item["type"] == "function_call" {
			item["arguments"] = ""
		}
		if item["type"] == "custom_tool_call" {
			item["input"] = ""
		}
		opened, err := e.add(item)
		if err != nil {
			return err
		}
		s.index = opened.index
		s.item = opened.item
		s.opened = true
		e.items[s.index] = s
		args = s.args
	}
	if s.opened && s.item["type"] == "function_call" && args != "" {
		return e.send("response.function_call_arguments.delta", Object{"item_id": s.item["id"], "output_index": s.index, "delta": args})
	}
	return nil
}
func (e *emitter) close(s *streamItem, complete bool) error {
	if s.closed {
		return nil
	}
	s.closed = true
	kind := s.item["type"]
	if kind == "message" {
		s.item["content"] = []any{Object{"type": "output_text", "text": s.text, "annotations": []any{}}}
		s.item["status"] = "completed"
		if err := e.send("response.output_text.done", Object{"item_id": s.item["id"], "output_index": s.index, "content_index": 0, "text": s.text}); err != nil {
			return err
		}
		if err := e.send("response.content_part.done", Object{"item_id": s.item["id"], "output_index": s.index, "content_index": 0, "part": Obj(List(s.item["content"])[0])}); err != nil {
			return err
		}
	} else if kind == "reasoning" {
		s.item["summary"] = []any{Object{"type": "summary_text", "text": s.text}}
		if s.source != nil {
			if item := ReasoningItem(s.source); item != nil {
				s.item["encrypted_content"] = item["encrypted_content"]
			}
		} else {
			s.item["encrypted_content"] = EncodeEnvelope(ChatReasoningPrefix, s.text)
		}
		if err := e.send("response.reasoning_summary_text.done", Object{"item_id": s.item["id"], "output_index": s.index, "summary_index": 0, "text": s.text}); err != nil {
			return err
		}
		if err := e.send("response.reasoning_summary_part.done", Object{"item_id": s.item["id"], "output_index": s.index, "summary_index": 0, "part": Object{"type": "summary_text", "text": s.text}}); err != nil {
			return err
		}
	} else {
		status := "completed"
		if !complete {
			status = "incomplete"
		}
		if s.args == "" {
			s.args = "{}"
		}
		if complete {
			if _, err := Decode([]byte(s.args)); err != nil {
				return fmt.Errorf("upstream returned invalid tool arguments")
			}
		}
		s.item = e.tools.Call(s.id, s.name, s.args, status)
		if e.reasoning != nil && e.reasoning.text != "" {
			s.item["reasoning_content"] = e.reasoning.text
		}
		if complete {
			event := "response.function_call_arguments.done"
			data := Object{"item_id": s.item["id"], "output_index": s.index, "arguments": s.args}
			if s.item["type"] == "custom_tool_call" {
				event = "response.custom_tool_call_input.done"
				delete(data, "arguments")
				data["input"] = s.item["input"]
				if err := e.send("response.custom_tool_call_input.delta", Object{"item_id": s.item["id"], "output_index": s.index, "delta": s.item["input"]}); err != nil {
					return err
				}
			}
			if err := e.send(event, data); err != nil {
				return err
			}
		}
	}
	return e.send("response.output_item.done", Object{"output_index": s.index, "item": s.item})
}
func (e *emitter) finish(reason string) error {
	if e.finished {
		return nil
	}
	if err := e.start(); err != nil {
		return err
	}
	complete := reason != "length" && reason != "max_tokens" && reason != "content_filter" && reason != "refusal" && reason != "model_context_window_exceeded"
	keys := []int{}
	for key := range e.calls {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	for _, key := range keys {
		s := e.calls[key]
		if !s.opened {
			return fmt.Errorf("upstream tool call is missing call_id or name")
		}
	}
	for _, s := range e.items {
		if err := e.close(s, complete); err != nil {
			return err
		}
	}
	response := e.response("completed")
	event := "response.completed"
	if !complete {
		response["status"] = "incomplete"
		why := "max_output_tokens"
		if reason == "refusal" || reason == "content_filter" {
			why = "content_filter"
		}
		response["incomplete_details"] = Object{"reason": why}
		event = "response.incomplete"
	}
	if e.tools.Compaction && complete {
		var err error
		response, err = compactResponse(response)
		if err != nil {
			return err
		}
		item := Obj(List(response["output"])[0])
		if err := e.send("response.output_item.done", Object{"output_index": len(e.items), "item": item}); err != nil {
			return err
		}
	}
	if len(e.items) == 0 {
		return fmt.Errorf("upstream returned an empty response")
	}
	e.finished = true
	return e.send(event, Object{"response": response})
}

// Stream converts incrementally; errors never become successful empty turns.
func Stream(r io.Reader, protocol, model string, tools *Tools, sink Sink) error {
	if protocol == config.OpenAI {
		return streamNative(r, tools, sink)
	}
	e := newEmitter(sink, tools, model)
	reason := ""
	terminal := false
	blocks := map[int]*streamItem{}
	nativeUsage := Object{}
	err := ReadSSE(r, func(event, data string) (bool, error) {
		if data == "[DONE]" {
			terminal = reason != ""
			return false, nil
		}
		m, err := Decode([]byte(data))
		if err != nil {
			return false, fmt.Errorf("invalid upstream SSE data")
		}
		kind := Str(m["type"])
		if kind == "" {
			kind = event
		}
		if m["error"] != nil || kind == "error" {
			return false, fmt.Errorf("upstream stream reported an error")
		}
		if protocol == config.OpenAICompat {
			if usage := Obj(m["usage"]); usage != nil {
				e.usage = Usage(usage, false)
			}
			choices := List(m["choices"])
			if len(choices) == 0 {
				return true, nil
			}
			choice := Obj(choices[0])
			delta := Obj(choice["delta"])
			text := Str(delta["reasoning_content"])
			if text == "" {
				text = Str(delta["reasoning"])
			}
			if err := e.textDelta(true, text); err != nil {
				return false, err
			}
			if err := e.textDelta(false, Str(delta["content"])); err != nil {
				return false, err
			}
			for _, raw := range List(delta["tool_calls"]) {
				call := Obj(raw)
				f := Obj(call["function"])
				if err := e.toolDelta(int(Number(call["index"])), Str(call["id"]), Str(f["name"]), Str(f["arguments"])); err != nil {
					return false, err
				}
			}
			if value := Str(choice["finish_reason"]); value != "" {
				reason = value
				terminal = true
			}
			return true, nil
		}
		switch kind {
		case "message_start":
			message := Obj(m["message"])
			if message == nil {
				return false, fmt.Errorf("invalid Anthropic message_start")
			}
			if id := Str(message["id"]); id != "" {
				e.id = "resp_" + id
			}
			for k, v := range Obj(message["usage"]) {
				nativeUsage[k] = v
			}
			e.usage = Usage(nativeUsage, true)
			return true, e.start()
		case "content_block_start":
			index := int(Number(m["index"]))
			b := Obj(m["content_block"])
			s := &streamItem{source: Clone(b)}
			blocks[index] = s
			switch b["type"] {
			case "tool_use":
				args := ""
				if input := Obj(b["input"]); len(input) > 0 {
					args = JSON(input)
				}
				return true, e.toolDelta(index, Str(b["id"]), Str(b["name"]), args)
			case "text":
				return true, e.textDelta(false, Str(b["text"]))
			case "thinking":
				if err := e.textDelta(true, Str(b["thinking"])); err != nil {
					return false, err
				}
				if e.reasoning == nil {
					opened, err := e.add(Object{"id": ID("rs_"), "type": "reasoning", "summary": []any{}})
					if err != nil {
						return false, err
					}
					e.reasoning = opened
					if err := e.send("response.reasoning_summary_part.added", Object{"item_id": opened.item["id"], "output_index": opened.index, "summary_index": 0, "part": Object{"type": "summary_text", "text": ""}}); err != nil {
						return false, err
					}
				}
				s.item = e.reasoning.item
				e.reasoning.source = s.source
			case "redacted_thinking":
				item := ReasoningItem(b)
				if item != nil {
					opened, err := e.add(item)
					if err != nil {
						return false, err
					}
					opened.source = b
					opened.closed = true
					if err := e.send("response.output_item.done", Object{"output_index": opened.index, "item": item}); err != nil {
						return false, err
					}
				}
			}
		case "content_block_delta":
			index := int(Number(m["index"]))
			d := Obj(m["delta"])
			switch d["type"] {
			case "text_delta":
				return true, e.textDelta(false, Str(d["text"]))
			case "thinking_delta":
				if s := blocks[index]; s != nil {
					s.source["thinking"] = Str(s.source["thinking"]) + Str(d["thinking"])
				}
				return true, e.textDelta(true, Str(d["thinking"]))
			case "signature_delta":
				if s := blocks[index]; s != nil {
					s.source["signature"] = Str(s.source["signature"]) + Str(d["signature"])
				}
			case "input_json_delta":
				return true, e.toolDelta(index, "", "", Str(d["partial_json"]))
			}
		case "content_block_stop":
			index := int(Number(m["index"]))
			if s := e.calls[index]; s != nil && s.opened {
				return true, e.close(s, true)
			}
			if s := blocks[index]; s != nil && s.source["type"] == "thinking" && e.reasoning != nil {
				err := e.close(e.reasoning, true)
				e.reasoning = nil
				return true, err
			}
			if s := blocks[index]; s != nil && s.source["type"] == "text" && e.text != nil {
				err := e.close(e.text, true)
				e.text = nil
				return true, err
			}
		case "message_delta":
			reason = Str(Obj(m["delta"])["stop_reason"])
			for k, v := range Obj(m["usage"]) {
				nativeUsage[k] = v
			}
			e.usage = Usage(nativeUsage, true)
		case "message_stop":
			terminal = reason != ""
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	if !terminal {
		return fmt.Errorf("upstream stream ended before its terminal event")
	}
	return e.finish(reason)
}

func ResponseSSE(response Object, sink Sink) error {
	sequence := 0
	send := func(event string, data Object) error {
		data["type"] = event
		data["sequence_number"] = sequence
		sequence++
		return sink(event, data)
	}
	start := Clone(response)
	start["status"] = "in_progress"
	start["output"] = []any{}
	if err := send("response.created", Object{"response": start}); err != nil {
		return err
	}
	if err := send("response.in_progress", Object{"response": start}); err != nil {
		return err
	}
	for i, raw := range List(response["output"]) {
		item := Obj(raw)
		if err := send("response.output_item.added", Object{"output_index": i, "item": item}); err != nil {
			return err
		}
		if item["type"] == "message" {
			if err := send("response.output_text.delta", Object{"item_id": item["id"], "output_index": i, "content_index": 0, "delta": Text(item["content"])}); err != nil {
				return err
			}
		}
		if err := send("response.output_item.done", Object{"output_index": i, "item": item}); err != nil {
			return err
		}
	}
	event := "response.completed"
	if response["status"] == "incomplete" {
		event = "response.incomplete"
	}
	if response["status"] == "failed" {
		event = "response.failed"
	}
	return send(event, Object{"response": response})
}
func streamNative(r io.Reader, tools *Tools, sink Sink) error {
	terminal := false
	err := ReadSSE(r, func(event, data string) (bool, error) {
		if data == "[DONE]" {
			return false, nil
		}
		m, err := Decode([]byte(data))
		if err != nil {
			return false, fmt.Errorf("invalid Responses SSE data")
		}
		if event == "" {
			event = Str(m["type"])
		}
		if event == "response.failed" || event == "error" {
			return false, fmt.Errorf("upstream Responses stream failed")
		}
		if event == "response.completed" || event == "response.incomplete" {
			if _, err := Response(Obj(m["response"]), config.OpenAI, "", &Tools{}); err != nil {
				return false, err
			}
			terminal = true
			if tools.Compaction && event == "response.completed" {
				response, err := compactResponse(Obj(m["response"]))
				if err != nil {
					return false, err
				}
				item := Obj(List(response["output"])[0])
				if err := sink("response.output_item.done", Object{"type": "response.output_item.done", "output_index": len(List(Obj(m["response"])["output"])), "item": item}); err != nil {
					return false, err
				}
				m["response"] = response
			}
		}
		return !terminal, sink(event, m)
	})
	if err != nil {
		return err
	}
	if !terminal {
		return fmt.Errorf("Responses stream ended before its terminal event")
	}
	return nil
}
