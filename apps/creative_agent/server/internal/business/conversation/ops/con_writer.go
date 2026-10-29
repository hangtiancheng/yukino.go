package ops

import (
	"encoding/json"
	"sync"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/sse"
)

// ConWriterNode is the exit operator of the conversation flow.
//
// It watches the SSE frames on two Slices in the flow context and writes them into the HTTP response in realtime.
// One operator, two listening goroutines, converging on the same writeFrame exit:
//
//	Goroutine 1 (ConEndSSE):    watches ConOnePackSSEKey   — sync path, frames are "semi-finished"
//	Goroutine 2 (ConReconnectSSE): watches ConReconnectSSEKey — async path, frames are "finished"
//
// Async frames are finished products already assembled and persisted by the Worker; they are marshaled and sent as-is, with no duplicate DB writes.
type ConWriterNode struct {
	writer *sse.Writer // SSE writer, writing frames directly into the HTTP ResponseWriter
}

// NewConWriterNode creates a ConWriterNode.
func NewConWriterNode(w *sse.Writer) *ConWriterNode {
	return &ConWriterNode{writer: w}
}

// Name returns the operator name.
func (n *ConWriterNode) Name() string { return "con_writer" }

// DoImpl starts two goroutines watching the sync key and the async key respectively,
// converging on the same writeFrame exit to write the HTTP response.
// Whichever goroutine receives __FLOW_END__ signals the other to exit.
func (n *ConWriterNode) DoImpl(ctx *seda.Context) (int, string) {
	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(stopCh) }) }

	// Goroutine 1: sync frames (ConOnePackSSEKey) — from Phase 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		slice := ctx.GetSliceWithInit(ConOnePackSSEKey)
		cursor := 0
		for {
			ch := slice.WatchPush()
			diff, next := slice.DiffFrom(cursor)
			cursor = next
			for _, raw := range diff {
				frame, ok := raw.(string)
				if !ok {
					continue
				}
				if frame == "__FLOW_END__" {
					stop()
					return
				}
				n.writeFrame(frame)
			}
			select {
			case <-stopCh:
				return
			case <-ch:
			}
		}
	}()

	// Goroutine 2: async/reconnect frames (ConReconnectSSEKey) — added in Phase 2
	// Async frames are "finished"; the Worker already assembled and persisted them, so just send them as-is
	wg.Add(1)
	go func() {
		defer wg.Done()
		slice := ctx.GetSliceWithInit(ConReconnectSSEKey)
		cursor := 0
		for {
			ch := slice.WatchPush()
			diff, next := slice.DiffFrom(cursor)
			cursor = next
			for _, raw := range diff {
				frame, ok := raw.(string)
				if !ok {
					continue
				}
				if frame == "__FLOW_END__" {
					stop()
					return
				}
				n.writeFrame(frame)
			}
			select {
			case <-stopCh:
				return
			case <-ch:
			}
		}
	}()

	wg.Wait()
	return 0, ""
}

// writeFrame writes an SSEFrame JSON string into the HTTP response.
// It extracts the type field from the frame as the SSE event name.
func (n *ConWriterNode) writeFrame(frame string) {
	if n.writer == nil {
		return
	}
	eventType := extractEventType(frame)
	if eventType != "" {
		_ = n.writer.WriteEvent(eventType, json.RawMessage(frame))
	} else {
		_ = n.writer.WriteRaw(frame)
	}
}

// extractEventType extracts the type field from the SSEFrame JSON as the SSE event name.
func extractEventType(raw string) string {
	var tmp struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(raw), &tmp); err != nil {
		return ""
	}
	return tmp.Type
}
