package otelx_test

import (
	"context"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestNewMemoryExporter 验证内存 exporter 能产出 span。
func TestNewMemoryExporter(t *testing.T) {
	tp, exp := otelx.NewMemoryExporter()
	defer func() { testx.NoError(t, tp.Shutdown(context.Background())) }()

	_, span := tp.Tracer("test").Start(context.Background(), "test-span")
	span.End()

	testx.Eventually(t, func() bool {
		return len(exp.GetSpans()) > 0
	}, 2*time.Second, 10*time.Millisecond, "内存 exporter 应收到 span")

	spans := exp.GetSpans()
	testx.Equal(t, "test-span", spans[0].Name())
}

// TestInitDisabled 验证 enabled=false 时返回 no-op provider。
func TestInitDisabled(t *testing.T) {
	tp, err := otelx.Init(config.OTelConfig{
		Enabled:          false,
		ExporterEndpoint: "localhost:4317",
		ServiceName:      "test",
		SampleRate:       1.0,
	}, nil)
	testx.NoError(t, err)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// no-op provider：Start 应返回 non-recording span（IsRecording()==false）
	_, span := tp.Tracer("test").Start(context.Background(), "x")
	defer span.End()
	testx.False(t, span.IsRecording(), "disabled 时不应记录 span")
}
