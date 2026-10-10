package bridge

import (
	"fmt"
	"sync"
)

type historyEntry struct {
	input, output []any
	bytes         int
}

type History struct {
	mu      sync.Mutex
	entries map[string]historyEntry
	order   []string
	bytes   int
}

func (h *History) Enrich(body Object) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	previous := Str(body["previous_response_id"])
	if previous != "" {
		entry, ok := h.entries[previous]
		if !ok {
			return fmt.Errorf("previous_response_id is not available; resend the full input history")
		}
		items := append([]any{}, entry.input...)
		items = append(items, entry.output...)
		body["input"] = append(items, InputItems(body["input"])...)
		delete(body, "previous_response_id")
		return nil
	}
	items := InputItems(body["input"])
	existing := map[string]bool{}
	reasoning := map[string]bool{}
	for _, raw := range items {
		item := Obj(raw)
		if item["type"] == "reasoning" {
			reasoning[Str(item["encrypted_content"])] = true
		}
		if item["type"] == "function_call" || item["type"] == "custom_tool_call" || item["type"] == "tool_search_call" {
			existing[Str(item["call_id"])] = true
		}
	}
	repaired := []any{}
	restored := map[string]bool{}
	for _, raw := range items {
		repaired = append(repaired, raw)
		item := Obj(raw)
		if item["type"] != "function_call_output" && item["type"] != "custom_tool_call_output" && item["type"] != "tool_search_output" {
			continue
		}
		id := Str(item["call_id"])
		if existing[id] || restored[id] {
			continue
		}
		var found []any
		matches := 0
		for _, entry := range h.entries {
			for _, candidate := range entry.output {
				if Obj(candidate)["call_id"] == id {
					matches++
					found = entry.output
					break
				}
			}
		}
		if matches == 1 {
			repaired = repaired[:len(repaired)-1]
			for _, candidate := range found {
				call := Obj(candidate)
				callID := Str(call["call_id"])
				cipher := Str(call["encrypted_content"])
				if (call["type"] == "reasoning" && !reasoning[cipher]) || (callID != "" && !existing[callID] && !restored[callID]) {
					repaired = append(repaired, candidate)
					if call["type"] == "reasoning" {
						reasoning[cipher] = true
					}
					if callID != "" {
						restored[callID] = true
					}
				}
			}
			repaired = append(repaired, raw)
		}
	}
	body["input"] = repaired
	return nil
}
func (h *History) Record(body, response Object) {
	id := Str(response["id"])
	if id == "" {
		return
	}
	input, output := InputItems(body["input"]), List(response["output"])
	size := len(JSON(input)) + len(JSON(output))
	if size > 32<<20 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.entries == nil {
		h.entries = map[string]historyEntry{}
	}
	if _, exists := h.entries[id]; exists {
		return
	}
	h.entries[id] = historyEntry{input: input, output: output, bytes: size}
	h.bytes += size
	h.order = append(h.order, id)
	for len(h.order) > 128 || h.bytes > 64<<20 {
		old := h.order[0]
		h.order = h.order[1:]
		h.bytes -= h.entries[old].bytes
		delete(h.entries, old)
	}
}
