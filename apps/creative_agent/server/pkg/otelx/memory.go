package otelx

import (
	"context"
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// MemoryExporter is an in-memory span collector for tests.
// It implements sdktrace.SpanExporter, storing received spans in a slice for assertions.
type MemoryExporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

// newMemoryExporter creates a MemoryExporter.
func newMemoryExporter() *MemoryExporter {
	return &MemoryExporter{}
}

// ExportSpans implements sdktrace.SpanExporter.
func (e *MemoryExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, spans...)
	return nil
}

// Shutdown implements sdktrace.SpanExporter.
func (e *MemoryExporter) Shutdown(ctx context.Context) error { return nil }

// GetSpans returns all spans collected so far.
func (e *MemoryExporter) GetSpans() []sdktrace.ReadOnlySpan {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]sdktrace.ReadOnlySpan, len(e.spans))
	copy(out, e.spans)
	return out
}

// Reset clears the collected spans.
func (e *MemoryExporter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = nil
}

// Compile-time assertion: MemoryExporter implements sdktrace.SpanExporter.
var _ sdktrace.SpanExporter = (*MemoryExporter)(nil)
