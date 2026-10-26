// Package otelx 提供 OpenTelemetry 初始化与测试辅助。
package otelx

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"
)

// Init 初始化全局 TracerProvider。
// enabled=false 时返回 no-op provider（测试/CI 零副作用，不启动 exporter）。
// exporter 建失败不阻断启动：logger.Warn 后降级为 no-op。
// logger 为 nil 时用 zap.L()。
func Init(cfg config.OTelConfig, logger *zap.Logger) (*sdktrace.TracerProvider, error) {
	if logger == nil {
		logger = zap.L()
	}
	if !cfg.Enabled {
		// 无 exporter 的 sdk provider：span 进不了任何 exporter，等同 no-op，
		// 但保留 Shutdown 语义，调用方统一处理。Start 产生的 span 为 non-recording。
		return sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample())), nil
	}

	// 统一 W3C tracecontext 传播（HTTP header / Redis stream 透传用）。
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	sampler := sdktrace.AlwaysSample()
	if cfg.SampleRate < 1.0 {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	exp, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithEndpoint(cfg.ExporterEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		// exporter 建失败不阻断启动：降级为 no-op（无 exporter），保证服务可用。
		logger.Warn("otelx: failed to create otlp exporter (trace disabled)",
			zap.String("endpoint", cfg.ExporterEndpoint), zap.Error(err))
		return sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample())), nil
	}

	svcName := cfg.ServiceName
	if svcName == "" {
		svcName = "yukino-creative-agent"
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(svcName),
		)),
	)

	// 设为全局：GORM plugin / redisotel / otelhttp 默认读全局 provider。
	otel.SetTracerProvider(tp)
	return tp, nil
}

// NewMemoryExporter 创建测试用的内存 exporter + TracerProvider。
// 测试内调用，避免依赖真实 Jaeger。注意：不调用 otel.SetTracerProvider，
// 由测试方自行决定是否替换全局 provider（避免覆盖 app.New 的初始化）。
func NewMemoryExporter() (*sdktrace.TracerProvider, *MemoryExporter) {
	exp := newMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(10*time.Millisecond)),
	)
	return tp, exp
}
