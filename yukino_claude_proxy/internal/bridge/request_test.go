package bridge

import (
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/config"
)

func TestMidConversationSystemMessages(t *testing.T) {
	for _, protocol := range []string{config.Anthropic, config.OpenAICompat, config.OpenAI} {
		t.Run(protocol, func(t *testing.T) {
			messages := []any{
				Object{"role": "user", "content": "Hello"},
				Object{"role": "system", "content": []any{
					Object{"type": "text", "text": "Current context.", "cache_control": Object{"type": "ephemeral"}},
					Object{"type": "text", "text": "Additional context."},
				}},
				Object{"role": "assistant", "content": "Hello back"},
				Object{"role": "user", "content": "Continue"},
				Object{"role": "system", "content": "New context."},
			}
			original := JSON(messages)
			body := Object{"max_tokens": 128, "system": "Global context.", "messages": messages}
			request, err := Request(body, config.Provider{Protocol: protocol, Model: "model"})
			if err != nil {
				t.Fatal(err)
			}
			key, offset := "messages", 0
			if protocol == config.OpenAI {
				key = "input"
				if request["instructions"] != "Global context." {
					t.Fatal("top-level system instructions were lost")
				}
			} else if protocol == config.OpenAICompat {
				offset = 1
			}
			converted := List(request[key])
			if len(converted) != len(messages)+offset {
				t.Fatalf("message count changed: %s", JSON(converted))
			}
			if offset == 1 && (Obj(converted[0])["role"] != "system" || Obj(converted[0])["content"] != "Global context.") {
				t.Fatal("top-level system instructions were lost")
			}
			for i, value := range messages {
				if Obj(converted[i+offset])["role"] != Obj(value)["role"] {
					t.Fatalf("message %d lost its role or position", i)
				}
			}
			if protocol == config.Anthropic {
				if JSON(converted) != original {
					t.Fatal("native message fields were changed")
				}
			} else {
				content := Obj(converted[1+offset])["content"]
				var text string
				if protocol == config.OpenAICompat {
					text = Text(content)
				} else {
					var parts []string
					for _, part := range List(content) {
						if Obj(part)["type"] != "input_text" {
							t.Fatal("system text must be a Responses input_text block")
						}
						parts = append(parts, Str(Obj(part)["text"]))
					}
					text = strings.Join(parts, "\n")
				}
				if text != "Current context.\nAdditional context." {
					t.Fatal("system content was dropped or reordered")
				}
			}
		})
	}
}

func TestInvalidMessageRoles(t *testing.T) {
	for _, protocol := range []string{config.Anthropic, config.OpenAICompat, config.OpenAI} {
		for _, role := range []any{"tool", "developer", "", nil, 1} {
			body := Object{"max_tokens": 128, "messages": []any{Object{"role": role, "content": "Hello"}}}
			if _, err := Request(body, config.Provider{Protocol: protocol, Model: "model"}); err == nil {
				t.Errorf("%s accepted invalid Anthropic role %v", protocol, role)
			}
		}
	}
}
