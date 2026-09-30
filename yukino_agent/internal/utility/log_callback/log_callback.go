// Package log_callback provides a callback handler for the Eino framework
// that logs pipeline component lifecycle events (start/end) via the unified
// slog logger.
package log_callback

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/callbacks"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/utility/logger"
)

// Config controls the verbosity of the log callback handler.
type Config struct {
	// Detail enables logging of input/output payloads.
	Detail bool
	// Debug enables pretty-printed (indented) JSON output.
	Debug bool
}

// NewHandler creates an Eino callback handler that logs component start/end events.
// If config is nil, a default configuration with Detail=true is used.
func NewHandler(config *Config) callbacks.Handler {
	if config == nil {
		config = &Config{Detail: true}
	}

	builder := callbacks.NewHandlerBuilder()
	builder.OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
		logger.L().Info("view start",
			"component", info.Component,
			"type", info.Type,
			"name", info.Name,
		)
		if config.Detail {
			var b []byte
			if config.Debug {
				b, _ = json.MarshalIndent(input, "", "  ")
			} else {
				b, _ = json.Marshal(input)
			}
			logger.L().Info("callback input", "payload", string(b))
		}
		return ctx
	})
	builder.OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
		logger.L().Info("view end",
			"component", info.Component,
			"type", info.Type,
			"name", info.Name,
		)
		return ctx
	})
	builder.OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
		logger.L().Error("view error",
			"component", info.Component,
			"type", info.Type,
			"name", info.Name,
			"error", err,
		)
		return ctx
	})
	return builder.Build()
}
