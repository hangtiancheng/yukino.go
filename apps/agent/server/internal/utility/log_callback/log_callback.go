package log_callback

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/callbacks"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/logger"
)

type Config struct {
	Detail bool
	Debug  bool
}

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
