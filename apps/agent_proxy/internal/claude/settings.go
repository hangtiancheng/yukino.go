// Package claude projects a selected provider into Claude Code settings.
package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
)

const ProxyToken = "YUKINO_PROXY_MANAGED"

func DefaultDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// Configure saves the exact previous bytes before changing routing fields.
// Backups are never restored or deleted automatically.
func Configure(dir string, p config.Provider, proxyURL string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "settings.json")
	original, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		original = []byte("{}\n")
	} else if err != nil {
		return "", fmt.Errorf("read Claude Code settings: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(original, &settings); err != nil || settings == nil {
		return "", fmt.Errorf("Claude Code settings must be a JSON object")
	}
	env := make(map[string]any)
	if old, exists := settings["env"]; exists {
		var ok bool
		env, ok = old.(map[string]any)
		if !ok {
			return "", fmt.Errorf("Claude Code settings.env must be a JSON object")
		}
	}
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_SUBAGENT_MODEL"} {
		delete(env, key)
	}
	delete(settings, "model")
	delete(settings, "apiKeyHelper")
	if p.Protocol == config.Anthropic {
		env["ANTHROPIC_BASE_URL"] = p.BaseURL
		env["ANTHROPIC_API_KEY"] = p.APIKey
	} else {
		env["ANTHROPIC_BASE_URL"] = proxyURL
		env["ANTHROPIC_AUTH_TOKEN"] = ProxyToken
	}
	for _, key := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL"} {
		env[key] = p.Model
	}
	// Declare the selected provider's context window for custom model IDs.
	// Clear a previous provider's value when this provider has no declaration.
	delete(env, "CLAUDE_CODE_MAX_CONTEXT_TOKENS")
	if p.ContextWindow > 0 {
		env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = strconv.FormatInt(p.ContextWindow, 10)
	}
	settings["env"] = env
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	backup := path + ".yukino-claude-proxy." + time.Now().UTC().Format("20060102T150405.000000000Z") + ".bak"
	if err := os.WriteFile(backup, original, 0600); err != nil {
		return "", fmt.Errorf("back up Claude Code settings: %w", err)
	}
	if err := WritePrivate(path, append(data, '\n')); err != nil {
		return backup, fmt.Errorf("write Claude Code settings (backup: %s): %w", backup, err)
	}
	return backup, nil
}

// WritePrivate replaces a file atomically, without exposing partial JSON.
func WritePrivate(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".yukino-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
