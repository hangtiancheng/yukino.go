package appkitrunner

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"

// PrepareFunc 是 ReAct 引擎的 prompt 注入函数（q 为用户 query，返回初始 messages）。
type PrepareFunc func(q string) []react.Message

// DefaultAssistantPrepare 是通用 assistant prompt（无业务语义，可作兜底）。
// 业务角色 prompt（如管家）由调用方通过 SetPrepare 注入。
func DefaultAssistantPrepare(q string) []react.Message {
	return []react.Message{
		{Role: "system", Content: "你是一个有帮助的 AI 助手，请用 JSON 格式回复。"},
		{Role: "user", Content: q},
	}
}
