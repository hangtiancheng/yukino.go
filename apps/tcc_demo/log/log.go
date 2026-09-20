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
	defaultLogger = NewSugarLogger(NewOptions())
}

// Options configuration
type Options struct {
	LogName    string // Log name
	LogLevel   string // Log level
	FileName   string // File name
	MaxAge     int    // Log retention time in days
	MaxSize    int    // Log retention size in MB
	MaxBackups int    // Number of backup files to keep
	Compress   bool   // Whether to compress
}

// Option is a functional option type
type Option func(*Options)

// NewOptions initializes with defaults
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

// WithLogLevel sets the log level
func WithLogLevel(level string) Option {
	return func(o *Options) {
		o.LogLevel = level
	}
}

// WithFileName sets the log file name
func WithFileName(filename string) Option {
	return func(o *Options) {
		o.FileName = filename
	}
}

// Levels log level
var Levels = map[string]int{
	"":      0,
	"debug": 0,
	"info":  1,
	"warn":  2,
	"error": 3,
	"fatal": 4,
}

type stdLoggerWrapper struct {
	logger *log.Logger
	level  int
}

func NewSugarLogger(options Options) *stdLoggerWrapper {
	file, err := os.OpenFile(options.FileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		file = os.Stderr
	}
	return &stdLoggerWrapper{
		logger: log.New(file, "", log.LstdFlags|log.Lshortfile),
		level:  Levels[options.LogLevel],
	}
}

func (w *stdLoggerWrapper) Debug(v ...any) {
	if w.level <= 0 {
		w.logger.Output(2, fmt.Sprint(v...))
	}
}

func (w *stdLoggerWrapper) Info(v ...any) {
	if w.level <= 1 {
		w.logger.Output(2, fmt.Sprint(v...))
	}
}

func (w *stdLoggerWrapper) Warn(v ...any) {
	if w.level <= 2 {
		w.logger.Output(2, fmt.Sprint(v...))
	}
}

func (w *stdLoggerWrapper) Error(v ...any) {
	if w.level <= 3 {
		w.logger.Output(2, fmt.Sprint(v...))
	}
}

func (w *stdLoggerWrapper) Debugf(format string, v ...any) {
	if w.level <= 0 {
		w.logger.Output(2, fmt.Sprintf(format, v...))
	}
}

func (w *stdLoggerWrapper) Infof(format string, v ...any) {
	if w.level <= 1 {
		w.logger.Output(2, fmt.Sprintf(format, v...))
	}
}

func (w *stdLoggerWrapper) Warnf(format string, v ...any) {
	if w.level <= 2 {
		w.logger.Output(2, fmt.Sprintf(format, v...))
	}
}

func (w *stdLoggerWrapper) Errorf(format string, v ...any) {
	if w.level <= 3 {
		w.logger.Output(2, fmt.Sprintf(format, v...))
	}
}

// GetDefaultLogger returns the default logger
func GetDefaultLogger() Logger {
	return defaultLogger
}

// Debugf logs a message at Debug level
func Debugf(format string, args ...any) {
	GetDefaultLogger().Debugf(format, args...)
}

// Infof logs a message at Info level
func Infof(format string, args ...any) {
	GetDefaultLogger().Infof(format, args...)
}

// Warnf logs a message at Warn level
func Warnf(format string, args ...any) {
	GetDefaultLogger().Warnf(format, args...)
}

// Errorf logs a message at Error level
func Errorf(format string, args ...any) {
	GetDefaultLogger().Errorf(format, args...)
}

// DebugContext logs at Debug level with context
func DebugContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Debug(args...)
}

// DebugContextf logs at Debug level with context
func DebugContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Debugf(format, args...)
}

// InfoContext logs at Info level with context
func InfoContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Info(args...)
}

// InfoContextf logs at Info level with context
func InfoContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Infof(format, args...)
}

// WarnContext logs at Warn level with context
func WarnContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Warn(args...)
}

// WarnContextf logs at Warn level with context
func WarnContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Warnf(format, args...)
}

// ErrorContext logs at Error level with context
func ErrorContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Error(args...)
}

func ErrorContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Errorf(format, args...)
}

func Fatalf(format string, args ...any) {
	Errorf(format, args...)
}
