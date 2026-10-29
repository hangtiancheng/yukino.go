package otelx

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

// GormLogger is a zap implementation of the GORM logger.
// It routes slow SQL through zap JSON instead of the standard-library log.Printf, unifying the log format.
type GormLogger struct {
	logger        *zap.Logger
	slowThreshold time.Duration
}

// NewGormLogger creates the GORM zap logger. slowThreshold<=0 defaults to 200ms.
func NewGormLogger(l *zap.Logger, slowThreshold time.Duration) *GormLogger {
	if slowThreshold <= 0 {
		slowThreshold = 200 * time.Millisecond
	}
	return &GormLogger{logger: l, slowThreshold: slowThreshold}
}

// LogMode implements gormlogger.Interface.
func (g *GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return g
}

// withTrace derives a logger with trace_id/span_id fields from ctx (same as tracex.Logger, avoiding a circular dependency).
func (g *GormLogger) withTrace(ctx context.Context) *zap.Logger {
	l := g.logger
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		l = l.With(
			zap.String("trace_id", sc.TraceID().String()),
			zap.String("span_id", sc.SpanID().String()),
		)
	}
	return l
}

// Info implements gormlogger.Interface.
func (g *GormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	g.withTrace(ctx).Info(msg, zap.Any("data", data))
}

// Warn implements gormlogger.Interface.
func (g *GormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	g.withTrace(ctx).Warn(msg, zap.Any("data", data))
}

// Error implements gormlogger.Interface.
func (g *GormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	g.withTrace(ctx).Error(msg, zap.Any("data", data))
}

// Trace implements gormlogger.Interface: records SQL execution and logs slow SQL beyond the threshold.
func (g *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	l := g.withTrace(ctx)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Error("sql error",
			zap.Error(err),
			zap.Duration("duration", elapsed),
			zap.Int64("rows", rows),
			zap.String("sql", sql),
		)
		return
	}
	if elapsed > g.slowThreshold {
		l.Warn("slow SQL",
			zap.Duration("duration", elapsed),
			zap.Int64("rows", rows),
			zap.String("sql", sql),
			zap.String("threshold", g.slowThreshold.String()),
			zap.String("file", utils.FileWithLineNum()),
		)
		return
	}
	// Regular SQL is not logged one by one (to avoid flooding); switch to Debug if needed.
	l.Debug("sql",
		zap.Duration("duration", elapsed),
		zap.Int64("rows", rows),
		zap.String("sql", sql),
	)
}
