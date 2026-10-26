package sse

import (
	"encoding/json"
	"fmt"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// Writer 负责将 SSE 事件写入 HTTP 响应。
// 基于 yukino_http 的 SSEWriter 构建：ctx.SSE() 完成流式响应头、200 状态码
// 与响应接管（禁用延迟渲染），之后帧直接落盘到连接。
type Writer struct {
	w *yukino.SSEWriter
}

// NewWriter 基于 yukino Context 创建 SSE Writer。
// 调用前可用 ctx.Set 设置自定义响应头（如 X-Accel-Buffering），会被 SSE() 复制进响应。
func NewWriter(ctx *yukino.Context) *Writer {
	return &Writer{w: ctx.SSE()}
}

// WriteEvent 写入一条带 event 类型的 SSE 帧，data 以 JSON 序列化。
func (s *Writer) WriteEvent(event string, data interface{}) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化 SSE 数据失败: %w", err)
	}
	s.w.Event(event, string(b))
	return nil
}

// WriteRaw 直接写入原始字符串作为 SSE data 行。
func (s *Writer) WriteRaw(frame string) error {
	s.w.Data(frame)
	return nil
}
