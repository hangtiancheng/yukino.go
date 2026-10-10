package daemon

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
)

func settingsFile(agentName string) string {
	if agentName == agent.Codex {
		return "config.toml"
	}
	return "settings.json"
}

func originalSettings(agentName string) []byte {
	if agentName == agent.Codex {
		return []byte("original = \"value\"\n")
	}
	return []byte("{\"env\":{\"ORIGINAL\":\"value\"}}\n")
}

func checkResponse(agentName, protocol string) string {
	switch protocol {
	case config.Anthropic:
		content := "[]"
		if agentName == agent.Codex {
			content = `[{"type":"text","text":"OK"}]`
		}
		return `{"type":"message","content":` + content + `,"id":"msg_check","role":"assistant","model":"selected-model","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	case config.OpenAICompat:
		return `{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`
	default:
		if agentName == agent.Codex {
			return `{"object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}]}`
		}
		return `{"status":"completed","output":[]}`
	}
}

func testManager(t *testing.T, agentName, protocol string) *Manager {
	t.Helper()
	root := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, checkResponse(agentName, protocol))
	}))
	t.Cleanup(upstream.Close)
	opts := Options{Agent: agentName, Protocol: protocol, ConfigPath: filepath.Join(root, "config.yaml"), AgentDir: filepath.Join(root, agentName), StateDir: filepath.Join(root, "state"), Listen: "127.0.0.1:0"}
	text := "providers:\n  - name: selected\n    protocol: " + protocol + "\n    base_url: " + upstream.URL + "/custom\n    model: selected-model\n    api_key: test-key\n"
	if err := os.WriteFile(opts.ConfigPath, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(opts.AgentDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.AgentDir, settingsFile(agentName)), originalSettings(agentName), 0600); err != nil {
		t.Fatal(err)
	}
	return &Manager{Options: opts}
}

func TestServeShutdownRetainsSettingsAndBackup(t *testing.T) {
	for _, agentName := range agent.Names() {
		for _, protocol := range []string{config.Anthropic, config.OpenAICompat, config.OpenAI} {
			t.Run(agentName+"/"+protocol, func(t *testing.T) {
				manager := testManager(t, agentName, protocol)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				finished := make(chan error, 1)
				go func() { finished <- manager.Serve(ctx, true); close(finished) }()
				defer func() {
					cancel()
					select {
					case <-finished:
					case <-time.After(3 * time.Second):
						t.Error("server leaked")
					}
				}()
				var status Status
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					var err error
					status, err = manager.Status(ctx)
					if err == nil && status.Running {
						break
					}
					select {
					case err := <-finished:
						t.Fatalf("server exited: %v", err)
					default:
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !status.Running {
					t.Fatal("server did not become ready")
				}
				if status.Agent != agentName || status.Name != "selected" || status.BackupPath == "" {
					t.Fatal("incomplete status")
				}
				switch {
				case agentName == agent.Claude && protocol == config.Anthropic:
					if status.Mode != "direct" || status.BaseURL != selectedBaseURL(t, manager) {
						t.Fatal("native endpoint was not selected directly")
					}
				case agentName == agent.Codex:
					if status.Mode != "proxy" || status.BaseURL != status.GatewayURL+"/v1" {
						t.Fatal("Responses gateway was not selected")
					}
				default:
					if status.Mode != "proxy" || status.BaseURL != status.GatewayURL {
						t.Fatal("bridge was not selected")
					}
				}
				unauthorized, err := http.Get(status.GatewayURL + "/_yukino/status")
				if err != nil {
					t.Fatal(err)
				}
				unauthorized.Body.Close()
				if unauthorized.StatusCode != 401 {
					t.Fatal("management endpoint is unauthenticated")
				}
				settingsPath := filepath.Join(manager.Options.AgentDir, settingsFile(agentName))
				configured, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatal(err)
				}
				backup, err := os.ReadFile(status.BackupPath)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(backup, originalSettings(agentName)) {
					t.Fatal("backup is not the original settings")
				}
				state, err := os.ReadFile(manager.statePath())
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(state, []byte("test-key")) {
					t.Fatal("API key leaked to daemon state")
				}
				if err := manager.Shutdown(ctx); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(after, configured) {
					t.Fatal("shutdown restored or changed settings")
				}
				if _, err := os.Stat(status.BackupPath); err != nil {
					t.Fatal("shutdown removed backup")
				}
				current, err := manager.Status(ctx)
				if err != nil || current.Running {
					t.Fatal("shutdown did not stop server")
				}
				if err := manager.Shutdown(ctx); err != nil {
					t.Fatal("repeated shutdown failed")
				}
			})
		}
	}
}

func TestListenFailureDoesNotChangeSettings(t *testing.T) {
	for _, agentName := range agent.Names() {
		t.Run(agentName, func(t *testing.T) {
			manager := testManager(t, agentName, config.OpenAICompat)
			manager.Options.Listen = "0.0.0.0:0"
			path := filepath.Join(manager.Options.AgentDir, settingsFile(agentName))
			before, _ := os.ReadFile(path)
			if err := manager.Serve(context.Background(), true); err == nil {
				t.Fatal("non-loopback listener accepted")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("failed startup changed settings")
			}
		})
	}
}

func selectedBaseURL(t *testing.T, m *Manager) string {
	t.Helper()
	p, err := m.selected()
	if err != nil {
		t.Fatal(err)
	}
	return p.BaseURL
}

func TestFailedConnectionDoesNotChangeSettings(t *testing.T) {
	for _, agentName := range agent.Names() {
		for _, background := range []bool{true, false} {
			t.Run(agentName+"/"+fmt.Sprint(background), func(t *testing.T) {
				manager := testManager(t, agentName, config.OpenAICompat)
				failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
				failing.Close()
				text := "providers:\n  - name: selected\n    protocol: openai-compat\n    base_url: " + failing.URL + "\n    model: model\n    api_key: key\n"
				if err := os.WriteFile(manager.Options.ConfigPath, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(manager.Options.AgentDir, settingsFile(agentName))
				before, _ := os.ReadFile(path)
				var err error
				if background {
					_, err = manager.Start(context.Background())
				} else {
					err = manager.Serve(context.Background(), true)
				}
				if err == nil {
					t.Fatal("unreachable provider was accepted")
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(before, after) {
					t.Fatal("failed connection changed settings")
				}
				backups, _ := filepath.Glob(filepath.Join(manager.Options.AgentDir, "*.bak"))
				if len(backups) != 0 {
					t.Fatal("failed connection created a settings backup")
				}
				if _, err := os.Stat(manager.statePath()); !os.IsNotExist(err) {
					t.Fatal("failed connection published daemon state")
				}
			})
		}
	}
}

func TestConcurrentAgentsAndStateIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	managers := make(map[string]*Manager)
	for _, name := range agent.Names() {
		m := testManager(t, name, config.OpenAICompat)
		managers[name] = m
		finished := make(chan error, 1)
		go func() { finished <- m.Serve(ctx, true) }()
		defer func() {
			cancel()
			select {
			case err := <-finished:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(3 * time.Second):
				t.Error("server leaked")
			}
		}()
		for {
			status, err := m.Status(ctx)
			if err == nil && status.Running {
				if status.Agent != name {
					t.Fatalf("wrong running agent: %+v", status)
				}
				break
			}
			select {
			case err := <-finished:
				t.Fatalf("server exited: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	for _, name := range agent.Names() {
		other := agent.Claude
		if name == agent.Claude {
			other = agent.Codex
		}
		opts := managers[name].Options
		opts.StateDir = managers[other].Options.StateDir
		wrong := &Manager{Options: opts}
		checks := []func() error{
			func() error { _, err := wrong.Status(ctx); return err },
			func() error { return wrong.Shutdown(ctx) },
			func() error { _, err := wrong.Start(ctx); return err },
			func() error { return wrong.Serve(ctx, true) },
		}
		for _, check := range checks {
			if err := check(); err == nil || !strings.Contains(err.Error(), "belongs to agent") {
				t.Fatalf("cross-agent state access was accepted: %v", err)
			}
		}
	}
	if err := managers[agent.Claude].Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if status, err := managers[agent.Codex].Status(ctx); err != nil || !status.Running || status.Agent != agent.Codex {
		t.Fatalf("stopping Claude affected Codex: %+v, %v", status, err)
	}
	if err := managers[agent.Codex].Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
