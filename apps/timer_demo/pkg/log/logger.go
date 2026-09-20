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

package log

import (
	"context"
	"fmt"
	"log"
	"os"
)

type Logger interface {
	Error(v ...any)
	Warn(v ...any)
	Info(v ...any)
	Debug(v ...any)
	Errorf(format string, v ...any)
	Warnf(format string, v ...any)
	Infof(format string, v ...any)
	Debugf(format string, v ...any)
}

var (
	defaultLogger Logger
)

func init() {
	defaultLogger = newStandardLogger(NewOptions())
}

// Options holds logger configuration.
type Options struct {
	LogName    string
	LogLevel   string
	FileName   string
	MaxAge     int
	MaxSize    int
	MaxBackups int
	Compress   bool
}

// Option is a functional option for the logger.
type Option func(*Options)

// NewOptions creates default logger options.
func NewOptions(opts ...Option) Options {
	options := Options{
		LogName:    "app",
		LogLevel:   "info",
		FileName:   "app.log",
		MaxAge:     10,
		MaxSize:    100,
		MaxBackups: 3,
		Compress:   true,
	}
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// WithLogLevel sets the log level.
func WithLogLevel(level string) Option {
	return func(o *Options) {
		o.LogLevel = level
	}
}

// WithFileName sets the log file name.
func WithFileName(filename string) Option {
	return func(o *Options) {
		o.FileName = filename
	}
}

type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarn
	levelError
)

// Levels maps level names to logLevel values.
var Levels = map[string]logLevel{
	"":      levelDebug,
	"debug": levelDebug,
	"info":  levelInfo,
	"warn":  levelWarn,
	"error": levelError,
	"fatal": levelError,
}

type standardLogger struct {
	logger *log.Logger
	level  logLevel
}

func newStandardLogger(options Options) *standardLogger {
	return &standardLogger{
		logger: log.New(os.Stdout, "", log.LstdFlags),
		level:  Levels[options.LogLevel],
	}
}

func (l *standardLogger) output(level logLevel, levelStr string, callDepth int, v ...any) {
	if level < l.level {
		return
	}
	_ = l.logger.Output(callDepth, fmt.Sprintf("[%s] %s", levelStr, fmt.Sprint(v...)))
}

func (l *standardLogger) outputf(level logLevel, levelStr string, callDepth int, format string, v ...any) {
	if level < l.level {
		return
	}
	_ = l.logger.Output(callDepth, fmt.Sprintf("[%s] %s", levelStr, fmt.Sprintf(format, v...)))
}

func (l *standardLogger) Error(v ...any) { l.output(levelError, "ERROR", 3, v...) }
func (l *standardLogger) Warn(v ...any)  { l.output(levelWarn, "WARN", 3, v...) }
func (l *standardLogger) Info(v ...any)  { l.output(levelInfo, "INFO", 3, v...) }
func (l *standardLogger) Debug(v ...any) { l.output(levelDebug, "DEBUG", 3, v...) }

func (l *standardLogger) Errorf(format string, v ...any) {
	l.outputf(levelError, "ERROR", 3, format, v...)
}
func (l *standardLogger) Warnf(format string, v ...any) {
	l.outputf(levelWarn, "WARN", 3, format, v...)
}
func (l *standardLogger) Infof(format string, v ...any) {
	l.outputf(levelInfo, "INFO", 3, format, v...)
}
func (l *standardLogger) Debugf(format string, v ...any) {
	l.outputf(levelDebug, "DEBUG", 3, format, v...)
}

// GetDefaultLogger returns the default logger instance.
func GetDefaultLogger() Logger {
	return defaultLogger
}

// Debugf logs a message at debug level.
func Debugf(format string, args ...any) {
	GetDefaultLogger().Debugf(format, args...)
}

// Infof logs a message at info level.
func Infof(format string, args ...any) {
	GetDefaultLogger().Infof(format, args...)
}

// Warnf logs a message at warn level.
func Warnf(format string, args ...any) {
	GetDefaultLogger().Warnf(format, args...)
}

// Errorf logs a message at error level.
func Errorf(format string, args ...any) {
	GetDefaultLogger().Errorf(format, args...)
}

// DebugContext logs a message at debug level with context.
func DebugContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Debug(args...)
}

// DebugContextf logs a formatted message at debug level with context.
func DebugContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Debugf(format, args...)
}

// InfoContext logs a message at info level with context.
func InfoContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Info(args...)
}

// InfoContextf logs a formatted message at info level with context.
func InfoContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Infof(format, args...)
}

// WarnContext logs a message at warn level with context.
func WarnContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Warn(args...)
}

// WarnContextf logs a formatted message at warn level with context.
func WarnContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Warnf(format, args...)
}

// ErrorContext logs a message at error level with context.
func ErrorContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Error(args...)
}

// ErrorContextf logs a formatted message at error level with context.
func ErrorContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Errorf(format, args...)
}

// Fatalf logs a message at error level and is kept for compatibility.
func Fatalf(format string, args ...any) {
	Errorf(format, args...)
}
