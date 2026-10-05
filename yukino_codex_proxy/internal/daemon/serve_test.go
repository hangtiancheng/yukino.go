package daemon

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_codex_proxy/internal/config"
)

func testManager(t *testing.T, protocol string) *Manager {
	t.Helper()
	root := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch protocol {
		case config.Anthropic:
			_, _ = fmt.Fprint(w, `{"type":"message","content":[{"type":"text","text":"OK"}],"id":"msg_check","role":"assistant","model":"selected-model","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
		case config.OpenAICompat:
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`)
		case config.OpenAI:
			_, _ = fmt.Fprint(w, `{"object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}]}`)
		}
	}))
	t.Cleanup(upstream.Close)
	opts := Options{Protocol: protocol, ConfigPath: filepath.Join(root, "config.yaml"), CodexDir: filepath.Join(root, "codex"), StateDir: filepath.Join(root, "state"), Listen: "127.0.0.1:0"}
	text := "providers:\n  - name: selected\n    protocol: " + protocol + "\n    base_url: " + upstream.URL + "/custom\n    model: selected-model\n    api_key: test-key\n"
	if err := os.WriteFile(opts.ConfigPath, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(opts.CodexDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.CodexDir, "config.toml"), []byte("original = \"value\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return &Manager{Options: opts}
}

func TestServeShutdownRetainsSettingsAndBackup(t *testing.T) {
	for _, protocol := range []string{config.Anthropic, config.OpenAICompat, config.OpenAI} {
		t.Run(protocol, func(t *testing.T) {
			manager := testManager(t, protocol)
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
			if status.Name != "selected" || status.BackupPath == "" {
				t.Fatal("incomplete status")
			}
			if status.Mode != "proxy" || status.CodexBaseURL != status.GatewayURL+"/v1" {
				t.Fatal("Responses gateway was not selected")
			}
			unauthorized, err := http.Get(status.GatewayURL + "/_yukino/status")
			if err != nil {
				t.Fatal(err)
			}
			unauthorized.Body.Close()
			if unauthorized.StatusCode != 401 {
				t.Fatal("management endpoint is unauthenticated")
			}
			configured, err := os.ReadFile(filepath.Join(manager.Options.CodexDir, "config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			backup, err := os.ReadFile(status.BackupPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(backup) != "original = \"value\"\n" {
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
			after, err := os.ReadFile(filepath.Join(manager.Options.CodexDir, "config.toml"))
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

func TestListenFailureDoesNotChangeSettings(t *testing.T) {
	manager := testManager(t, config.OpenAICompat)
	manager.Options.Listen = "0.0.0.0:0"
	path := filepath.Join(manager.Options.CodexDir, "config.toml")
	before, _ := os.ReadFile(path)
	if err := manager.Serve(context.Background(), true); err == nil {
		t.Fatal("non-loopback listener accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed startup changed settings")
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
	for _, background := range []bool{true, false} {
		t.Run(fmt.Sprint(background), func(t *testing.T) {
			manager := testManager(t, config.OpenAICompat)
			failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
			failing.Close()
			text := "providers:\n  - name: selected\n    protocol: openai-compat\n    base_url: " + failing.URL + "\n    model: model\n    api_key: key\n"
			if err := os.WriteFile(manager.Options.ConfigPath, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(manager.Options.CodexDir, "config.toml")
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
			backups, _ := filepath.Glob(filepath.Join(manager.Options.CodexDir, "*.bak"))
			if len(backups) != 0 {
				t.Fatal("failed connection created a settings backup")
			}
			if _, err := os.Stat(manager.statePath()); !os.IsNotExist(err) {
				t.Fatal("failed connection published daemon state")
			}
		})
	}
}
