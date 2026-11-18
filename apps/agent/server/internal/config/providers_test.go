package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNamedYukinoProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `providers:
  - name: ds-openai
    protocol: openai-compat
    base_url: https://chat.example/v1
    model: deepseek-test
    api_key: test-chat-key
    thinking: high
    max_output_tokens: 16000
    context_window: 200000
  - name: ali-openai
    protocol: openai-compat
    base_url: https://embedding.example/v1
    model: chat-model
    api_key: test-embedding-key
`
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := writeConfig(t, fmt.Sprintf(`{"yukino_config":%q,"think_chat_model":{"provider_name":"ds-openai"},"quick_chat_model":{"provider_name":"ds-openai","max_tokens":4096},"embedding_model":{"provider_name":"ali-openai","model":"qwen3.7-text-embedding-flash"}}`, path))
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	think, quick := cfg.ThinkChatModel.ResolvedProvider, cfg.QuickChatModel.ResolvedProvider
	if think == nil || quick == nil {
		t.Fatal("providers not resolved")
	}
	if think.Protocol != "openai-compat" || think.Thinking != "high" || think.MaxOutputTokens != 16000 || quick.MaxOutputTokens != 4096 || think == quick {
		t.Fatal("provider settings or independent overrides lost")
	}
	if cfg.EmbeddingModel.Model != "qwen3.7-text-embedding-flash" || cfg.EmbeddingModel.BaseURL != "https://embedding.example/v1" || cfg.EmbeddingModel.APIKey != "test-embedding-key" {
		t.Fatal("embedding profile or model override lost")
	}
	if _, err := Load(writeConfig(t, fmt.Sprintf(`{"yukino_config":%q,"quick_chat_model":{"provider_name":"missing"}}`, path))); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatal("missing provider must fail")
	}
}
