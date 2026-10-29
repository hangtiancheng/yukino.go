// Package config loads the service configuration: JSON files driven by Go struct tags.
//
// Loading order (later sources override earlier ones):
//  1. In-code defaults (Defaults)
//  2. config/config.json (public config)
//  3. config/secrets.json (passwords etc., git-ignored)
//  4. Env vars (names derived from json tag paths, e.g. mysql.host → MYSQL_HOST;
//     the `env` tag can specify one explicitly, e.g. ideaking.base_url → IDEA_KING_BASE_URL)
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Duration is a JSON-friendly wrapper around time.Duration: serialized as a string like "30s".
type Duration time.Duration

// Std returns the standard-library time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// UnmarshalJSON accepts a string ("30s") or a number (nanoseconds).
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(parsed)
		return nil
	default:
		return fmt.Errorf("config: invalid duration %v", v)
	}
}

// MarshalJSON outputs the time.Duration string form.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// ServerConfig holds the HTTP server listen parameters.
type ServerConfig struct {
	Port string `json:"port"`
}

// JWTConfig holds the JWT signing parameters.
type JWTConfig struct {
	Secret string `json:"secret"`
}

// LogConfig holds the logging parameters.
type LogConfig struct {
	Level string `json:"level"`
}

// MySQLConfig holds the MySQL connection parameters.
type MySQLConfig struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

// DSN builds the MySQL connection string.
func (m MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		m.User, m.Password, m.Host, m.Port, m.Database)
}

// RedisConfig holds the Redis connection parameters.
type RedisConfig struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Password string `json:"password"`
	DB       int    `json:"db"`
}

// Addr returns the connection address in host:port form.
func (r RedisConfig) Addr() string { return r.Host + ":" + r.Port }

// AsyncConfig holds the async pipeline configuration.
type AsyncConfig struct {
	Enabled  bool   `json:"enabled"`
	WorkerID string `json:"worker_id"`
}

// LLMConfig holds the real-LLM access parameters.
// Provider is "openai" (OpenAI-compatible /chat/completions, default) or "claude" (Anthropic).
type LLMConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// CronConfig holds the cron poller configuration.
type CronConfig struct {
	PollInterval Duration `json:"poll_interval"`
}

// OTelConfig holds the OpenTelemetry configuration.
type OTelConfig struct {
	Enabled          bool    `json:"enabled"`
	ExporterEndpoint string  `json:"exporter_endpoint"`
	ServiceName      string  `json:"service_name"`
	SampleRate       float64 `json:"sample_rate"`
}

// BaseServerConfig holds the address of the downstream accounting service (base-server domain, now in-process).
type BaseServerConfig struct {
	BaseURL string `json:"base_url"`
}

// IdeaKingConfig holds the address of the hot-topic retrieval service.
type IdeaKingConfig struct {
	BaseURL string `json:"base_url" env:"IDEA_KING_BASE_URL"`
}

// FakeGenConfig holds the simulated generation service parameters (for local integration).
type FakeGenConfig struct {
	Enabled bool   `json:"enabled"`
	Port    string `json:"port"`
}

// SentryConfig holds the Sentry error-reporting parameters (disabled when the DSN is empty).
type SentryConfig struct {
	DSN string `json:"dsn"`
}

// MetricsConfig holds the Prometheus metrics exposure parameters.
type MetricsConfig struct {
	Enabled bool `json:"enabled"`
}

// Config holds all configuration required to run the service.
type Config struct {
	Server     ServerConfig     `json:"server"`
	JWT        JWTConfig        `json:"jwt"`
	Log        LogConfig        `json:"log"`
	MySQL      MySQLConfig      `json:"mysql"`
	Redis      RedisConfig      `json:"redis"`
	Async      AsyncConfig      `json:"async"`
	LLM        LLMConfig        `json:"llm"`
	Cron       CronConfig       `json:"cron"`
	OTel       OTelConfig       `json:"otel"`
	BaseServer BaseServerConfig `json:"base_server"`
	IdeaKing   IdeaKingConfig   `json:"ideaking"`
	FakeGen    FakeGenConfig    `json:"fake_gen"`
	Sentry     SentryConfig     `json:"sentry"`
	Metrics    MetricsConfig    `json:"metrics"`
}

// Defaults returns the configuration with default values.
func Defaults() *Config {
	return &Config{
		Server: ServerConfig{Port: "8080"},
		JWT:    JWTConfig{Secret: "dev-secret-change-me"},
		Log:    LogConfig{Level: "info"},
		MySQL: MySQLConfig{
			Host:     "127.0.0.1",
			Port:     "3306",
			User:     "root",
			Database: "all_in_one",
		},
		Redis: RedisConfig{
			Host: "127.0.0.1",
			Port: "6379",
		},
		Async: AsyncConfig{Enabled: true},
		Cron:  CronConfig{PollInterval: Duration(30 * time.Second)},
		OTel: OTelConfig{
			Enabled:          false,
			ExporterEndpoint: "localhost:4317",
			ServiceName:      "yukino-creative-agent",
			SampleRate:       1.0,
		},
		LLM:     LLMConfig{Provider: "openai"},
		FakeGen: FakeGenConfig{Port: "8091"},
		Metrics: MetricsConfig{Enabled: true},
	}
}

// Load loads the configuration from config files and env vars, falling back to defaults when missing.
// Loading order: defaults → configPath (public config) → config/secrets.json → env vars.
// Missing files are silently skipped (same behavior as the old viper setup).
func Load(configPath string) *Config {
	cfg := Defaults()
	if configPath != "" {
		_ = MergeFile(cfg, configPath)
	}
	_ = MergeFile(cfg, "config/secrets.json")
	ApplyEnv(cfg, os.LookupEnv)
	return cfg
}

// MergeFile merges a JSON config file into cfg: keys present in the file override existing values; keys absent from it stay unchanged.
func MergeFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	return nil
}

// ApplyEnv derives env-var names from json tag paths and overrides the corresponding fields.
// lookup is usually os.LookupEnv; tests can inject a custom implementation.
func ApplyEnv(cfg *Config, lookup func(string) (string, bool)) {
	applyEnvStruct(reflect.ValueOf(cfg).Elem(), "", lookup)
}

func applyEnvStruct(v reflect.Value, prefix string, lookup func(string) (string, bool)) {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			name = strings.ToLower(field.Name)
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.Struct && fv.Type() != reflect.TypeOf(Duration(0)) {
			applyEnvStruct(fv, path, lookup)
			continue
		}
		envKey := field.Tag.Get("env")
		if envKey == "" {
			envKey = strings.ToUpper(strings.ReplaceAll(path, ".", "_"))
		}
		raw, ok := lookup(envKey)
		if !ok || raw == "" {
			continue
		}
		if err := setFieldFromString(fv, raw); err != nil {
			continue
		}
	}
}

func setFieldFromString(fv reflect.Value, raw string) error {
	if fv.Type() == reflect.TypeOf(Duration(0)) {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		fv.SetInt(int64(d))
		return nil
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	default:
		return fmt.Errorf("config: unsupported field kind %s", fv.Kind())
	}
	return nil
}
