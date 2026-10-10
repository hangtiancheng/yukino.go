package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadJSONC(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.jsonc"))
	if err != nil {
		t.Fatalf("Load(config.example.jsonc): %v", err)
	}

	if cfg.ServerAddr != ":8123" {
		t.Errorf("ServerAddr = %q, want %q", cfg.ServerAddr, ":8123")
	}
	if cfg.ModelProvider != ModelProviderOpenAI {
		t.Errorf("ModelProvider = %q, want %q", cfg.ModelProvider, ModelProviderOpenAI)
	}
	if cfg.ThinkChatModel.Model != "deepseek-flash" {
		t.Errorf("ThinkChatModel.Model = %q, want %q", cfg.ThinkChatModel.Model, "deepseek-flash")
	}
	if cfg.EmbeddingModel.Model != "text-embedding-v4" {
		t.Errorf("EmbeddingModel.Model = %q, want %q", cfg.EmbeddingModel.Model, "text-embedding-v4")
	}
	if cfg.MCP.Transport != MCPTransportStreamableHTTP {
		t.Errorf("MCP.Transport = %q, want %q", cfg.MCP.Transport, MCPTransportStreamableHTTP)
	}
	if cfg.Milvus.CollectionName != "biz" {
		t.Errorf("Milvus.CollectionName = %q, want %q", cfg.Milvus.CollectionName, "biz")
	}
}

func TestLoadJSONCPreservesURLs(t *testing.T) {
	path := writeConfig(t, `{
  // a line comment mentioning "quotes" and // markers
  "think_chat_model": {
    "base_url": "https://api.deepseek.com", // trailing comment
    "model": "m1",
  },
  "quick_chat_model": {
    /* block comment */
    "base_url": "http://localhost:11434/v1",
    "model": "m2",
  },
  "mcp": {
    "url": "http://localhost:3000/mcp",
    // "command": "npx",  <- commented-out alternative
  },
}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.ThinkChatModel.BaseURL, "https://api.deepseek.com"; got != want {
		t.Errorf("ThinkChatModel.BaseURL = %q, want %q", got, want)
	}
	if got, want := cfg.QuickChatModel.BaseURL, "http://localhost:11434/v1"; got != want {
		t.Errorf("QuickChatModel.BaseURL = %q, want %q", got, want)
	}
	if got, want := cfg.MCP.URL, "http://localhost:3000/mcp"; got != want {
		t.Errorf("MCP.URL = %q, want %q", got, want)
	}
	if got, want := cfg.ThinkChatModel.Model, "m1"; got != want {
		t.Errorf("ThinkChatModel.Model = %q, want %q", got, want)
	}
}

func TestStripJSONCDirect(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "line comment removed",
			in:   "{\n// c\n\"a\":1\n}",
			want: "{\n\n\"a\":1\n}",
		},
		{
			name: "url double slash preserved",
			in:   `{"u":"https://x/v1"}`,
			want: `{"u":"https://x/v1"}`,
		},
		{
			name: "trailing comma removed",
			in:   `{"a":1,}`,
			want: `{"a":1}`,
		},
		{
			name: "trailing comma in array removed",
			in:   `{"a":[1,2,]}`,
			want: `{"a":[1,2]}`,
		},
		{
			name: "block comment removed",
			in:   `{"a":/* x */1}`,
			want: `{"a":1}`,
		},
		{
			name: "comment markers inside string preserved",
			in:   `{"a":"// not a comment /* nor this */"}`,
			want: `{"a":"// not a comment /* nor this */"}`,
		},
		{
			name: "escaped quote inside string handled",
			in:   `{"a":"he said \"// hi\"","b":2,}`,
			want: `{"a":"he said \"// hi\"","b":2}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(stripJSONC([]byte(tc.in))); got != tc.want {
				t.Errorf("stripJSONC(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestApplyDefaultsDoesNotForcePrometheusURL(t *testing.T) {
	path := writeConfig(t, `{}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PrometheusURL != "" {
		t.Errorf("PrometheusURL = %q, want empty so the alerts tool stays disabled", cfg.PrometheusURL)
	}
}

func TestApplyDefaults(t *testing.T) {
	path := writeConfig(t, `{}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ServerAddr != ":8123" {
		t.Errorf("ServerAddr = %q, want :8123", cfg.ServerAddr)
	}
	if cfg.ModelProvider != ModelProviderOpenAI {
		t.Errorf("ModelProvider = %q, want openai", cfg.ModelProvider)
	}
	if cfg.FileDir != "./data/docs" {
		t.Errorf("FileDir = %q, want ./data/docs", cfg.FileDir)
	}
	if cfg.Milvus.Addr != "localhost:19530" || cfg.Milvus.DBName != "agent" || cfg.Milvus.CollectionName != "biz" {
		t.Errorf("Milvus = %+v, want localhost:19530/agent/biz", cfg.Milvus)
	}
	if cfg.MCP.Transport != MCPTransportStreamableHTTP {
		t.Errorf("MCP.Transport = %q, want streamable_http", cfg.MCP.Transport)
	}
	if cfg.ThinkChatModel.MaxTokens != 4096 || cfg.QuickChatModel.MaxTokens != 4096 {
		t.Errorf("MaxTokens = think %d / quick %d, want 4096 each",
			cfg.ThinkChatModel.MaxTokens, cfg.QuickChatModel.MaxTokens)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("Load(absent) = nil error, want error")
	}
}
