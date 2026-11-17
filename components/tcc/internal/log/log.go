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

type Options struct {
	LogName    string
	LogLevel   string
	FileName   string
	MaxAge     int
	MaxSize    int
	MaxBackups int
	Compress   bool
}

type Option func(*Options)

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

func WithLogLevel(level string) Option {
	return func(o *Options) {
		o.LogLevel = level
	}
}

func WithFileName(filename string) Option {
	return func(o *Options) {
		o.FileName = filename
	}
}

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
