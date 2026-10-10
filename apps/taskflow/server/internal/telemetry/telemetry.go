package telemetry

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

type Manager struct {
	provider *sdktrace.TracerProvider
	file     *os.File
}

func Setup(ctx context.Context, cfg conf.TelemetryConf, nodeID string) (*Manager, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	m := &Manager{}
	if !cfg.Enabled {
		return m, nil
	}

	var writer io.Writer = io.Discard
	switch cfg.Exporter {
	case "stdout":
		writer = os.Stdout
	case "file":
		if cfg.FilePath == "" {
			return nil, fmt.Errorf("telemetry exporter file requires file_path")
		}
		if err := os.MkdirAll(filepath.Dir(cfg.FilePath), 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		m.file = f
		writer = f
	default:
		return m, nil
	}

	exporter, err := stdouttrace.New(stdouttrace.WithWriter(writer), stdouttrace.WithoutTimestamps())
	if err != nil {
		return nil, err
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("taskflow"),
		semconv.ServiceInstanceID(nodeID),
	))
	if err != nil {
		return nil, err
	}

	m.provider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)
	otel.SetTracerProvider(m.provider)
	return m, nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	var firstErr error
	if m.provider != nil {
		if err := m.provider.Shutdown(ctx); err != nil {
			firstErr = err
		}
	}
	if m.file != nil {
		if err := m.file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

func TraceIDFrom(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

func Inject(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

func Extract(ctx context.Context, carrier map[string]string) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
}
