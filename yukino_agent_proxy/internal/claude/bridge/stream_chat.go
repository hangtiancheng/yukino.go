package bridge

import (
	"encoding/json"
	"fmt"
	"io"
)

func ChatStream(r io.Reader, model string, sink Sink) error {
	e := newEmitter(sink, model)
	finished := false
	reason := ""
	err := ReadSSE(r, func(_ string, data string) (bool, error) {
		if data == "[DONE]" {
			if !finished {
				return false, fmt.Errorf("upstream Chat stream ended without a finish_reason")
			}
			return false, e.finish(reason)
		}
		var chunk Object
		if json.Unmarshal([]byte(data), &chunk) != nil {
			return false, fmt.Errorf("upstream Chat stream contains invalid JSON")
		}
		if upstreamError := Obj(chunk["error"]); upstreamError != nil {
			return false, fmt.Errorf("upstream Chat stream failed: %s", Str(upstreamError["message"]))
		}
		if id := Str(chunk["id"]); !e.started && id != "" {
			e.id = id
		}
		if u := Obj(chunk["usage"]); u != nil {
			e.usage = Usage(u)
		}
		choices := List(chunk["choices"])
		if len(choices) == 0 {
			return true, nil
		}
		choice := Obj(choices[0])
		delta := Obj(choice["delta"])
		if err := e.text("thinking", Str(delta["reasoning_content"])); err != nil {
			return false, err
		}
		if err := e.text("text", Str(delta["content"])); err != nil {
			return false, err
		}
		if err := e.text("text", Str(delta["refusal"])); err != nil {
			return false, err
		}
		calls := List(delta["tool_calls"])
		if f := Obj(delta["function_call"]); f != nil {
			calls = append(calls, Object{"index": int64(0), "id": "call_legacy", "function": f})
		}
		for _, value := range calls {
			call := Obj(value)
			f := Obj(call["function"])
			key := fmt.Sprint(Number(call["index"]))
			if err := e.tool(key, Str(call["id"]), Str(f["name"]), Str(f["arguments"]), false); err != nil {
				return false, err
			}
		}
		if value := Str(choice["finish_reason"]); value != "" && !finished {
			reason = value
			finished = true
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	if e.finished {
		return nil
	}
	if !finished {
		return fmt.Errorf("upstream Chat stream was truncated before finish_reason")
	}
	return e.finish(reason)
}
