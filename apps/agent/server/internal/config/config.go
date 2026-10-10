package config

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	ModelProviderOpenAI    = "openai"
	ModelProviderAnthropic = "anthropic"
)

const (
	MCPTransportStreamableHTTP = "streamable_http"
	MCPTransportSSE            = "sse"
	MCPTransportStdio          = "stdio"
)

type Config struct {
	ServerAddr string `json:"server_addr"`

	ModelProvider string `json:"model_provider"`

	ThinkChatModel ChatModelConfig `json:"think_chat_model"`

	QuickChatModel ChatModelConfig `json:"quick_chat_model"`

	EmbeddingModel EmbeddingConfig `json:"embedding_model"`

	FileDir string `json:"file_dir"`

	MCP MCPConfig `json:"mcp"`

	PrometheusURL string `json:"prometheus_url"`

	LogTopicRegion string `json:"log_topic_region"`
	LogTopicID     string `json:"log_topic_id"`

	Milvus MilvusConfig `json:"milvus"`
}

type ChatModelConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`

	MaxTokens int `json:"max_tokens"`

	Thinking bool `json:"thinking"`
}

type EmbeddingConfig struct {
	Provider string `json:"provider"`

	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
}

type MCPConfig struct {
	Transport string `json:"transport"`

	URL string `json:"url"`

	Command string `json:"command"`

	Args []string `json:"args"`

	Env map[string]string `json:"env"`
}

type MilvusConfig struct {
	Addr string `json:"addr"`

	Username string `json:"username"`
	Password string `json:"password"`

	DBName         string `json:"db_name"`
	CollectionName string `json:"collection_name"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(stripJSONC(data), cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}

	applyDefaults(cfg)
	return cfg, nil
}

func stripJSONC(src []byte) []byte {
	out := make([]byte, 0, len(src))
	inString := false
	escaped := false

	for i := 0; i < len(src); i++ {
		c := src[i]

		if inString {
			out = append(out, c)
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
			out = append(out, c)
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					i++
				}
				i--
			} else if i+1 < len(src) && src[i+1] == '*' {
				i += 2
				for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
					i++
				}
				i++
			} else {
				out = append(out, c)
			}
		case ',':
			if j := nextSignificant(src, i+1); j < len(src) && (src[j] == '}' || src[j] == ']') {
				continue
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

func nextSignificant(b []byte, i int) int {
	for i < len(b) {
		switch b[i] {
		case ' ', '\t', '\n', '\r':
			i++
		case '/':
			if i+1 < len(b) && b[i+1] == '/' {
				i += 2
				for i < len(b) && b[i] != '\n' {
					i++
				}
			} else if i+1 < len(b) && b[i+1] == '*' {
				i += 2
				for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
					i++
				}
				i += 2
			} else {
				return i
			}
		default:
			return i
		}
	}
	return i
}

func applyDefaults(cfg *Config) {
	if cfg.ServerAddr == "" {
		cfg.ServerAddr = ":8123"
	}
	if cfg.ModelProvider == "" {
		cfg.ModelProvider = ModelProviderOpenAI
	}
	if cfg.ThinkChatModel.MaxTokens == 0 {
		cfg.ThinkChatModel.MaxTokens = 4096
	}
	if cfg.QuickChatModel.MaxTokens == 0 {
		cfg.QuickChatModel.MaxTokens = 4096
	}
	if cfg.FileDir == "" {
		cfg.FileDir = "./data/docs"
	}
	if cfg.Milvus.Addr == "" {
		cfg.Milvus.Addr = "localhost:19530"
	}
	if cfg.Milvus.DBName == "" {
		cfg.Milvus.DBName = "agent"
	}
	if cfg.Milvus.CollectionName == "" {
		cfg.Milvus.CollectionName = "biz"
	}
	if cfg.EmbeddingModel.Provider == "" {
		cfg.EmbeddingModel.Provider = "openai"
	}
	if cfg.MCP.Transport == "" {
		cfg.MCP.Transport = MCPTransportStreamableHTTP
	}
}
