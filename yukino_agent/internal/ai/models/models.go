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

// Package models provides factory functions for creating LLM chat model instances.
// It supports both OpenAI-compatible API endpoints and Anthropic Claude for
// different reasoning use cases, selected via the model_provider configuration.
package models

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
)

// NewThinkChatModel creates a chat model instance configured for deep reasoning tasks.
// This model is used by the planner and replanner in the plan-execute-replan pipeline.
func NewThinkChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	return newChatModel(ctx, cfg, cfg.ThinkChatModel)
}

// NewQuickChatModel creates a chat model instance configured for fast responses.
// This model is used for standard chat interactions and tool execution steps.
func NewQuickChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	return newChatModel(ctx, cfg, cfg.QuickChatModel)
}

// newChatModel builds a chat model from the given model settings, selecting the
// underlying implementation based on cfg.ModelProvider. It defaults to the
// OpenAI-compatible implementation unless "anthropic" is explicitly configured.
//
// For Anthropic, extended thinking is enabled when mc.Thinking is set, with
// budgetTokens = mc.MaxTokens - 1 (mirrors Next.js ANTHROPIC_THINKING). A
// signature-patching HTTP transport is injected to backfill the missing
// `signature` field on thinking blocks returned by non-official Anthropic
// gateways, mirroring the Next.js createAnthropicFetch workaround.
func newChatModel(ctx context.Context, cfg *config.Config, mc config.ChatModelConfig) (model.ToolCallingChatModel, error) {
	if cfg.ModelProvider == config.ModelProviderAnthropic {
		// Claude's BaseURL is optional; nil falls back to the default Anthropic endpoint.
		var baseURL *string
		if mc.BaseURL != "" {
			baseURL = &mc.BaseURL
		}
		claudeCfg := &claude.Config{
			APIKey:    mc.APIKey,
			BaseURL:   baseURL,
			Model:     mc.Model,
			MaxTokens: mc.MaxTokens,
			HTTPClient: &http.Client{
				Transport: &signaturePatchingTransport{base: http.DefaultTransport},
			},
		}
		if mc.Thinking && mc.MaxTokens > 1 {
			claudeCfg.ThinkingConfig = &anthropic.ThinkingConfigParamUnion{
				OfEnabled: &anthropic.ThinkingConfigEnabledParam{
					BudgetTokens: int64(mc.MaxTokens - 1),
				},
			}
		}
		return claude.NewChatModel(ctx, claudeCfg)
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		Model:   mc.Model,
		APIKey:  mc.APIKey,
		BaseURL: mc.BaseURL,
	})
}

// signaturePatchingTransport wraps an http.RoundTripper to backfill the
// `signature` field on thinking content blocks returned by non-official
// Anthropic-compatible gateways. The official Anthropic API always includes
// `signature` on non-streaming thinking blocks; some proxies omit it, which
// makes the Anthropic SDK reject the response with "Invalid JSON response".
// Streaming (SSE) responses are passed through unchanged.
//
// Mirrors the Next.js createAnthropicFetch workaround in lib/ai/models.ts.
type signaturePatchingTransport struct {
	base http.RoundTripper
}

func (t *signaturePatchingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		return resp, nil
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return resp, err
	}

	patched, changed := patchThinkingSignature(body)
	if !changed {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}
	resp.Body = io.NopCloser(bytes.NewReader(patched))
	resp.Header.Set("Content-Length", strconv.Itoa(len(patched)))
	resp.ContentLength = int64(len(patched))
	return resp, nil
}

// patchThinkingSignature parses an Anthropic message response and backfills
// `signature: ""` on any thinking content block missing it. Returns the
// (possibly modified) body bytes and whether a change was made.
func patchThinkingSignature(body []byte) ([]byte, bool) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body, false
	}
	if obj["type"] != "message" {
		return body, false
	}
	content, ok := obj["content"].([]any)
	if !ok {
		return body, false
	}
	changed := false
	for _, c := range content {
		block, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if block["type"] != "thinking" {
			continue
		}
		if _, has := block["signature"]; !has {
			block["signature"] = ""
			changed = true
		}
	}
	if !changed {
		return body, false
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return body, false
	}
	return out, true
}
