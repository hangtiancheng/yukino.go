package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/components/tool/utils"
)

// TolerateEmptyArguments is a utils.UnmarshalArguments implementation that
// tolerates models (e.g. Qwen3.7) returning an empty string for the tool-call
// Arguments of a parameter-less tool instead of the "{}" the JSON spec requires.
//
// eino's default sonic.UnmarshalString treats "" as a syntax error, which
// surfaces as "[LocalFunc] failed to unmarshal arguments in json ... the input
// json is empty" and breaks the executor. This wrapper normalizes any
// empty/whitespace-only arguments to "{}" before decoding into a new T, so the
// zero-value input is produced as expected.
//
// Usage with InferOptionableTool:
//
//	utils.InferOptionableTool(name, desc, fn,
//	    utils.WithUnmarshalArguments(TolerateEmptyArguments[MyInput]()))
//
// T must be a pointer to the tool's input struct (e.g. *GetCurrentTimeInput).
func TolerateEmptyArguments[T any]() utils.UnmarshalArguments {
	return func(ctx context.Context, arguments string) (any, error) {
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}
		var input T
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return nil, err
		}
		return input, nil
	}
}
