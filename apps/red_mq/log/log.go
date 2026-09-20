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
	"io"
	"log"
	"os"
)

// Logger is the logging surface used by red_mq.
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

var defaultLogger Logger

func init() {
	defaultLogger = NewLogger(NewOptions())
}

// Options holds logger configuration.
type Options struct {
	LogName  string
	LogLevel string
	FileName string
	Writer   io.Writer
}

// Option mutates Options.
type Option func(*Options)

// NewOptions builds default Options and applies the given Option list.
func NewOptions(opts ...Option) Options {
	options := Options{
		LogName:  "app",
		LogLevel: "info",
		FileName: "",
		Writer:   os.Stdout,
	}
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// WithLogLevel sets the log level (debug, info, warn, error, fatal).
func WithLogLevel(level string) Option {
	return func(o *Options) {
		o.LogLevel = level
	}
}

// WithFileName redirects log output to the given file.
func WithFileName(filename string) Option {
	return func(o *Options) {
		o.FileName = filename
	}
}

// WithWriter overrides the underlying writer.
func WithWriter(w io.Writer) Option {
	return func(o *Options) {
		o.Writer = w
	}
}

// Level represents log severity.
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// Levels maps string level names to Level values.
var Levels = map[string]Level{
	"":      DebugLevel,
	"debug": DebugLevel,
	"info":  InfoLevel,
	"warn":  WarnLevel,
	"error": ErrorLevel,
	"fatal": FatalLevel,
}

// stdLogger wraps the standard library log.Logger with leveled filtering.
type stdLogger struct {
	logger *log.Logger
	level  Level
}

// NewLogger constructs a Logger from Options.
func NewLogger(options Options) Logger {
	writer := options.Writer
	if writer == nil {
		if options.FileName != "" {
			f, err := os.OpenFile(options.FileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				panic(err)
			}
			writer = f
		} else {
			writer = os.Stdout
		}
	}

	return &stdLogger{
		logger: log.New(writer, "", log.LstdFlags|log.Lshortfile|log.Lmsgprefix),
		level:  Levels[options.LogLevel],
	}
}

func (l *stdLogger) output(callDepth int, level string, msg string) {
	l.logger.Output(callDepth+1, fmt.Sprintf("[%s] %s", level, msg))
}

func (l *stdLogger) Debug(v ...any) {
	if l.level <= DebugLevel {
		l.output(2, "DEBUG", fmt.Sprint(v...))
	}
}

func (l *stdLogger) Info(v ...any) {
	if l.level <= InfoLevel {
		l.output(2, "INFO", fmt.Sprint(v...))
	}
}

func (l *stdLogger) Warn(v ...any) {
	if l.level <= WarnLevel {
		l.output(2, "WARN", fmt.Sprint(v...))
	}
}

func (l *stdLogger) Error(v ...any) {
	if l.level <= ErrorLevel {
		l.output(2, "ERROR", fmt.Sprint(v...))
	}
}

func (l *stdLogger) Debugf(format string, v ...any) {
	if l.level <= DebugLevel {
		l.output(2, "DEBUG", fmt.Sprintf(format, v...))
	}
}

func (l *stdLogger) Infof(format string, v ...any) {
	if l.level <= InfoLevel {
		l.output(2, "INFO", fmt.Sprintf(format, v...))
	}
}

func (l *stdLogger) Warnf(format string, v ...any) {
	if l.level <= WarnLevel {
		l.output(2, "WARN", fmt.Sprintf(format, v...))
	}
}

func (l *stdLogger) Errorf(format string, v ...any) {
	if l.level <= ErrorLevel {
		l.output(2, "ERROR", fmt.Sprintf(format, v...))
	}
}

// GetDefaultLogger returns the package-level default Logger.
func GetDefaultLogger() Logger {
	return defaultLogger
}

func Debugf(format string, args ...any) {
	GetDefaultLogger().Debugf(format, args...)
}

func Infof(format string, args ...any) {
	GetDefaultLogger().Infof(format, args...)
}

func Warnf(format string, args ...any) {
	GetDefaultLogger().Warnf(format, args...)
}

func Errorf(format string, args ...any) {
	GetDefaultLogger().Errorf(format, args...)
}

func DebugContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Debug(args...)
}

func DebugContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Debugf(format, args...)
}

func InfoContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Info(args...)
}

func InfoContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Infof(format, args...)
}

func WarnContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Warn(args...)
}

func WarnContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Warnf(format, args...)
}

func ErrorContext(ctx context.Context, args ...any) {
	GetDefaultLogger().Error(args...)
}

func ErrorContextf(ctx context.Context, format string, args ...any) {
	GetDefaultLogger().Errorf(format, args...)
}

func Fatalf(format string, args ...any) {
	Errorf(format, args...)
}
