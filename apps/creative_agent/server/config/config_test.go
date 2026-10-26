package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
)

func noEnv(string) (string, bool) { return "", false }

func TestDefaults(t *testing.T) {
	cfg := config.Defaults()
	if cfg.Server.Port != "8080" {
		t.Fatalf("server.port = %q, want 8080", cfg.Server.Port)
	}
	if cfg.Log.Level != "info" {
		t.Fatalf("log.level = %q, want info", cfg.Log.Level)
	}
	if cfg.MySQL.Host != "127.0.0.1" || cfg.MySQL.Port != "3306" {
		t.Fatalf("mysql addr = %s:%s, want 127.0.0.1:3306", cfg.MySQL.Host, cfg.MySQL.Port)
	}
	if got := cfg.Redis.Addr(); got != "127.0.0.1:6379" {
		t.Fatalf("redis addr = %q, want 127.0.0.1:6379", got)
	}
	if cfg.OTel.Enabled {
		t.Fatal("otel.enabled default should be false")
	}
	if cfg.OTel.ServiceName != "yukino-creative-agent" {
		t.Fatalf("otel.service_name = %q", cfg.OTel.ServiceName)
	}
	if cfg.Cron.PollInterval.Std() != 30*time.Second {
		t.Fatalf("cron.poll_interval = %v, want 30s", cfg.Cron.PollInterval.Std())
	}
}

func TestMergeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{
	  "server": { "port": "3000" },
	  "jwt": { "secret": "file-secret" },
	  "mysql": { "host": "db.local", "port": "3307" },
	  "cron": { "poll_interval": "10s" },
	  "otel": { "enabled": true, "sample_rate": 0.5 }
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	if err := config.MergeFile(cfg, path); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != "3000" {
		t.Fatalf("server.port = %q, want 3000", cfg.Server.Port)
	}
	if cfg.JWT.Secret != "file-secret" {
		t.Fatalf("jwt.secret = %q", cfg.JWT.Secret)
	}
	if cfg.MySQL.Host != "db.local" || cfg.MySQL.Port != "3307" {
		t.Fatalf("mysql = %+v", cfg.MySQL)
	}
	// 未出现的字段保持默认值
	if cfg.MySQL.User != "root" {
		t.Fatalf("mysql.user = %q, want default root", cfg.MySQL.User)
	}
	if cfg.Redis.Addr() != "127.0.0.1:6379" {
		t.Fatalf("redis addr = %q, want default", cfg.Redis.Addr())
	}
	if cfg.Cron.PollInterval.Std() != 10*time.Second {
		t.Fatalf("cron.poll_interval = %v, want 10s", cfg.Cron.PollInterval.Std())
	}
	if !cfg.OTel.Enabled || cfg.OTel.SampleRate != 0.5 {
		t.Fatalf("otel = %+v", cfg.OTel)
	}
}

func TestMergeFileMissing(t *testing.T) {
	cfg := config.Defaults()
	if err := config.MergeFile(cfg, filepath.Join(t.TempDir(), "absent.json")); err != nil {
		t.Fatalf("missing file should be ignored, got %v", err)
	}
}

func TestApplyEnv(t *testing.T) {
	env := map[string]string{
		"SERVER_PORT":          "9090",
		"JWT_SECRET":           "env-secret",
		"MYSQL_HOST":           "db.example.com",
		"REDIS_HOST":           "redis.example.com",
		"REDIS_PORT":           "6380",
		"REDIS_DB":             "3",
		"OTEL_ENABLED":         "true",
		"OTEL_SAMPLE_RATE":     "0.25",
		"CRON_POLL_INTERVAL":   "5s",
		"IDEA_KING_BASE_URL":   "http://idea.local",
		"BASE_SERVER_BASE_URL": "http://base.local",
		"FAKE_GEN_ENABLED":     "1",
	}
	cfg := config.Defaults()
	config.ApplyEnv(cfg, func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	})

	if cfg.Server.Port != "9090" {
		t.Fatalf("server.port = %q", cfg.Server.Port)
	}
	if cfg.JWT.Secret != "env-secret" {
		t.Fatalf("jwt.secret = %q", cfg.JWT.Secret)
	}
	if cfg.MySQL.Host != "db.example.com" {
		t.Fatalf("mysql.host = %q", cfg.MySQL.Host)
	}
	if cfg.Redis.Addr() != "redis.example.com:6380" || cfg.Redis.DB != 3 {
		t.Fatalf("redis = %+v", cfg.Redis)
	}
	if !cfg.OTel.Enabled || cfg.OTel.SampleRate != 0.25 {
		t.Fatalf("otel = %+v", cfg.OTel)
	}
	if cfg.Cron.PollInterval.Std() != 5*time.Second {
		t.Fatalf("cron.poll_interval = %v", cfg.Cron.PollInterval.Std())
	}
	if cfg.IdeaKing.BaseURL != "http://idea.local" {
		t.Fatalf("ideaking.base_url = %q", cfg.IdeaKing.BaseURL)
	}
	if cfg.BaseServer.BaseURL != "http://base.local" {
		t.Fatalf("base_server.base_url = %q", cfg.BaseServer.BaseURL)
	}
	if !cfg.FakeGen.Enabled {
		t.Fatal("fake_gen.enabled should be true")
	}
}

func TestApplyEnvNoVars(t *testing.T) {
	cfg := config.Defaults()
	config.ApplyEnv(cfg, noEnv)
	if cfg.Server.Port != "8080" {
		t.Fatalf("defaults should be kept, got %q", cfg.Server.Port)
	}
}

func TestLoadPriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"server":{"port":"3000"},"jwt":{"secret":"file"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JWT_SECRET", "env-wins")

	cfg := config.Load(path)
	if cfg.Server.Port != "3000" {
		t.Fatalf("file value should win over default, got %q", cfg.Server.Port)
	}
	if cfg.JWT.Secret != "env-wins" {
		t.Fatalf("env should win over file, got %q", cfg.JWT.Secret)
	}
}

func TestDurationJSON(t *testing.T) {
	type wrapper struct {
		Interval config.Duration `json:"interval"`
	}
	var w wrapper
	if err := json.Unmarshal([]byte(`{"interval":"1m30s"}`), &w); err != nil {
		t.Fatal(err)
	}
	if w.Interval.Std() != 90*time.Second {
		t.Fatalf("interval = %v, want 1m30s", w.Interval.Std())
	}
	out, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"interval":"1m30s"}` {
		t.Fatalf("marshal = %s", out)
	}
}

func TestMySQLDSN(t *testing.T) {
	cfg := config.MySQLConfig{
		Host:     "db.example.com",
		Port:     "3306",
		User:     "appuser",
		Password: "secret",
		Database: "mydb",
	}
	dsn := cfg.DSN()
	for _, want := range []string{
		"appuser:secret@tcp(db.example.com:3306)/mydb",
		"charset=utf8mb4",
		"parseTime=True",
	} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("DSN %q should contain %q", dsn, want)
		}
	}
}

func TestLoadLLMFromJSONAndEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	content := `{
	  "llm": {
	    "provider": "openai",
	    "base_url": "https://api.deepseek.com/v1",
	    "model": "deepseek-v4-flash"
	  }
	}`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// 从 JSON 文件加载
	cfg := config.Defaults()
	if err := config.MergeFile(cfg, configPath); err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("llm.base_url = %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != "deepseek-v4-flash" {
		t.Fatalf("llm.model = %q", cfg.LLM.Model)
	}

	// 环境变量覆盖
	config.ApplyEnv(cfg, func(key string) (string, bool) {
		switch key {
		case "LLM_BASE_URL":
			return "https://env.example.com/v1", true
		case "LLM_MODEL":
			return "env-model", true
		}
		return "", false
	})
	if cfg.LLM.BaseURL != "https://env.example.com/v1" {
		t.Fatalf("llm.base_url = %q, want env override", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != "env-model" {
		t.Fatalf("llm.model = %q, want env override", cfg.LLM.Model)
	}
}

func TestLoadSecretsOverridesConfig(t *testing.T) {
	dir := t.TempDir()

	// 公开配置（无密码）
	configPath := filepath.Join(dir, "config.json")
	configJSON := `{
	  "mysql": { "host": "db.local", "port": "3306" },
	  "redis": { "host": "cache.local", "port": "6379" }
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	// 密码文件
	secretsPath := filepath.Join(dir, "secrets.json")
	secretsJSON := `{
	  "mysql": { "password": "my-secret-password" },
	  "redis": { "password": "redis-pwd" }
	}`
	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	// Load 的合并顺序：默认值 → config.json → secrets.json → 环境变量。
	cfg := config.Defaults()
	if err := config.MergeFile(cfg, configPath); err != nil {
		t.Fatal(err)
	}
	if err := config.MergeFile(cfg, secretsPath); err != nil {
		t.Fatal(err)
	}

	if cfg.MySQL.Host != "db.local" {
		t.Fatalf("mysql.host = %q, want db.local", cfg.MySQL.Host)
	}
	if cfg.MySQL.Password != "my-secret-password" {
		t.Fatalf("mysql.password = %q, want secrets value", cfg.MySQL.Password)
	}
	if cfg.Redis.Host != "cache.local" {
		t.Fatalf("redis.host = %q, want cache.local", cfg.Redis.Host)
	}
	if cfg.Redis.Password != "redis-pwd" {
		t.Fatalf("redis.password = %q, want secrets value", cfg.Redis.Password)
	}
}
