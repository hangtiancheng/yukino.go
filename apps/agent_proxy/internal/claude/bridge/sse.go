package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func ReadSSE(r io.Reader, handle func(event, data string) (bool, error)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var event string
	var data []string
	dispatch := func() (bool, error) {
		if len(data) == 0 {
			event = ""
			return true, nil
		}
		more, err := handle(event, strings.Join(data, "\n"))
		event = ""
		data = nil
		return more, err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			more, err := dispatch()
			if err != nil || !more {
				return err
			}
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read upstream stream: %w", err)
	}
	_, err := dispatch()
	return err
}

type toolState struct {
	id, name, args string
	ready, closed  bool
}

type emitter struct {
	sink              Sink
	id, model         string
	started, finished bool
	next              int
	textIndex         int
	textKind          string
	tools             map[string]*toolState
	order             []*toolState
	usage             Object
}

func newEmitter(sink Sink, model string) *emitter {
	return &emitter{sink: sink, id: ID("msg_"), model: model, textIndex: -1, tools: make(map[string]*toolState), usage: Usage(nil)}
}

func (e *emitter) send(event string, payload Object) error {
	payload["type"] = event
	return e.sink(event, payload)
}
func (e *emitter) start() error {
	if e.started {
		return nil
	}
	e.started = true
	return e.send("message_start", Object{"message": Object{"id": e.id, "type": "message", "role": "assistant", "model": e.model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": Usage(nil)}})
}
func (e *emitter) closeText() error {
	if e.textIndex < 0 {
		return nil
	}
	index := e.textIndex
	e.textIndex = -1
	e.textKind = ""
	return e.send("content_block_stop", Object{"index": index})
}
func (e *emitter) text(kind, text string) error {
	if text == "" {
		return nil
	}
	if err := e.start(); err != nil {
		return err
	}
	if e.textKind != kind {
		if err := e.closeText(); err != nil {
			return err
		}
		e.textIndex = e.next
		e.next++
		e.textKind = kind
		block := Object{"type": kind}
		if kind == "thinking" {
			block["thinking"] = ""
		} else {
			block["text"] = ""
		}
		if err := e.send("content_block_start", Object{"index": e.textIndex, "content_block": block}); err != nil {
			return err
		}
	}
	delta := Object{"type": "text_delta", "text": text}
	if kind == "thinking" {
		delta = Object{"type": "thinking_delta", "thinking": text}
	}
	return e.send("content_block_delta", Object{"index": e.textIndex, "delta": delta})
}
func (e *emitter) thinkingItem(item Object) error {
	block := ReasoningBlock(item)
	if block == nil {
		return nil
	}
	if err := e.start(); err != nil {
		return err
	}
	if block["type"] == "redacted_thinking" {
		if err := e.closeText(); err != nil {
			return err
		}
		index := e.next
		e.next++
		if err := e.send("content_block_start", Object{"index": index, "content_block": block}); err != nil {
			return err
		}
		return e.send("content_block_stop", Object{"index": index})
	}
	if e.textKind != "thinking" {
		if err := e.text("thinking", Str(block["thinking"])); err != nil {
			return err
		}
	}
	if signature := Str(block["signature"]); signature != "" {
		if err := e.send("content_block_delta", Object{"index": e.textIndex, "delta": Object{"type": "signature_delta", "signature": signature}}); err != nil {
			return err
		}
	}
	return e.closeText()
}

func (e *emitter) tool(key, id, name, args string, complete bool) error {
	if err := e.start(); err != nil {
		return err
	}
	if err := e.closeText(); err != nil {
		return err
	}
	t := e.tools[key]
	if t == nil {
		t = &toolState{}
		e.tools[key] = t
		e.order = append(e.order, t)
	}
	if t.closed {
		return nil
	}
	if id != "" {
		t.id = id
	}
	if name != "" {
		t.name = name
	}
	if complete {
		if args == "" {
			args = "{}"
		}
		if !strings.HasPrefix(args, t.args) {
			return fmt.Errorf("upstream replaced previously streamed tool arguments")
		}
		args = strings.TrimPrefix(args, t.args)
	}
	t.args += args
	t.ready = complete
	for _, pending := range e.order {
		if pending.closed {
			continue
		}
		if !pending.ready {
			break
		}
		if err := e.emitTool(pending); err != nil {
			return err
		}
	}
	return nil
}

func (e *emitter) emitTool(t *toolState) error {
	block, err := toolBlock(t.id, t.name, t.args)
	if err != nil {
		return err
	}
	index := e.next
	e.next++
	if err := e.send("content_block_start", Object{"index": index, "content_block": Object{"type": "tool_use", "id": t.id, "name": t.name, "input": Object{}}}); err != nil {
		return err
	}
	if err := e.send("content_block_delta", Object{"index": index, "delta": Object{"type": "input_json_delta", "partial_json": JSON(block["input"])}}); err != nil {
		return err
	}
	t.closed = true
	return e.send("content_block_stop", Object{"index": index})
}
func (e *emitter) finish(reason string) error {
	if e.finished {
		return nil
	}
	if err := e.start(); err != nil {
		return err
	}
	if err := e.closeText(); err != nil {
		return err
	}
	for _, t := range e.order {
		if !t.closed {
			if err := e.emitTool(t); err != nil {
				return err
			}
		}
	}
	if err := e.send("message_delta", Object{"delta": Object{"stop_reason": stopReason(reason, len(e.order) > 0), "stop_sequence": nil}, "usage": e.usage}); err != nil {
		return err
	}
	e.finished = true
	return e.send("message_stop", Object{})
}

func MessageSSE(message Object, sink Sink) error {
	start := Object{}
	for k, v := range message {
		start[k] = v
	}
	start["content"] = []any{}
	start["stop_reason"] = nil
	start["stop_sequence"] = nil
	if err := sink("message_start", Object{"type": "message_start", "message": start}); err != nil {
		return err
	}
	for i, value := range List(message["content"]) {
		b := Obj(value)
		initial := Object{}
		for k, v := range b {
			initial[k] = v
		}
		var deltas []Object
		switch Str(b["type"]) {
		case "text":
			initial["text"] = ""
			deltas = append(deltas, Object{"type": "text_delta", "text": b["text"]})
		case "thinking":
			initial["thinking"] = ""
			delete(initial, "signature")
			deltas = append(deltas, Object{"type": "thinking_delta", "thinking": b["thinking"]})
			if signature := Str(b["signature"]); signature != "" {
				deltas = append(deltas, Object{"type": "signature_delta", "signature": signature})
			}
		case "tool_use":
			initial["input"] = Object{}
			deltas = append(deltas, Object{"type": "input_json_delta", "partial_json": JSON(b["input"])})
		}
		if err := sink("content_block_start", Object{"type": "content_block_start", "index": i, "content_block": initial}); err != nil {
			return err
		}
		for _, delta := range deltas {
			if err := sink("content_block_delta", Object{"type": "content_block_delta", "index": i, "delta": delta}); err != nil {
				return err
			}
		}
		if err := sink("content_block_stop", Object{"type": "content_block_stop", "index": i}); err != nil {
			return err
		}
	}
	if err := sink("message_delta", Object{"type": "message_delta", "delta": Object{"stop_reason": message["stop_reason"], "stop_sequence": message["stop_sequence"]}, "usage": message["usage"]}); err != nil {
		return err
	}
	return sink("message_stop", Object{"type": "message_stop"})
}

type Collector struct {
	Message   Object
	blocks    map[int]Object
	arguments map[int]string
	stopped   bool
}

func (c *Collector) Sink(event string, data Object) error {
	if c.blocks == nil {
		c.blocks = make(map[int]Object)
		c.arguments = make(map[int]string)
	}
	switch event {
	case "error":
		return fmt.Errorf("upstream stream failed: %s", Str(Obj(data["error"])["message"]))
	case "message_start":
		c.Message = Obj(data["message"])
	case "content_block_start":
		index := int(Number(data["index"]))
		c.blocks[index] = Obj(data["content_block"])
	case "content_block_delta":
		index := int(Number(data["index"]))
		b := c.blocks[index]
		d := Obj(data["delta"])
		if b == nil {
			return fmt.Errorf("stream delta has no content block")
		}
		switch Str(d["type"]) {
		case "text_delta":
			b["text"] = Str(b["text"]) + Str(d["text"])
		case "thinking_delta":
			b["thinking"] = Str(b["thinking"]) + Str(d["thinking"])
		case "signature_delta":
			b["signature"] = Str(b["signature"]) + Str(d["signature"])
		case "input_json_delta":
			c.arguments[index] += Str(d["partial_json"])
		}
	case "message_delta":
		if c.Message == nil {
			return fmt.Errorf("stream has no message_start")
		}
		for k, v := range Obj(data["delta"]) {
			c.Message[k] = v
		}
		usage := Obj(c.Message["usage"])
		if usage == nil {
			usage = Object{}
		}
		for k, v := range Obj(data["usage"]) {
			usage[k] = v
		}
		c.Message["usage"] = usage
	case "message_stop":
		c.stopped = true
	}
	return nil
}
func (c *Collector) Result() (Object, error) {
	if !c.stopped || c.Message == nil {
		return nil, fmt.Errorf("upstream stream ended before message_stop")
	}
	content := make([]any, 0, len(c.blocks))
	for i := 0; i < len(c.blocks); i++ {
		b := c.blocks[i]
		if b == nil {
			return nil, fmt.Errorf("stream content block indexes are not contiguous")
		}
		if args := c.arguments[i]; args != "" {
			var input Object
			if json.Unmarshal([]byte(args), &input) != nil || input == nil {
				return nil, fmt.Errorf("upstream tool arguments must be a JSON object")
			}
			b["input"] = input
		}
		content = append(content, b)
	}
	c.Message["content"] = content
	return c.Message, nil
}
