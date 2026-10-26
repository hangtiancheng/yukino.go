package ops

import (
	"encoding/json"
	"sync"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/sse"
)

// ConWriterNode 是 conversation flow 的出口算子。
//
// 监听 flowcontext 中两条 Slice 的 SSE 帧，实时写入 HTTP 响应。
// 一个算子、两个监听 goroutine，汇到同一个 writeFrame 出口：
//
//	Goroutine 1 (ConEndSSE):    监听 ConOnePackSSEKey   — 同步路径，帧是"半成品"
//	Goroutine 2 (ConReconnectSSE): 监听 ConReconnectSSEKey — 异步路径，帧是"成品"
//
// 异步帧是 Worker 已组装+落库过的成品，只需原样 marshal 下发，不重复写 DB。
type ConWriterNode struct {
	writer *sse.Writer // SSE 写入器，直接将帧写入 HTTP ResponseWriter
}

// NewConWriterNode 创建 ConWriterNode。
func NewConWriterNode(w *sse.Writer) *ConWriterNode {
	return &ConWriterNode{writer: w}
}

// Name 返回算子名。
func (n *ConWriterNode) Name() string { return "con_writer" }

// DoImpl 启动两个 goroutine 分别监听同步 key 和异步 key，
// 汇到同一个 writeFrame 出口写入 HTTP 响应。
// 任一 goroutine 收到 __FLOW_END__ 后发信号给另一个退出。
func (n *ConWriterNode) DoImpl(ctx *seda.Context) (int, string) {
	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(stopCh) }) }

	// Goroutine 1: 同步帧 (ConOnePackSSEKey) — Phase 1 已有
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

	// Goroutine 2: 异步/重连帧 (ConReconnectSSEKey) — Phase 2 新增
	// 异步帧是 "成品"，Worker 那边已组装+落库过，这里只需原样下发
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

// writeFrame 将 SSEFrame JSON 字符串写入 HTTP 响应。
// 从 frame 中提取 type 字段作为 SSE event 名。
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

// extractEventType 从 SSEFrame JSON 中提取 type 字段作为 SSE event 名。
func extractEventType(raw string) string {
	var tmp struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(raw), &tmp); err != nil {
		return ""
	}
	return tmp.Type
}
