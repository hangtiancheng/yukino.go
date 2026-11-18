package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdkconfig "github.com/hangtiancheng/yukino-code/apps/yukino/config"
)

func resolveProviders(cfg *Config) error {
	if cfg.ThinkChatModel.ProviderName == "" && cfg.QuickChatModel.ProviderName == "" && cfg.EmbeddingModel.ProviderName == "" {
		return nil
	}
	path := cfg.YukinoConfig
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, path[2:])
	}
	source, err := sdkconfig.LoadConfig(path, sdkconfig.LoadOptions{})
	if err != nil {
		return fmt.Errorf("load Yukino providers: %w", err)
	}
	find := func(name string) (sdkconfig.ProviderConfig, error) {
		for _, provider := range source.Providers {
			if provider.Name == name {
				return provider, nil
			}
		}
		return sdkconfig.ProviderConfig{}, fmt.Errorf("Yukino provider %q not found", name)
	}
	for _, model := range []*ChatModelConfig{&cfg.ThinkChatModel, &cfg.QuickChatModel} {
		if model.ProviderName == "" {
			continue
		}
		provider, err := find(model.ProviderName)
		if err != nil {
			return err
		}
		if model.Model != "" {
			provider.Model = model.Model
		}
		if model.MaxTokens > 0 {
			provider.MaxOutputTokens = float64(model.MaxTokens)
		}
		if model.Thinking {
			provider.Thinking = "high"
		}
		model.ResolvedProvider = &provider
		model.Model = provider.Model
		model.MaxTokens = sdkconfig.GetMaxOutputTokens(&provider)
	}
	if cfg.EmbeddingModel.ProviderName != "" {
		provider, err := find(cfg.EmbeddingModel.ProviderName)
		if err != nil {
			return err
		}
		if provider.Protocol != "openai" && provider.Protocol != "openai-compat" {
			return fmt.Errorf("embedding provider %q must use an OpenAI protocol", provider.Name)
		}
		cfg.EmbeddingModel.Provider = ModelProviderOpenAI
		cfg.EmbeddingModel.APIKey = sdkconfig.ResolveAPIKey(&provider)
		cfg.EmbeddingModel.BaseURL = provider.BaseURL
		if cfg.EmbeddingModel.Model == "" {
			cfg.EmbeddingModel.Model = provider.Model
		}
	}
	return nil
}
