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

// GormLogger 是 GORM logger 的 zap 实现。
// 把 slow SQL 从标准库 log.Printf 改为 zap JSON，统一日志格式。
type GormLogger struct {
	logger        *zap.Logger
	slowThreshold time.Duration
}

// NewGormLogger 创建 GORM zap logger。slowThreshold<=0 时默认 200ms。
func NewGormLogger(l *zap.Logger, slowThreshold time.Duration) *GormLogger {
	if slowThreshold <= 0 {
		slowThreshold = 200 * time.Millisecond
	}
	return &GormLogger{logger: l, slowThreshold: slowThreshold}
}

// LogMode 实现 gormlogger.Interface。
func (g *GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return g
}

// withTrace 从 ctx 派生带 trace_id/span_id 字段的 logger（同 tracex.Logger，避免循环依赖）。
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

// Info 实现 gormlogger.Interface。
func (g *GormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	g.withTrace(ctx).Info(msg, zap.Any("data", data))
}

// Warn 实现 gormlogger.Interface。
func (g *GormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	g.withTrace(ctx).Warn(msg, zap.Any("data", data))
}

// Error 实现 gormlogger.Interface。
func (g *GormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	g.withTrace(ctx).Error(msg, zap.Any("data", data))
}

// Trace 实现 gormlogger.Interface：记录 SQL 执行，超阈值记 slow SQL。
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
	// 普通 SQL 不逐条打日志（避免刷屏）；如需可调成 Debug。
	l.Debug("sql",
		zap.Duration("duration", elapsed),
		zap.Int64("rows", rows),
		zap.String("sql", sql),
	)
}
