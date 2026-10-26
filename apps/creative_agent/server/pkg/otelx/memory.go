package otelx

import (
	"context"
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// MemoryExporter 是测试用的 span 内存收集器。
// 实现 sdktrace.SpanExporter，把收到的 span 存在切片里供断言。
type MemoryExporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

// newMemoryExporter 创建 MemoryExporter。
func newMemoryExporter() *MemoryExporter {
	return &MemoryExporter{}
}

// ExportSpans 实现 sdktrace.SpanExporter。
func (e *MemoryExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, spans...)
	return nil
}

// Shutdown 实现 sdktrace.SpanExporter。
func (e *MemoryExporter) Shutdown(ctx context.Context) error { return nil }

// GetSpans 返回当前收集到的所有 span。
func (e *MemoryExporter) GetSpans() []sdktrace.ReadOnlySpan {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]sdktrace.ReadOnlySpan, len(e.spans))
	copy(out, e.spans)
	return out
}

// Reset 清空已收集的 span。
func (e *MemoryExporter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = nil
}

// 编译期断言：MemoryExporter 实现 sdktrace.SpanExporter。
var _ sdktrace.SpanExporter = (*MemoryExporter)(nil)
