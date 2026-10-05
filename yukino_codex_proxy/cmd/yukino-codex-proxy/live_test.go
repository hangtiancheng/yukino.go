package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_codex_proxy/internal/codex"
	"github.com/hangtiancheng/yukino.go/yukino_codex_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_codex_proxy/internal/daemon"
)

var liveCodex = flag.Bool("live-codex", false, "Run the installed Codex CLI with real providers and temporary configuration")

func TestLiveCodex(t *testing.T) {
	if !*liveCodex {
		t.Skip("pass -args -live-codex to enable real Codex integration")
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{config.OpenAICompat, config.Anthropic} {
		t.Run(protocol, func(t *testing.T) {
			root := t.TempDir()
			opts, err := daemon.Defaults()
			if err != nil {
				t.Fatal(err)
			}
			opts.Protocol = protocol
			opts.CodexDir = filepath.Join(root, "codex")
			opts.StateDir = filepath.Join(root, "state")
			opts.Listen = "127.0.0.1:0"
			manager := daemon.Manager{Options: opts}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			status, err := manager.Start(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
				defer stop()
				if err := manager.Shutdown(cleanup); err != nil {
					t.Error(err)
				}
			}()
			t.Logf("provider=%s model=%s", status.Name, status.Model)
			target, _ := url.Parse(status.GatewayURL)
			probe := httputil.NewSingleHostReverseProxy(target)
			probe.ModifyResponse = func(response *http.Response) error {
				if response.StatusCode >= 400 {
					data, _ := io.ReadAll(response.Body)
					response.Body.Close()
					response.Body = io.NopCloser(bytes.NewReader(data))
					t.Logf("gateway HTTP %d: %s", response.StatusCode, data)
				}
				return nil
			}
			front := httptest.NewServer(probe)
			defer front.Close()
			cfg, err := config.Load(opts.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			p, err := cfg.Select(protocol, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codex.Configure(opts.CodexDir, p, front.URL); err != nil {
				t.Fatal(err)
			}
			workDir := filepath.Join(root, "work")
			if err := os.Mkdir(workDir, 0700); err != nil {
				t.Fatal(err)
			}
			marker := "yukino-proxy-marker-7c51a9"
			if err := os.WriteFile(filepath.Join(workDir, "marker.txt"), []byte(marker), 0600); err != nil {
				t.Fatal(err)
			}
			answerPath := filepath.Join(root, "answer.txt")
			cmd := exec.CommandContext(ctx, codexPath, "exec", "--strict-config", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "--json", "-C", workDir, "-c", `model_reasoning_effort="high"`, "-c", "model_providers.yukino-codex-proxy.request_max_retries=0", "-c", "model_providers.yukino-codex-proxy.stream_max_retries=0", "-o", answerPath, "Use the shell tool to read marker.txt in the current directory. Reply with only its exact contents.")
			for _, value := range os.Environ() {
				key, _, _ := strings.Cut(value, "=")
				if key != "CODEX_HOME" && !strings.EqualFold(key, "no_proxy") {
					cmd.Env = append(cmd.Env, value)
				}
			}
			cmd.Env = append(cmd.Env, "CODEX_HOME="+opts.CodexDir, "NO_PROXY=127.0.0.1,localhost,::1", "no_proxy=127.0.0.1,localhost,::1")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("Codex failed: %v\n%s\n%s", err, stdout.String(), stderr.String())
			}
			answer, err := os.ReadFile(answerPath)
			if err != nil || !strings.Contains(string(answer), marker) {
				t.Fatalf("Codex did not return the file contents: %v\n%s\n%s", err, stdout.String(), stderr.String())
			}
			if !bytes.Contains(stdout.Bytes(), []byte("command_execution")) {
				t.Fatalf("Codex did not exercise a shell tool: %s", stdout.String())
			}
		})
	}
}
