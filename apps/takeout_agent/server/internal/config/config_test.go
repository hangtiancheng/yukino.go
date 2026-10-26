package config

import (
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
