// Package config 加载服务配置：JSON 文件 + Go struct tag 驱动。
//
// 加载顺序（后者覆盖前者）：
//  1. 代码内默认值（Defaults）
//  2. config/config.json（公开配置）
//  3. config/secrets.json（密码等，Git 忽略）
//  4. 环境变量（键名由 json tag 路径派生，如 mysql.host → MYSQL_HOST；
//     可用 `env` tag 显式指定，如 ideaking.base_url → IDEA_KING_BASE_URL）
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

// Duration 是 time.Duration 的 JSON 友好包装：序列化为 "30s" 之类的字符串。
type Duration time.Duration

// Std 返回标准库 time.Duration。
func (d Duration) Std() time.Duration { return time.Duration(d) }

// UnmarshalJSON 接受字符串（"30s"）或数字（纳秒）。
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

// MarshalJSON 输出 time.Duration 字符串形式。
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// ServerConfig 保存 HTTP 服务监听参数。
type ServerConfig struct {
	Port string `json:"port"`
}

// JWTConfig 保存 JWT 签名参数。
type JWTConfig struct {
	Secret string `json:"secret"`
}

// LogConfig 保存日志参数。
type LogConfig struct {
	Level string `json:"level"`
}

// MySQLConfig 保存 MySQL 连接参数。
type MySQLConfig struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

// DSN 生成 MySQL 连接字符串。
func (m MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		m.User, m.Password, m.Host, m.Port, m.Database)
}

// RedisConfig 保存 Redis 连接参数。
type RedisConfig struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Password string `json:"password"`
	DB       int    `json:"db"`
}

// Addr 返回 host:port 形式的连接地址。
func (r RedisConfig) Addr() string { return r.Host + ":" + r.Port }

// AsyncConfig 保存异步管道相关配置。
type AsyncConfig struct {
	Enabled  bool   `json:"enabled"`
	WorkerID string `json:"worker_id"`
}

// LLMConfig 保存真实 LLM 接入参数。
// Provider 取值 "openai"（OpenAI 兼容 /chat/completions，默认）或 "claude"（Anthropic）。
type LLMConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// CronConfig 保存 Cron Poller 相关配置。
type CronConfig struct {
	PollInterval Duration `json:"poll_interval"`
}

// OTelConfig 保存 OpenTelemetry 相关配置。
type OTelConfig struct {
	Enabled          bool    `json:"enabled"`
	ExporterEndpoint string  `json:"exporter_endpoint"`
	ServiceName      string  `json:"service_name"`
	SampleRate       float64 `json:"sample_rate"`
}

// BaseServerConfig 保存下游记账服务（base-server 域，现已同进程）地址。
type BaseServerConfig struct {
	BaseURL string `json:"base_url"`
}

// IdeaKingConfig 保存热点/选题检索服务地址。
type IdeaKingConfig struct {
	BaseURL string `json:"base_url" env:"IDEA_KING_BASE_URL"`
}

// FakeGenConfig 保存模拟生成服务参数（本地联调用）。
type FakeGenConfig struct {
	Enabled bool   `json:"enabled"`
	Port    string `json:"port"`
}

// SentryConfig 保存 Sentry 错误上报参数（DSN 为空时不启用）。
type SentryConfig struct {
	DSN string `json:"dsn"`
}

// MetricsConfig 保存 Prometheus 指标暴露参数。
type MetricsConfig struct {
	Enabled bool `json:"enabled"`
}

// Config 保存服务运行所需的全部配置。
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

// Defaults 返回带默认值的配置。
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

// Load 从配置文件与环境变量加载配置，缺失时回退到默认值。
// 加载顺序：默认值 → configPath（公开配置）→ config/secrets.json → 环境变量。
// 文件不存在时静默跳过（与旧 viper 行为一致）。
func Load(configPath string) *Config {
	cfg := Defaults()
	if configPath != "" {
		_ = MergeFile(cfg, configPath)
	}
	_ = MergeFile(cfg, "config/secrets.json")
	ApplyEnv(cfg, os.LookupEnv)
	return cfg
}

// MergeFile 将 JSON 配置文件合并进 cfg：文件中出现的键覆盖现有值，未出现的保持不变。
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

// ApplyEnv 按 json tag 路径派生环境变量名并覆盖对应字段。
// lookup 通常传 os.LookupEnv；测试可注入自定义实现。
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
