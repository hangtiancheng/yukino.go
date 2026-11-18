package models

import (
	"fmt"

	"github.com/hangtiancheng/yukino-code/apps/yukino/config"
	"github.com/hangtiancheng/yukino-code/apps/yukino/llm"
	appconfig "github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func New(cfg *appconfig.Config, mc appconfig.ChatModelConfig, system string) (llm.Client, string, error) {
	protocol := "openai-compat"
	switch cfg.ModelProvider {
	case appconfig.ModelProviderOpenAI:
	case appconfig.ModelProviderAnthropic:
		protocol = "anthropic"
	default:
		return nil, "", fmt.Errorf("unsupported model provider %q", cfg.ModelProvider)
	}
	if mc.BaseURL == "" {
		if protocol == "anthropic" {
			mc.BaseURL = "https://api.anthropic.com"
		} else {
			mc.BaseURL = "https://api.openai.com/v1"
		}
	}
	thinking := "off"
	if mc.Thinking {
		thinking = "high"
	}
	client, err := llm.NewClient(&config.ProviderConfig{Protocol: protocol, Model: mc.Model, APIKey: mc.APIKey, BaseURL: mc.BaseURL, MaxOutputTokens: float64(mc.MaxTokens), Thinking: thinking}, system)
	return client, protocol, err
}
