// Package logger provides a unified structured logger (log/slog) for the yukino_agent
// application. It replaces the previous mix of fmt.Printf / log.Printf with a single
// leveled, key-value logger so output is consistent and greppable.
//
// Usage:
//
//	logger.Init()                       // call once at startup (main.go)
//	logger.L().Info("msg", "key", val)  // anywhere
//
// CLI commands (cmd/*) keep using log.Fatalf for startup failures, which is the
// conventional Go pattern and not routed through slog.
package logger

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

// Init initializes the default slog logger with a text handler writing to stdout.
// Call once at program startup (before any component logs). Idempotent.
func Init() {
	if defaultLogger != nil {
		return
	}
	defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(defaultLogger)
}

// L returns the package-level logger. If Init has not been called, it falls back
// to a default slog.Default()-backed logger so callers are safe in any order.
func L() *slog.Logger {
	if defaultLogger == nil {
		defaultLogger = slog.Default()
	}
	return defaultLogger
}
