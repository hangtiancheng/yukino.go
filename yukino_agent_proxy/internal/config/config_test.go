package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrderedSelection(t *testing.T) {
	cfg := Config{Providers: []Provider{
		{Name: "same", Protocol: Anthropic, BaseURL: "https://example.com", Model: "native", APIKey: "key"},
		{Name: "first", Protocol: OpenAICompat, BaseURL: "https://example.com", Model: "first-model", APIKey: "key"},
		{Name: "same", Protocol: OpenAICompat, BaseURL: "https://example.com", Model: "second-model", APIKey: "key"},
		{Name: "same", Protocol: OpenAICompat, BaseURL: "https://example.com", Model: "third-model", APIKey: "key"},
	}}
	for _, tc := range []struct{ protocol, name, model string }{{Anthropic, "", "native"}, {OpenAICompat, "", "first-model"}, {OpenAICompat, "same", "second-model"}} {
		p, err := cfg.Select(tc.protocol, tc.name)
		if err != nil || p.Model != tc.model {
			t.Fatalf("selection = %s, %v; want %s", p.Model, err, tc.model)
		}
	}
	if _, err := cfg.Select(OpenAI, "same"); err == nil {
		t.Fatal("name must not override the protocol filter")
	}
	if _, err := cfg.Select("gemini", ""); err == nil {
		t.Fatal("invalid protocol accepted")
	}
}

func TestFirstInvalidMatchIsNotSkipped(t *testing.T) {
	cfg := Config{Providers: []Provider{{Name: "bad", Protocol: Anthropic}, {Name: "good", Protocol: Anthropic, BaseURL: "https://example.com", Model: "model", APIKey: "key"}}}
	if _, err := cfg.Select(Anthropic, ""); err == nil {
		t.Fatal("invalid first provider was skipped")
	}
}

func TestSharedYAMLAndEnvironmentKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "environment-key")
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "permission_mode: auto\nproviders:\n  - name: provider\n    protocol: openai\n    base_url: https://example.com/v1\n    model: configured-model\nmcp_servers: []\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := cfg.Select(OpenAI, "")
	if err != nil || p.APIKey != "environment-key" {
		t.Fatalf("API key fallback failed: %v", err)
	}
}

func TestMalformedYAMLDoesNotExposeKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("providers: [secret-api-key: broken"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || strings.Contains(err.Error(), "secret-api-key") {
		t.Fatalf("unsafe YAML error: %v", err)
	}
}

func TestThinkingScalarFormats(t *testing.T) {
	for _, value := range []string{"high", "true", "false", "medium"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		data := "providers:\n  - name: provider\n    protocol: anthropic\n    base_url: https://example.com\n    model: model\n    api_key: key\n    thinking: " + value + "\n"
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil || cfg.Providers[0].Thinking != value {
			t.Fatalf("thinking %s is not accepted: %v", value, err)
		}
	}
}

func TestLegacyClaudeThinkingDefaults(t *testing.T) {
	for _, thinking := range []string{"enabled", "adaptive", "HIGH"} {
		t.Run(thinking, func(t *testing.T) {
			p := Provider{Name: "provider", Protocol: OpenAI, BaseURL: "https://example.com", Model: "gpt-5", APIKey: "key", Thinking: thinking}
			validated, err := p.Validate()
			if err != nil || validated.Thinking != "high" {
				t.Fatalf("legacy thinking default %q = %q, %v; want high", thinking, validated.Thinking, err)
			}
		})
	}
}

func TestSelectionModesAndDefaultIndex(t *testing.T) {
	cfg := Config{DefaultProvider: 3, Providers: []Provider{
		{Name: "shared", Protocol: OpenAICompat, BaseURL: "https://example.com", Model: "first-name", APIKey: "key"},
		{Name: "native", Protocol: Anthropic, BaseURL: "https://example.com", Model: "first-protocol", APIKey: "key"},
		{Name: "shared", Protocol: Anthropic, BaseURL: "https://example.com", Model: "first-pair", APIKey: "key"},
		{Name: "shared", Protocol: Anthropic, BaseURL: "https://example.com", Model: "configured-default", APIKey: "key"},
	}}
	for _, tc := range []struct{ protocol, name, model string }{{"", "", "configured-default"}, {"", "shared", "first-name"}, {Anthropic, "", "first-protocol"}, {Anthropic, "shared", "first-pair"}} {
		p, err := cfg.Select(tc.protocol, tc.name)
		if err != nil || p.Model != tc.model {
			t.Fatalf("selection (%q,%q) = %q, %v; expected %q", tc.protocol, tc.name, p.Model, err, tc.model)
		}
	}
	cfg.DefaultProvider = 0
	if p, err := cfg.Select("", ""); err != nil || p.Model != "first-name" {
		t.Fatal("default index zero does not select the first provider")
	}
	for _, tc := range []struct{ protocol, name string }{{OpenAI, ""}, {"", "missing"}, {Anthropic, "missing"}, {OpenAI, "shared"}} {
		if _, err := cfg.Select(tc.protocol, tc.name); err == nil {
			t.Fatalf("unmatched filters (%q,%q) succeeded", tc.protocol, tc.name)
		}
	}
	for _, index := range []int{-1, 4} {
		cfg.DefaultProvider = index
		if _, err := cfg.Select("", ""); err == nil {
			t.Fatal("out-of-range default index was accepted")
		}
		if _, err := cfg.Select("", "shared"); err != nil {
			t.Fatal("unused default index affected an explicit filter")
		}
	}
	if _, err := (&Config{}).Select("", ""); err == nil {
		t.Fatal("empty provider list was accepted")
	}
}

func TestDefaultProviderYAML(t *testing.T) {
	provider := "providers:\n  - name: one\n    protocol: anthropic\n    base_url: https://example.com\n    model: one\n    api_key: key\n  - name: two\n    protocol: openai\n    base_url: https://example.com\n    model: two\n    api_key: key\n"
	for _, tc := range []struct{ value, model string }{{"", "one"}, {"0", "one"}, {"1", "two"}, {"1.5", ""}, {"1.0", ""}, {"\"1\"", ""}, {"true", ""}, {"null", ""}, {"[]", ""}, {"-1", ""}, {"2", ""}} {
		t.Run(tc.value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			text := provider
			if tc.value != "" {
				text += "default_provider: " + tc.value + "\n"
			}
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			var p Provider
			if err == nil {
				p, err = cfg.Select("", "")
			}
			if tc.model == "" {
				if err == nil {
					t.Fatalf("invalid default_provider %q was accepted", tc.value)
				}
			} else if err != nil || p.Model != tc.model {
				t.Fatalf("default_provider %q selected %q: %v", tc.value, p.Model, err)
			}
		})
	}
}

func TestSelectedProviderValidationForEveryMode(t *testing.T) {
	valid := Provider{Name: "same", Protocol: Anthropic, BaseURL: "https://example.com", Model: "valid", APIKey: "key"}
	for _, mode := range []struct{ protocol, name string }{{"", ""}, {"", "same"}, {Anthropic, ""}, {Anthropic, "same"}} {
		cfg := Config{Providers: []Provider{{Name: "same", Protocol: Anthropic, BaseURL: "https://example.com", Model: "", APIKey: "key"}, valid}}
		if _, err := cfg.Select(mode.protocol, mode.name); err == nil {
			t.Fatal("invalid first selection was skipped")
		}
	}
	cfg := Config{Providers: []Provider{{Name: "same", Protocol: "unsupported", BaseURL: "https://example.com", Model: "invalid", APIKey: "key"}, valid}}
	if _, err := cfg.Select("", "same"); err == nil {
		t.Fatal("name-only selection accepted an invalid protocol")
	}
	if _, err := cfg.Select("", ""); err == nil {
		t.Fatal("default selection accepted an invalid protocol")
	}
	if p, err := cfg.Select(Anthropic, ""); err != nil || p.Model != "valid" {
		t.Fatal("unselected invalid provider blocked explicit protocol selection")
	}
}

func TestNativeEnvironmentKeyWithoutProtocolFilter(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "native-env-key")
	t.Setenv("OPENAI_API_KEY", "openai-env-key")
	cfg := Config{Providers: []Provider{{Name: "native", Protocol: Anthropic, BaseURL: "https://example.com", Model: "model"}}}
	for _, name := range []string{"", "native"} {
		if p, err := cfg.Select("", name); err != nil || p.APIKey != "native-env-key" {
			t.Fatalf("wrong environment key for name %q: %v", name, err)
		}
	}
}
