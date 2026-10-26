package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New 根据指定 level 创建 *zap.Logger。
// level 取值: "debug", "info", "warn", "error"，其他值默认 "info"。
func New(level string) *zap.Logger {
	var lvl zapcore.Level
	switch level {
	case "debug":
		lvl = zapcore.DebugLevel
	case "warn":
		lvl = zapcore.WarnLevel
	case "error":
		lvl = zapcore.ErrorLevel
	default:
		lvl = zapcore.InfoLevel
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	cfg.EncoderConfig.TimeKey = "time"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, err := cfg.Build()
	if err != nil {
		panic("failed to build logger: " + err.Error())
	}
	return logger
}
