package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/components/tool/utils"
)

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
