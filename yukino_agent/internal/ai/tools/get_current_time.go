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
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/utility/logger"
)

// GetCurrentTimeInput is empty as no input parameters are needed.
type GetCurrentTimeInput struct{}

// GetCurrentTimeOutput contains the current time in multiple formats.
type GetCurrentTimeOutput struct {
	Success      bool   `json:"success" jsonschema:"description=Whether the time retrieval was successful"`
	Seconds      int64  `json:"seconds" jsonschema:"description=Current Unix timestamp in seconds"`
	Milliseconds int64  `json:"milliseconds" jsonschema:"description=Current Unix timestamp in milliseconds"`
	Microseconds int64  `json:"microseconds" jsonschema:"description=Current Unix timestamp in microseconds"`
	Timestamp    string `json:"timestamp" jsonschema:"description=Human-readable timestamp in YYYY-MM-DD HH:MM:SS.milliseconds format"`
	Message      string `json:"message" jsonschema:"description=Status message"`
}

// NewGetCurrentTimeTool creates a tool that returns the current system time
// in multiple formats (Unix seconds, milliseconds, microseconds, and human-readable).
// Construction errors are returned to the caller instead of terminating the process.
//
// The tool input is parameter-less, so TolerateEmptyArguments is used to handle
// models that return an empty Arguments string (see empty_arguments.go).
func NewGetCurrentTimeTool() (tool.InvokableTool, error) {
	t, err := utils.InferOptionableTool(
		"get_current_time",
		"Get current system time in multiple formats. Returns Unix timestamp in seconds, milliseconds, and microseconds. Use when you need current time for logging, timing operations, or timestamping events.",
		func(ctx context.Context, input *GetCurrentTimeInput, opts ...tool.Option) (string, error) {
			now := time.Now()
			timestamp := now.Format("2006-01-02 15:04:05.000")
			logger.L().Info("getting current time", "timestamp", timestamp)

			out := GetCurrentTimeOutput{
				Success:      true,
				Seconds:      now.Unix(),
				Milliseconds: now.UnixMilli(),
				Microseconds: now.UnixMicro(),
				Timestamp:    timestamp,
				Message:      "Current time retrieved successfully",
			}
			b, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				logger.L().Warn("marshal current time result", "err", err)
				return "", err
			}
			return string(b), nil
		},
		utils.WithUnmarshalArguments(TolerateEmptyArguments[*GetCurrentTimeInput]()),
	)
	if err != nil {
		return nil, fmt.Errorf("infer get_current_time tool: %w", err)
	}
	return t, nil
}
