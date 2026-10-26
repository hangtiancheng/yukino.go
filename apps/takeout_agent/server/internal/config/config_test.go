package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDSNIncludesClientFoundRows(t *testing.T) {
	d := DatabaseConfig{Host: "db", Port: 3306, User: "root", Password: "pw", Database: "takeout_db"}
	dsn := d.DSN()
	if !strings.Contains(dsn, "clientFoundRows=true") {
		t.Fatalf("DSN = %q, want clientFoundRows=true so UPDATE reports matched rows like MyBatis/Connector-J", dsn)
	}
	if !strings.Contains(dsn, "root:pw@tcp(db:3306)/takeout_db") {
		t.Fatalf("DSN = %q, want the user/password/host/database prefix", dsn)
	}
}

func TestCallTimeout(t *testing.T) {
	if got := (LLMConfig{TimeoutSeconds: 0}).CallTimeout(); got != 0 {
		t.Fatalf("CallTimeout(0) = %v, want 0 (disabled)", got)
	}
	if got := (LLMConfig{TimeoutSeconds: 45}).CallTimeout(); got != 45*time.Second {
		t.Fatalf("CallTimeout(45) = %v, want 45s", got)
	}
	if got := (LLMConfig{TimeoutSeconds: -1}).CallTimeout(); got != 0 {
		t.Fatalf("CallTimeout(-1) = %v, want 0 (disabled)", got)
	}
}

func TestDefaultHasLLMTimeout(t *testing.T) {
	cfg := Default()
	if cfg.LLM.TimeoutSeconds <= 0 {
		t.Fatalf("Default().LLM.TimeoutSeconds = %d, want a positive bound so a hung LLM call cannot deadlock a session", cfg.LLM.TimeoutSeconds)
	}
}

func TestLoadEmptyFileKeepsDefaults(t *testing.T) {
	for name, content := range map[string]string{
		"empty":        "",
		"comment-only": "# nothing here\n",
		"doc-marker":   "---\n",
	} {
		path := filepath.Join(t.TempDir(), name+".yaml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load(%s): %v", name, err)
		}
		if *cfg != *Default() {
			t.Fatalf("Load(%s) = %+v, want untouched defaults %+v", name, cfg, Default())
		}
	}
}

func TestLoadPartialFileMergesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 9090 || cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("server = %+v, want port overridden and host defaulted", cfg.Server)
	}
	if cfg.Database.MaxOpenConns != 20 {
		t.Fatalf("database defaults lost: %+v", cfg.Database)
	}
}
