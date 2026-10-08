package codex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func TestConfigurationPreservesUserSettingsAndExactBackup(t *testing.T) {
	dir := t.TempDir()
	original := []byte("# My config\nmodel='old'\nprofile='work'\nmodel_provider='old'\n[profiles.work]\nmodel='override'\nmodel_provider='openai'\napproval_policy='never'\n[mcp_servers.example]\ncommand='example'\n[projects.'/repo']\ntrust_level='trusted'\n")
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	auth := []byte("user credentials remain here")
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), auth, 0600); err != nil {
		t.Fatal(err)
	}
	p := config.Provider{Name: "selected", Protocol: config.Anthropic, Model: "claude-model", APIKey: "secret-key", ContextWindow: 200000, MaxOutputTokens: 4096}
	backup, err := Configure(dir, p, "http://127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(backup)
	if !bytes.Equal(saved, original) {
		t.Fatal("backup must preserve exact original bytes")
	}
	data, _ := os.ReadFile(path)
	if bytes.Contains(data, []byte(p.APIKey)) {
		t.Fatal("upstream credential leaked into Codex config")
	}
	var doc map[string]any
	if toml.Unmarshal(data, &doc) != nil {
		t.Fatal("invalid generated TOML")
	}
	if doc["model"] != p.Model || doc["model_provider"] != "yukino-codex-proxy" {
		t.Fatal("incorrect selected model or route")
	}
	if asMap(doc["mcp_servers"])["example"] == nil || asMap(doc["projects"])["/repo"] == nil {
		t.Fatal("unrelated settings lost")
	}
	profile := asMap(asMap(doc["profiles"])["work"])
	if profile["approval_policy"] != "never" || profile["model_provider"] != nil {
		t.Fatal("profile does not inherit proxy route")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "auth.json"))
	if !bytes.Equal(after, auth) {
		t.Fatal("official login file modified")
	}
	catalog, _ := os.ReadFile(filepath.Join(dir, "yukino-codex-proxy-models.json"))
	var models map[string]any
	if json.Unmarshal(catalog, &models) != nil {
		t.Fatal("invalid model catalog")
	}
	for _, file := range []string{path, backup, filepath.Join(dir, "yukino-codex-proxy-models.json")} {
		info, _ := os.Stat(file)
		if info.Mode().Perm() != 0600 {
			t.Fatal("config, catalog and backups must be private")
		}
	}
}
func TestMalformedConfigurationIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := []byte("broken = [")
	os.WriteFile(path, original, 0600)
	if _, err := Configure(dir, config.Provider{Model: "model"}, "http://localhost"); err == nil {
		t.Fatal("invalid TOML accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, original) {
		t.Fatal("invalid config overwritten")
	}
}
