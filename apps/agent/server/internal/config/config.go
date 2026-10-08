// Package config provides configuration loading and types for the yukino_agent application.
// Configuration is loaded from a JSON file and provides settings for AI models,
// vector database connections, and application behavior.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Model provider identifiers selectable via the model_provider config field.
const (
	// ModelProviderOpenAI selects the OpenAI-compatible chat model implementation.
	ModelProviderOpenAI = "openai"
	// ModelProviderAnthropic selects the Anthropic Claude chat model implementation.
	ModelProviderAnthropic = "anthropic"
)

// MCP transport identifiers selectable via the mcp.transport config field.
const (
	// MCPTransportStreamableHTTP connects over streamable HTTP (spec 2025-03-26+).
	MCPTransportStreamableHTTP = "streamable_http"
	// MCPTransportSSE connects over HTTP+SSE (spec 2024-11-05).
	MCPTransportSSE = "sse"
	// MCPTransportStdio spawns a subprocess and talks to it over stdin/stdout.
	MCPTransportStdio = "stdio"
)

// Config holds all application configuration values.
type Config struct {
	// ServerAddr is the address the HTTP server listens on (e.g., ":8123").
	ServerAddr string `json:"server_addr"`

	// ModelProvider selects the chat model implementation: "openai" (default) or "anthropic".
	ModelProvider string `json:"model_provider"`

	// ThinkChatModel configures the LLM used for deep reasoning tasks (planning, replanning).
	ThinkChatModel ChatModelConfig `json:"think_chat_model"`

	// QuickChatModel configures the LLM used for fast chat responses and tool execution.
	QuickChatModel ChatModelConfig `json:"quick_chat_model"`

	// EmbeddingModel configures the embedding model used for vectorization.
	EmbeddingModel EmbeddingConfig `json:"embedding_model"`

	// FileDir is the directory path for storing uploaded knowledge base files.
	FileDir string `json:"file_dir"`

	// MCP configures the connection to the MCP (Model Context Protocol) tool server.
	MCP MCPConfig `json:"mcp"`

	// PrometheusURL is the base URL of the Prometheus server used by the
	// query_prometheus_alerts tool (e.g. "http://127.0.0.1:9090"). Empty disables queries.
	PrometheusURL string `json:"prometheus_url"`

	// LogTopicRegion / LogTopicID configure the log topic context injected into
	// the chat system prompt. Both must be set to include the line; empty omits
	// it.
	LogTopicRegion string `json:"log_topic_region"`
	LogTopicID     string `json:"log_topic_id"`

	// Milvus configures the connection to the Milvus vector database.
	Milvus MilvusConfig `json:"milvus"`
}

// ChatModelConfig holds LLM connection settings for OpenAI-compatible and Anthropic API endpoints.
type ChatModelConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`

	// MaxTokens caps the response length. Required by the Anthropic provider
	// (defaults to 4096 when unset); ignored by the OpenAI provider.
	MaxTokens int `json:"max_tokens"`

	// Thinking enables Anthropic extended thinking (only applies when
	// model_provider=anthropic). When enabled, budgetTokens = MaxTokens-1.
	Thinking bool `json:"thinking"`
}

// EmbeddingConfig holds embedding model settings. The vector dimension is
// probed from the live provider at startup (see embedder.ProbeDimension),
// so no dimension configuration is needed.
type EmbeddingConfig struct {
	// Provider selects the embedding server: "openai" (only)
	Provider string `json:"provider"`

	// OpenAI fields (OpenAI-compatible endpoint).
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
}

// MCPConfig holds the connection settings for the MCP tool server.
// The transport field selects which fields are used:
//   - "streamable_http" / "sse": URL
//   - "stdio": Command, Args, Env
type MCPConfig struct {
	// Transport selects the MCP transport: "streamable_http" (default), "sse" or "stdio".
	Transport string `json:"transport"`

	// URL is the server endpoint used by the streamable_http and sse transports.
	URL string `json:"url"`

	// Command is the executable to spawn for the stdio transport.
	Command string `json:"command"`

	// Args are command-line arguments passed to the stdio command.
	Args []string `json:"args"`

	// Env holds extra environment variables for the stdio command, applied on
	// top of the current process environment.
	Env map[string]string `json:"env"`
}

// MilvusConfig holds Milvus connection settings. The database and collection
// are auto-provisioned on first connect (database "agent", collection "biz").
type MilvusConfig struct {
	Addr string `json:"addr"` // Milvus address, default "localhost:19530".

	// Username / Password are optional; set both when the Milvus deployment
	// has authorization enabled.
	Username string `json:"username"`
	Password string `json:"password"`

	DBName         string `json:"db_name"`         // Milvus database name, default "agent".
	CollectionName string `json:"collection_name"` // Milvus collection name, default "biz".
}

// Load reads and parses the configuration file at the given path.
// The file may be JSON or JSONC (the shipped config.example.jsonc carries //
// comments and trailing commas), so comments and trailing commas are stripped
// before decoding. Missing fields are filled with sensible defaults.
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

// stripJSONC removes // line comments, /* */ block comments and trailing commas
// so JSONC source can be decoded by encoding/json.
//
// String literals are copied verbatim, so comment markers inside strings are
// preserved — notably URLs such as "https://api.example.com/v1", whose "//"
// must not be mistaken for a line comment.
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
				// Line comment: skip to the newline, keeping the newline itself.
				for i < len(src) && src[i] != '\n' {
					i++
				}
				i--
			} else if i+1 < len(src) && src[i+1] == '*' {
				// Block comment: skip through the closing */.
				i += 2
				for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
					i++
				}
				i++
			} else {
				out = append(out, c)
			}
		case ',':
			// Trailing comma: drop it when the next significant token closes the
			// enclosing object or array. Comments count as insignificant, so a
			// comma followed by a comment and then "}" is still trailing.
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

// nextSignificant returns the index of the first byte at or after i that is
// neither JSON whitespace nor part of a comment, or len(b) when only
// whitespace and comments remain.
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

// applyDefaults fills in zero-valued fields with sensible defaults.
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
