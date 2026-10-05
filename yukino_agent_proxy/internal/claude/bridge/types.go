// Package bridge translates Anthropic Messages to OpenAI wire protocols.
package bridge

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type Object = map[string]any
type Sink func(event string, data Object) error

func Obj(v any) Object { m, _ := v.(map[string]any); return m }
func List(v any) []any { a, _ := v.([]any); return a }
func Str(v any) string { s, _ := v.(string); return s }
func Number(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
func JSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func ID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
func Blocks(content any) []any {
	if s, ok := content.(string); ok {
		return []any{Object{"type": "text", "text": s}}
	}
	return List(content)
}
func Text(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	var parts []string
	for _, b := range List(content) {
		if m := Obj(b); m["type"] == "text" {
			parts = append(parts, Str(m["text"]))
		}
	}
	return strings.Join(parts, "\n")
}
func imageURL(b Object) (string, error) {
	s := Obj(b["source"])
	switch Str(s["type"]) {
	case "url":
		if u := Str(s["url"]); u != "" {
			return u, nil
		}
	case "base64":
		if d := Str(s["data"]); d != "" {
			return "data:" + Str(s["media_type"]) + ";base64," + d, nil
		}
	}
	return "", fmt.Errorf("unsupported image source")
}

const reasoningPrefix = "yukino-openai-reasoning-v1:"

func ReasoningBlock(item Object) Object {
	text := ""
	for _, part := range List(item["summary"]) {
		text += Str(Obj(part)["text"])
	}
	if Str(item["encrypted_content"]) != "" {
		envelope := reasoningPrefix + base64.RawURLEncoding.EncodeToString([]byte(JSON(item)))
		if text == "" {
			return Object{"type": "redacted_thinking", "data": envelope}
		}
		return Object{"type": "thinking", "thinking": text, "signature": envelope}
	}
	if text != "" {
		return Object{"type": "thinking", "thinking": text}
	}
	return nil
}

func replayReasoning(b Object) Object {
	encoded := Str(b["signature"])
	if b["type"] == "redacted_thinking" {
		encoded = Str(b["data"])
	}
	if !strings.HasPrefix(encoded, reasoningPrefix) {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, reasoningPrefix))
	if err != nil {
		return nil
	}
	var item Object
	if json.Unmarshal(raw, &item) != nil || item["type"] != "reasoning" {
		return nil
	}
	return item
}

func Usage(u Object) Object {
	input, output := Number(u["input_tokens"]), Number(u["output_tokens"])
	if _, ok := u["input_tokens"]; !ok {
		input = Number(u["prompt_tokens"])
	}
	if _, ok := u["output_tokens"]; !ok {
		output = Number(u["completion_tokens"])
	}
	cached := Number(Obj(u["input_tokens_details"])["cached_tokens"])
	if cached == 0 {
		cached = Number(Obj(u["prompt_tokens_details"])["cached_tokens"])
	}
	if _, ok := u["cache_read_input_tokens"]; ok {
		cached = Number(u["cache_read_input_tokens"])
	}
	created := Number(u["cache_creation_input_tokens"])
	if created == 0 {
		created = Number(Obj(u["input_tokens_details"])["cache_write_tokens"])
	}
	if created == 0 {
		created = Number(Obj(u["prompt_tokens_details"])["cache_write_tokens"])
	}
	result := Object{"input_tokens": max(int64(0), input-cached-created), "output_tokens": output}
	if cached > 0 {
		result["cache_read_input_tokens"] = cached
	}
	if created > 0 {
		result["cache_creation_input_tokens"] = created
	}
	return result
}

func stopReason(reason string, tools bool) string {
	if reason == "length" || reason == "max_output_tokens" || reason == "max_tokens" {
		return "max_tokens"
	}
	if tools {
		return "tool_use"
	}
	return "end_turn"
}
