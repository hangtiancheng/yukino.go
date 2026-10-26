package sse

// conversation 流中的 SSE 事件类型常量。
const (
	EventMessageStart    = "message-start"
	EventTextDelta       = "text-delta"
	EventToolCallStart   = "tool-call-start"
	EventToolCallResult  = "tool-call-result"
	EventMessageComplete = "message-complete"
	EventError           = "error"
	EventReconnect       = "reconnect"
	// EventTaskStep 是子任务进度事件（Step 3.5 poll_hook 回流用）。
	EventTaskStep = "task-step"
	// EventTaskDone 是子任务终态事件（poll 完成 / 交付产物）。
	EventTaskDone = "task-done"
	// EventUIBlockStart 是 UI 卡开始事件（如热点卡 PromptOptionGroup）。
	EventUIBlockStart = "ui-block-start"
	// EventUIBlockComplete 是 UI 卡结束事件。
	EventUIBlockComplete = "ui-block-complete"
)

// Payload 是通用 SSE 事件负载。
type Payload struct {
	Type    string `json:"type"`
	MsgID   string `json:"msgId,omitempty"`
	Content string `json:"content,omitempty"`
	TextID  string `json:"textId,omitempty"`
}
