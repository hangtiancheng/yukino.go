package agent

import (
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
)

func TestNames(t *testing.T) {
	names := Names()
	if len(names) != 2 || names[0] != Claude || names[1] != Codex {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestGet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		wantErr bool
	}{
		{Claude, false},
		{Codex, false},
		{"", true},
		{"gemini", true},
	} {
		a, err := Get(tc.name)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("Get(%q) expected an error", tc.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("Get(%q): %v", tc.name, err)
		}
		if a.Name() != tc.name {
			t.Fatalf("Get(%q).Name() = %q", tc.name, a.Name())
		}
	}
}

func TestAgentDefaults(t *testing.T) {
	claude, err := Get(Claude)
	if err != nil {
		t.Fatal(err)
	}
	codex, err := Get(Codex)
	if err != nil {
		t.Fatal(err)
	}
	if claude.DefaultListen() != "127.0.0.1:17861" || claude.StateDirName() != "claude-proxy" {
		t.Fatal("unexpected claude defaults")
	}
	if codex.DefaultListen() != "127.0.0.1:17862" || codex.StateDirName() != "codex-proxy" {
		t.Fatal("unexpected codex defaults")
	}
	if claude.SettingsLabel() != "Claude Code" || codex.SettingsLabel() != "Codex" {
		t.Fatal("unexpected settings labels")
	}
	if claudeDir, err := claude.DefaultDir(); err != nil || claudeDir == "" {
		t.Fatalf("claude default dir: %v %q", err, claudeDir)
	}
	if codexDir, err := codex.DefaultDir(); err != nil || codexDir == "" {
		t.Fatalf("codex default dir: %v %q", err, codexDir)
	}
}

func TestModeAndBaseURL(t *testing.T) {
	claude, _ := Get(Claude)
	codex, _ := Get(Codex)
	native := config.Provider{Protocol: config.Anthropic, BaseURL: "https://native.example/anthropic"}
	openai := config.Provider{Protocol: config.OpenAI, BaseURL: "https://api.example"}
	if claude.Mode(native) != "direct" || claude.BaseURL(native, "http://127.0.0.1:1") != native.BaseURL {
		t.Fatal("claude native mode should connect directly")
	}
	if claude.Mode(openai) != "proxy" || claude.BaseURL(openai, "http://127.0.0.1:1") != "http://127.0.0.1:1" {
		t.Fatal("claude bridge mode should use the local gateway")
	}
	if codex.Mode(native) != "proxy" || codex.BaseURL(native, "http://127.0.0.1:1") != "http://127.0.0.1:1/v1" {
		t.Fatal("codex always uses the local Responses gateway")
	}
}

func TestAgentImplementsInterface(t *testing.T) {
	var _ Agent = claudeAgent{}
	var _ Agent = codexAgent{}
}
