package sse

// SSE event type constants for the conversation stream.
const (
	EventMessageStart    = "message-start"
	EventTextDelta       = "text-delta"
	EventToolCallStart   = "tool-call-start"
	EventToolCallResult  = "tool-call-result"
	EventMessageComplete = "message-complete"
	EventError           = "error"
	EventReconnect       = "reconnect"
	// EventTaskStep is the subtask progress event (used by the Step 3.5 poll_hook backflow).
	EventTaskStep = "task-step"
	// EventTaskDone is the subtask terminal event (poll complete / product delivered).
	EventTaskDone = "task-done"
	// EventUIBlockStart is the UI-card start event (e.g. hot-topic card PromptOptionGroup).
	EventUIBlockStart = "ui-block-start"
	// EventUIBlockComplete is the UI-card end event.
	EventUIBlockComplete = "ui-block-complete"
)

// Payload is the generic SSE event payload.
type Payload struct {
	Type    string `json:"type"`
	MsgID   string `json:"msgId,omitempty"`
	Content string `json:"content,omitempty"`
	TextID  string `json:"textId,omitempty"`
}
