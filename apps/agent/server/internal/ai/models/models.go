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
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func NewThinkChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	return newChatModel(ctx, cfg, cfg.ThinkChatModel)
}

func NewQuickChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	return newChatModel(ctx, cfg, cfg.QuickChatModel)
}

func newChatModel(ctx context.Context, cfg *config.Config, mc config.ChatModelConfig) (model.ToolCallingChatModel, error) {
	if cfg.ModelProvider == config.ModelProviderAnthropic {
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
