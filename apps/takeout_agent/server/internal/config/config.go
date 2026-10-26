// Package config loads takeout_agent runtime settings from a YAML file with
// optional environment variable overrides for secrets.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration object.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	LLM      LLMConfig      `yaml:"llm"`
	Session  SessionConfig  `yaml:"session"`
	Static   StaticConfig   `yaml:"static"`
}

// ServerConfig configures the HTTP listener.
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// Addr returns the listen address, e.g. "127.0.0.1:8080".
func (s ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// DatabaseConfig configures the MySQL connection.
// PasswordEnv, when set, names an environment variable that holds the
// password; it takes precedence over the inline Password field.
type DatabaseConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	PasswordEnv  string `yaml:"password_env"`
	Database     string `yaml:"database"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

// PasswordValue resolves the effective database password.
func (d DatabaseConfig) PasswordValue() string {
	return envOr(d.PasswordEnv, d.Password)
}

// DSN builds a MySQL data source name in go-sql-driver/mysql format, which
// is consumed by GORM's MySQL driver.
//
// clientFoundRows is required for parity with the Java implementation:
// MySQL Connector/J sets CLIENT_FOUND_ROWS by default, so MyBatis reported
// the number of *matched* rows from an UPDATE. go-sql-driver reports
// *changed* rows unless this flag is set. Every write path here treats a
// zero row count as "row missing or not owned by the caller", so without the
// flag a no-op UPDATE (same values written inside the same second, which
// leaves the second-precision updated_at unchanged) would be misreported as
// a failed save or a permission error.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=Local&timeout=5s&clientFoundRows=true",
		d.User, d.PasswordValue(), d.Host, d.Port, d.Database,
	)
}

// LLMConfig configures the OpenAI-compatible chat models.
// Each *_env field names an environment variable that takes precedence over
// the matching inline value, so secrets and endpoint overrides can live in
// the process environment or a .env file instead of the YAML file.
type LLMConfig struct {
	APIKey        string `yaml:"api_key"`
	APIKeyEnv     string `yaml:"api_key_env"`
	BaseURL       string `yaml:"base_url"`
	BaseURLEnv    string `yaml:"base_url_env"`
	MainModel     string `yaml:"main_model"`
	MainModelEnv  string `yaml:"main_model_env"`
	LightModel    string `yaml:"light_model"`
	LightModelEnv string `yaml:"light_model_env"`
	// TimeoutSeconds bounds a single LLM call. Zero or negative disables the
	// timeout. A bound is important because a hung provider call would
	// otherwise hold the per-session lock forever and block that session.
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

// APIKeyValue resolves the effective LLM API key.
func (l LLMConfig) APIKeyValue() string {
	return envOr(l.APIKeyEnv, l.APIKey)
}

// BaseURLValue resolves the effective OpenAI-compatible base URL; empty
// means the provider default.
func (l LLMConfig) BaseURLValue() string {
	return envOr(l.BaseURLEnv, l.BaseURL)
}

// MainModelValue resolves the effective main model name.
func (l LLMConfig) MainModelValue() string {
	return envOr(l.MainModelEnv, l.MainModel)
}

// LightModelValue resolves the effective light model name.
func (l LLMConfig) LightModelValue() string {
	return envOr(l.LightModelEnv, l.LightModel)
}

// CallTimeout resolves the per-call LLM timeout. Zero means no timeout.
func (l LLMConfig) CallTimeout() time.Duration {
	if l.TimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(l.TimeoutSeconds) * time.Second
}

// envOr returns the environment variable named by key when it is set and
// non-empty, otherwise the fallback value.
func envOr(key, fallback string) string {
	if key != "" {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return fallback
}

// SessionConfig tunes conversation history handling.
type SessionConfig struct {
	MaxHistoryTurns int `yaml:"max_history_turns"`
}

// StaticConfig points at the built frontend assets.
type StaticConfig struct {
	Dir string `yaml:"dir"`
}

// Load reads the YAML config file at path and applies defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// Default returns a Config populated with sane defaults.
func Default() *Config {
	return &Config{
		Server: ServerConfig{Host: "127.0.0.1", Port: 8080},
		Database: DatabaseConfig{
			Host:         "127.0.0.1",
			Port:         3306,
			User:         "root",
			Database:     "takeout_db",
			MaxOpenConns: 20,
			MaxIdleConns: 5,
		},
		LLM: LLMConfig{
			APIKeyEnv:      "DASHSCOPE_API_KEY",
			MainModel:      "qwen-plus",
			LightModel:     "qwen-turbo",
			TimeoutSeconds: 60,
		},
		Session: SessionConfig{MaxHistoryTurns: 10},
		Static:  StaticConfig{Dir: "../client/dist"},
	}
}

// ConfigPath resolves the config file path from the TAKEOUT_CONFIG environment
// variable, falling back to the provided default.
func ConfigPath(defaultPath string) string {
	if path := os.Getenv("TAKEOUT_CONFIG"); path != "" {
		return path
	}
	return defaultPath
}

// ParseInt parses a string as int, returning fallback on failure.
func ParseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// LoadDotEnv reads KEY=VALUE pairs from path into the process environment.
// Variables that are already set win over the file, blank lines and #
// comments are ignored, an optional "export " prefix is stripped, and
// surrounding single or double quotes are removed from values. A missing
// file is not an error, which keeps .env optional.
func LoadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read dotenv %s: %w", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("set env %s: %w", key, err)
			}
		}
	}
	return nil
}
