// Package codex projects the local Responses gateway into Codex configuration.
package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/config"
	"github.com/pelletier/go-toml/v2"
)

const ProxyToken = "YUKINO_CODEX_PROXY_MANAGED"

func DefaultDir() (string, error) {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// Configure retains an exact byte backup and leaves auth.json untouched.
// Shutdown never restores or removes either the config or its backups.
func Configure(dir string, p config.Provider, gateway string) (string, error) {
	var err error
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "config.toml")
	original, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		original = nil
	} else if err != nil {
		return "", fmt.Errorf("read Codex configuration: %w", err)
	}
	settings := map[string]any{}
	if err := toml.Unmarshal(original, &settings); err != nil {
		return "", fmt.Errorf("Codex config.toml must be valid TOML")
	}
	providers, ok := settings["model_providers"].(map[string]any)
	if settings["model_providers"] != nil && !ok {
		return "", fmt.Errorf("Codex model_providers must be a table")
	}
	if providers == nil {
		providers = map[string]any{}
	}
	// Newer Codex rejects custom entries shadowing reserved built-in IDs.
	for _, id := range []string{"openai", "ollama", "lmstudio"} {
		delete(providers, id)
	}
	providers["yukino-codex-proxy"] = map[string]any{
		"name": "Yukino Codex Proxy", "base_url": gateway + "/v1", "wire_api": "responses",
		"experimental_bearer_token": ProxyToken, "requires_openai_auth": false, "supports_websockets": false,
	}
	settings["model_providers"] = providers
	settings["model_provider"] = "yukino-codex-proxy"
	settings["model"] = p.Model
	settings["model_catalog_json"] = filepath.Join(dir, "yukino-codex-proxy-models.json")
	settings["web_search"] = "disabled"
	delete(settings, "openai_base_url")
	delete(settings, "forced_login_method")
	delete(settings, "model_context_window")
	delete(settings, "model_max_output_tokens")
	if p.ContextWindow > 0 {
		settings["model_context_window"] = p.ContextWindow
	}
	for _, raw := range asMap(settings["profiles"]) {
		profile := asMap(raw)
		for _, key := range []string{"model_provider", "model", "model_catalog_json", "openai_base_url", "model_context_window", "model_max_output_tokens"} {
			delete(profile, key)
		}
	}
	data, err := toml.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("encode Codex configuration: %w", err)
	}
	backup := path + ".yukino-codex-proxy." + time.Now().UTC().Format("20060102T150405.000000000Z") + ".bak"
	f, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("create Codex backup: %w", err)
	}
	_, writeErr := f.Write(original)
	closeErr := f.Close()
	if writeErr != nil {
		return backup, writeErr
	}
	if closeErr != nil {
		return backup, closeErr
	}
	catalog, err := json.MarshalIndent(modelCatalog(p), "", "  ")
	if err != nil {
		return backup, err
	}
	if err := WritePrivate(filepath.Join(dir, "yukino-codex-proxy-models.json"), append(catalog, '\n')); err != nil {
		return backup, err
	}
	if err := WritePrivate(path, data); err != nil {
		return backup, fmt.Errorf("write Codex configuration (backup: %s): %w", backup, err)
	}
	return backup, nil
}

func asMap(value any) map[string]any { m, _ := value.(map[string]any); return m }

func modelCatalog(p config.Provider) map[string]any {
	window := p.ContextWindow
	if window == 0 {
		window = 128000
	}
	effort := p.Thinking
	switch effort {
	case "", "true":
		effort = "high"
	case "off", "false":
		effort = "none"
	case "max":
		effort = "xhigh"
	}
	levels := []any{}
	for _, level := range []string{"none", "minimal", "low", "medium", "high", "xhigh"} {
		levels = append(levels, map[string]any{"effort": level, "description": "Reasoning effort: " + level})
	}
	return map[string]any{"models": []any{map[string]any{
		"slug": p.Model, "display_name": p.Model, "description": p.Name,
		"default_reasoning_level": effort, "supported_reasoning_levels": levels,
		"shell_type": "shell_command", "visibility": "list", "supported_in_api": true, "priority": 0,
		"base_instructions":            "You are a coding assistant. Use the available tools to complete the user's task.",
		"supports_reasoning_summaries": true, "default_reasoning_summary": "none", "support_verbosity": false,
		"truncation_policy": map[string]any{"mode": "bytes", "limit": 10000}, "supports_parallel_tool_calls": true,
		"supports_image_detail_original": false, "context_window": window, "max_context_window": window,
		"effective_context_window_percent": 95, "experimental_supported_tools": []any{}, "input_modalities": []string{"text", "image"}, "supports_search_tool": false,
	}}}
}

// WritePrivate atomically replaces a private file, including on initial creation.
func WritePrivate(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".yukino-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
