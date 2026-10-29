package appkitrunner

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"

// PrepareFunc is the prompt-injection function of the ReAct engine (q is the user query; it returns the initial messages).
type PrepareFunc func(q string) []react.Message

// DefaultAssistantPrepare is the generic assistant prompt (no business semantics; serves as a fallback).
// Business-role prompts (e.g. the manager) are injected by callers via SetPrepare.
func DefaultAssistantPrepare(q string) []react.Message {
	return []react.Message{
		{Role: "system", Content: "你是一个有帮助的 AI 助手，请用 JSON 格式回复。"},
		{Role: "user", Content: q},
	}
}
