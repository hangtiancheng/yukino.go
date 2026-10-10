package bridge

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type Object = map[string]any
type Sink func(string, Object) error

func Obj(v any) Object { m, _ := v.(map[string]any); return m }
func List(v any) []any { a, _ := v.([]any); return a }
func Str(v any) string { s, _ := v.(string); return s }
func Number(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
func JSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func ID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b[:])
}
func Decode(data []byte) (Object, error) {
	var m Object
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.UseNumber()
	err := d.Decode(&m)
	if err == nil && m == nil {
		err = fmt.Errorf("expected a JSON object")
	}
	if err == nil {
		var extra any
		if trailing := d.Decode(&extra); trailing != io.EOF {
			err = fmt.Errorf("expected exactly one JSON object")
		}
	}
	return m, err
}
func Clone(m Object) Object { copy, _ := Decode([]byte(JSON(m))); return copy }
func Text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	var parts []string
	for _, raw := range List(v) {
		m := Obj(raw)
		if s := Str(m["text"]); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

const ThinkingPrefix = "yukino-anthropic-thinking-v1:"
const CompactionPrefix = "yukino-codex-compaction-v1:"
const ChatReasoningPrefix = "yukino-chat-reasoning-v1:"

func EncodeEnvelope(prefix string, value any) string {
	return prefix + base64.RawURLEncoding.EncodeToString([]byte(JSON(value)))
}
func DecodeEnvelope(prefix, value string) any {
	if !strings.HasPrefix(value, prefix) {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return nil
	}
	var v any
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return nil
	}
	return v
}
func ReasoningItem(block Object) Object {
	if block["type"] == "thinking" && Str(block["signature"]) == "" {
		return nil
	}
	if block["type"] == "redacted_thinking" && Str(block["data"]) == "" {
		return nil
	}
	summary := []any{}
	if text := Str(block["thinking"]); text != "" {
		summary = append(summary, Object{"type": "summary_text", "text": text})
	}
	return Object{"id": ID("rs_"), "type": "reasoning", "summary": summary, "encrypted_content": EncodeEnvelope(ThinkingPrefix, block)}
}
func ReasoningText(item Object) string {
	if value := DecodeEnvelope(ChatReasoningPrefix, Str(item["encrypted_content"])); value != nil {
		return Str(value)
	}
	if text := Text(item["summary"]); text != "" {
		return text
	}
	return Text(item["content"])
}

func Usage(u Object, anthropic bool) Object {
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
	if anthropic {
		input += cached + created
	}
	reasoning := Number(Obj(u["completion_tokens_details"])["reasoning_tokens"])
	return Object{"input_tokens": input, "output_tokens": output, "total_tokens": input + output, "input_tokens_details": Object{"cached_tokens": cached, "cache_write_tokens": created}, "output_tokens_details": Object{"reasoning_tokens": reasoning}}
}
