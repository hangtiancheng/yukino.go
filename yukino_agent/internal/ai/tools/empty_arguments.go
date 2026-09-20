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
