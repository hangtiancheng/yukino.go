package sse

import (
	"encoding/json"
	"fmt"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// Writer writes SSE events into the HTTP response.
// Built on yukino_http's SSEWriter: ctx.SSE() sets up the streaming response headers, the 200 status code,
// and response takeover (deferred rendering disabled); afterwards frames are flushed directly to the connection.
type Writer struct {
	w *yukino.SSEWriter
}

// NewWriter creates an SSE Writer from a yukino Context.
// Before calling it, ctx.Set can set custom response headers (e.g. X-Accel-Buffering); SSE() copies them into the response.
func NewWriter(ctx *yukino.Context) *Writer {
	return &Writer{w: ctx.SSE()}
}

// WriteEvent writes one SSE frame with an event type; data is serialized as JSON.
func (s *Writer) WriteEvent(event string, data interface{}) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal SSE data: %w", err)
	}
	s.w.Event(event, string(b))
	return nil
}

// WriteRaw writes a raw string directly as the SSE data line.
func (s *Writer) WriteRaw(frame string) error {
	s.w.Data(frame)
	return nil
}
