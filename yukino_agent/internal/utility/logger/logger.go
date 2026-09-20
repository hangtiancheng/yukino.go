// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
