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
	"fmt"
	"io"
	"log"
	"os"
)

var defaultLogger *Logger

func init() {
	defaultLogger = NewLogger(NewOptions())
}

func GetLogger() *Logger {
	return defaultLogger
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
	"debug": DebugLevel,
	"info":  InfoLevel,
	"warn":  WarnLevel,
	"error": ErrorLevel,
	"fatal": FatalLevel,
}

// Logger wraps the standard library log.Logger with leveled filtering.
type Logger struct {
	logger *log.Logger
	level  Level
}

// NewLogger constructs a Logger from Options.
func NewLogger(options Options) *Logger {
	writer := options.Writer
	if writer == nil {
		writer = os.Stdout
	}
	return &Logger{
		logger: log.New(writer, "", log.LstdFlags|log.Lshortfile|log.Lmsgprefix),
		level:  Levels[options.LogLevel],
	}
}

func (l *Logger) Debugf(format string, v ...any) {
	if l.level <= DebugLevel {
		l.logger.Output(3, fmt.Sprintf("[DEBUG] "+format, v...))
	}
}

func (l *Logger) Infof(format string, v ...any) {
	if l.level <= InfoLevel {
		l.logger.Output(3, fmt.Sprintf("[INFO] "+format, v...))
	}
}

func (l *Logger) Warnf(format string, v ...any) {
	if l.level <= WarnLevel {
		l.logger.Output(3, fmt.Sprintf("[WARN] "+format, v...))
	}
}

func (l *Logger) Errorf(format string, v ...any) {
	if l.level <= ErrorLevel {
		l.logger.Output(3, fmt.Sprintf("[ERROR] "+format, v...))
	}
}

func (l *Logger) Fatalf(format string, v ...any) {
	if l.level <= FatalLevel {
		l.logger.Output(3, fmt.Sprintf("[FATAL] "+format, v...))
	}
}
