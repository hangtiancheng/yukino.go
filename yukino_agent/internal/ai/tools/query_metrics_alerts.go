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
	"io"
	"net/http"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/utility/logger"
)

// PrometheusAlert represents a single alert from the Prometheus alerts API.
type PrometheusAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"`
	ActiveAt    string            `json:"activeAt"`
	Value       string            `json:"value"`
}

// PrometheusAlertsResult holds the raw response from Prometheus /api/v1/alerts.
type PrometheusAlertsResult struct {
	Status string `json:"status"`
	Data   struct {
		Alerts []PrometheusAlert `json:"alerts"`
	} `json:"data"`
	Error     string `json:"error,omitempty"`
	ErrorType string `json:"errorType,omitempty"`
}

// SimplifiedAlert is a human-friendly representation of a Prometheus alert.
type SimplifiedAlert struct {
	AlertName   string `json:"alert_name" jsonschema:"description=Alert name from Prometheus labels.alertname"`
	Description string `json:"description" jsonschema:"description=Alert description from annotations.description"`
	State       string `json:"state" jsonschema:"description=Alert state, typically firing or pending"`
	ActiveAt    string `json:"active_at" jsonschema:"description=Activation timestamp in RFC3339 format"`
	Duration    string `json:"duration" jsonschema:"description=Time since activation, e.g. 2h30m15s"`
}

// PrometheusAlertsInput is empty as no input parameters are needed.
type PrometheusAlertsInput struct{}

// PrometheusAlertsOutput is the tool's output structure.
type PrometheusAlertsOutput struct {
	Success bool              `json:"success"`
	Alerts  []SimplifiedAlert `json:"alerts,omitempty"`
	Message string            `json:"message,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// queryPrometheusAlerts queries the Prometheus alerts API at the given base URL.
// An empty baseURL disables the query and returns an empty result, so the tool
// degrades gracefully when Prometheus is not configured.
func queryPrometheusAlerts(baseURL string) (PrometheusAlertsResult, error) {
	if baseURL == "" {
		return PrometheusAlertsResult{}, nil
	}
	apiURL := fmt.Sprintf("%s/api/v1/alerts", baseURL)

	logger.L().Info("querying prometheus alerts", "url", apiURL)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	var result PrometheusAlertsResult

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return result, fmt.Errorf("query Prometheus alerts: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, fmt.Errorf("read response: %w", err)
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// calculateDuration computes the elapsed time from activeAtStr to now.
func calculateDuration(activeAtStr string) string {
	activeAt, err := time.Parse(time.RFC3339Nano, activeAtStr)
	if err != nil {
		return "unknown"
	}

	d := time.Since(activeAt)
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	switch {
	case hours > 0:
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// NewPrometheusAlertsQueryTool creates a tool that queries active Prometheus alerts.
// For alerts with the same alertname, only the first occurrence is returned.
// prometheusURL is the base URL (e.g. "http://127.0.0.1:9090"); empty disables queries.
// Construction errors are returned to the caller instead of terminating the process.
//
// The tool input is parameter-less, so TolerateEmptyArguments is used to handle
// models that return an empty Arguments string (see empty_arguments.go).
func NewPrometheusAlertsQueryTool(prometheusURL string) (tool.InvokableTool, error) {
	t, err := utils.InferOptionableTool(
		"query_prometheus_alerts",
		"Query active alerts from Prometheus alerting system. Retrieves all currently active/firing alerts including name, description, state, active_at, and duration. Same alert name only kept once.",
		func(ctx context.Context, input *PrometheusAlertsInput, opts ...tool.Option) (string, error) {
			logger.L().Info("querying prometheus active alerts")

			result, err := queryPrometheusAlerts(prometheusURL)
			if err != nil {
				// Return a JSON error payload to the LLM instead of a tool error, so
				// the agent can reason about the failure rather than aborting.
				out := PrometheusAlertsOutput{
					Success: false,
					Error:   err.Error(),
					Message: "Failed to query Prometheus alerts",
				}
				b, _ := json.MarshalIndent(out, "", "  ")
				return string(b), nil
			}

			// Deduplicate by alertname, keeping only the first occurrence.
			seen := make(map[string]bool)
			var simplified []SimplifiedAlert
			for _, alert := range result.Data.Alerts {
				name := alert.Labels["alertname"]
				if seen[name] {
					continue
				}
				seen[name] = true
				simplified = append(simplified, SimplifiedAlert{
					AlertName:   name,
					Description: alert.Annotations["description"],
					State:       alert.State,
					ActiveAt:    alert.ActiveAt,
					Duration:    calculateDuration(alert.ActiveAt),
				})
			}

			out := PrometheusAlertsOutput{
				Success: true,
				Alerts:  simplified,
				Message: fmt.Sprintf("Successfully retrieved %d active alerts", len(simplified)),
			}
			b, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal alerts result: %w", err)
			}
			logger.L().Info("prometheus alerts query completed", "count", len(simplified))
			return string(b), nil
		},
		utils.WithUnmarshalArguments(TolerateEmptyArguments[*PrometheusAlertsInput]()),
	)
	if err != nil {
		return nil, fmt.Errorf("infer query_prometheus_alerts tool: %w", err)
	}
	return t, nil
}
