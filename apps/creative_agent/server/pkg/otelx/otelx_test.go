package otelx_test

import (
	"context"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestNewMemoryExporter verifies that the in-memory exporter produces spans.
func TestNewMemoryExporter(t *testing.T) {
	tp, exp := otelx.NewMemoryExporter()
	defer func() { testx.NoError(t, tp.Shutdown(context.Background())) }()

	_, span := tp.Tracer("test").Start(context.Background(), "test-span")
	span.End()

	testx.Eventually(t, func() bool {
		return len(exp.GetSpans()) > 0
	}, 2*time.Second, 10*time.Millisecond, "the in-memory exporter should receive spans")

	spans := exp.GetSpans()
	testx.Equal(t, "test-span", spans[0].Name())
}

// TestInitDisabled verifies that enabled=false returns a no-op provider.
func TestInitDisabled(t *testing.T) {
	tp, err := otelx.Init(config.OTelConfig{
		Enabled:          false,
		ExporterEndpoint: "localhost:4317",
		ServiceName:      "test",
		SampleRate:       1.0,
	}, nil)
	testx.NoError(t, err)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// no-op provider: Start should return a non-recording span (IsRecording()==false)
	_, span := tp.Tracer("test").Start(context.Background(), "x")
	defer span.End()
	testx.False(t, span.IsRecording(), "no spans should be recorded when disabled")
}
