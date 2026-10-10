package logger

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

func Init() {
	if defaultLogger != nil {
		return
	}
	defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(defaultLogger)
}

func L() *slog.Logger {
	if defaultLogger == nil {
		defaultLogger = slog.Default()
	}
	return defaultLogger
}
