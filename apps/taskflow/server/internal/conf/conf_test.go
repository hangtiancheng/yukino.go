package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConf(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "conf.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadComposesDSNFromFields(t *testing.T) {
	path := writeConf(t, `
server:
  port: 8090
mysql:
  host: db.example
  port: 3307
  user: app
  password_env: TEST_MYSQL_PASSWORD
  database: taskflow
redis:
  address: cache.example:6380
  db: 2
`)
	t.Setenv("TEST_MYSQL_PASSWORD", "s3cret")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := "app:s3cret@tcp(db.example:3307)/taskflow?charset=utf8mb4&parseTime=True&loc=UTC&timeout=5s&time_zone=%27%2B00%3A00%27"
	if got := cfg.MySQL.DSN(cfg.MySQL.Database); got != want {
		t.Fatalf("DSN = %q, want %q", got, want)
	}
	if cfg.Redis.Address != "cache.example:6380" || cfg.Redis.DB != 2 {
		t.Fatalf("redis endpoint not preserved: %+v", cfg.Redis)
	}
}

func TestLoadExpandsEnvInValues(t *testing.T) {
	path := writeConf(t, `
mysql:
  password_env: P
redis:
  address: "${TEST_REDIS_ADDR}"
`)
	t.Setenv("TEST_REDIS_ADDR", "10.0.0.1:6379")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Redis.Address != "10.0.0.1:6379" {
		t.Fatalf("expandEnv failed: address = %q", cfg.Redis.Address)
	}
}

func TestRepairDefaults(t *testing.T) {
	path := writeConf(t, "server:\n  port: 0\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 || cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("server defaults: %+v", cfg.Server)
	}
	if cfg.MySQL.Database != "taskflow" || cfg.MySQL.PasswordEnv != "MYSQL_PASSWORD" {
		t.Fatalf("mysql defaults: %+v", cfg.MySQL)
	}
	if cfg.Redis.Address != "127.0.0.1:6379" {
		t.Fatalf("redis default: %+v", cfg.Redis)
	}
	if cfg.Scheduler.RecoverWorkers != 8 {
		t.Fatalf("RecoverWorkers default = %d", cfg.Scheduler.RecoverWorkers)
	}
	if cfg.Journal.Dir != "./data/journal" {
		t.Fatalf("Journal.Dir default = %q", cfg.Journal.Dir)
	}
	if cfg.Consensus.ID != 1 {
		t.Fatalf("Consensus.ID default = %d", cfg.Consensus.ID)
	}
	if cfg.Node.ID == "" {
		t.Fatal("Node.ID should default to hostname-pid")
	}
}

func TestBaseURLRewritesWildcardHost(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"0.0.0.0", "http://127.0.0.1:8090"},
		{"::", "http://127.0.0.1:8090"},
		{"", "http://127.0.0.1:8090"},
		{"10.1.2.3", "http://10.1.2.3:8090"},
	}
	for _, c := range cases {
		s := ServerConf{Host: c.host, Port: 8090}
		if got := s.BaseURL(); got != c.want {
			t.Fatalf("BaseURL(host=%q) = %q, want %q", c.host, got, c.want)
		}
	}
}

func TestNoEnvFileIsLoaded(t *testing.T) {
	// A stale .env-style variable in the environment must not be picked up by
	// Load: the yaml is the single config source. Only the *_env field names
	// (read at use time) and ${VAR} expansion are honored.
	path := writeConf(t, "mysql:\n  password_env: TEST_MYSQL_PASSWORD\n")
	t.Setenv("TEST_MYSQL_PASSWORD", "from-shell")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(cfg.MySQL.DSN("db"), "from-shell") {
		t.Fatalf("password_env not read from process env: %q", cfg.MySQL.DSN("db"))
	}
}
