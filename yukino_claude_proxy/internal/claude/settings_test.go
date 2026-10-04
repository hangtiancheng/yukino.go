package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/config"
)

func TestConfigureKeepsExactBackupAndUserSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte("{\n  \"hooks\": {\"keep\": true},\n  \"model\": \"old\",\n  \"apiKeyHelper\": \"old-helper\",\n  \"env\": {\"DEBUG\": \"1\", \"ANTHROPIC_API_KEY\": \"old-key\", \"CLAUDE_CODE_USE_VERTEX\": \"1\"}\n}\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	p := config.Provider{Name: "example", Protocol: config.OpenAICompat, Model: "actual-model", APIKey: "new-secret"}
	backup, err := Configure(dir, p, "http://127.0.0.1:17861")
	if err != nil {
		t.Fatal(err)
	}
	backedUp, _ := os.ReadFile(backup)
	if !bytes.Equal(backedUp, original) {
		t.Fatal("backup changed original bytes")
	}
	data, _ := os.ReadFile(path)
	var settings map[string]any
	_ = json.Unmarshal(data, &settings)
	env := settings["env"].(map[string]any)
	if env["DEBUG"] != "1" || settings["hooks"] == nil {
		t.Fatal("user settings were lost")
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != ProxyToken || bytes.Contains(data, []byte("new-secret")) {
		t.Fatal("proxy credentials leaked into Claude settings")
	}
	if env["ANTHROPIC_API_KEY"] != nil || env["CLAUDE_CODE_USE_VERTEX"] != nil || settings["apiKeyHelper"] != nil {
		t.Fatal("conflicting routing fields survived")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("settings permissions are not private")
	}
	p.Protocol = config.Anthropic
	p.BaseURL = "https://example.com/anthropic"
	second, err := Configure(dir, p, "http://unused")
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	_ = json.Unmarshal(data, &settings)
	env = settings["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != p.BaseURL || env["ANTHROPIC_API_KEY"] != p.APIKey || env["ANTHROPIC_AUTH_TOKEN"] != nil {
		t.Fatal("native Anthropic configuration is incorrect")
	}
	if second == backup {
		t.Fatal("a later switch overwrote the original backup")
	}
}

func TestInvalidSettingsAreNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	data := []byte(`{"env":"invalid"}`)
	_ = os.WriteFile(path, data, 0600)
	if _, err := Configure(dir, config.Provider{}, "http://localhost"); err == nil {
		t.Fatal("invalid settings accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("invalid settings were overwritten")
	}
}
