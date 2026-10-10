package obsx

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
)

func Setup(cfg conf.SentryConf, nodeID string) error {
	dsn := cfg.DSN()
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      cfg.Environment,
		Release:          cfg.Release,
		ServerName:       nodeID,
		EnableTracing:    false,
		TracesSampleRate: 0,
		Debug:            false,
	})
	if err != nil {
		return fmt.Errorf("sentry init: %w", err)
	}
	return nil
}

func Flush(timeout time.Duration) {
	sentry.Flush(timeout)
}

func CaptureError(ctx context.Context, err error, tags map[string]string) {
	if err == nil {
		return
	}
	hub := sentry.CurrentHub().Clone()
	event := sentry.NewEvent()
	event.Message = err.Error()
	event.Exception = []sentry.Exception{{
		Type:  "TaskflowError",
		Value: err.Error(),
	}}
	event.Level = sentry.LevelError
	event.Tags = map[string]string{"component": "server"}
	for k, v := range tags {
		event.Tags[k] = v
	}
	if traceID := ctxTraceID(ctx); traceID != "" {
		event.Tags["trace_id"] = traceID
	}
	hub.CaptureEvent(event)
}

func CapturePanic(ctx context.Context, recovered any, tags map[string]string) {
	hub := sentry.CurrentHub().Clone()
	event := sentry.NewEvent()
	event.Level = sentry.LevelFatal
	event.Message = fmt.Sprintf("panic: %v", recovered)
	event.Exception = []sentry.Exception{{
		Type:  "Panic",
		Value: fmt.Sprintf("%v", recovered),
	}}
	event.Tags = map[string]string{"component": "server"}
	for k, v := range tags {
		event.Tags[k] = v
	}
	if traceID := ctxTraceID(ctx); traceID != "" {
		event.Tags["trace_id"] = traceID
	}
	hub.CaptureEvent(event)
}

func ctxTraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	return ""
}

type traceIDKey struct{}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}
