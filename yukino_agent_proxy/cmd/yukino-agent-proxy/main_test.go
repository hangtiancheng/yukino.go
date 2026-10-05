package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/agent"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/daemon"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Exercise the production entry point from the test executable, including the
// detached child and MCP stdio transport.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "start", "status", "shutdown", "mcp", "_serve":
			main()
			return
		}
	}
	os.Exit(m.Run())
}

func TestBackgroundCLIAndMCP(t *testing.T) {
	for _, agentName := range agent.Names() {
		t.Run(agentName, func(t *testing.T) {
			runAgentCLIAndMCP(t, agentName)
		})
	}
}

// agentExpectation captures the per-agent settings file, backup prefix, the
// mode used for a native Anthropic selection, and the canned upstream replies
// that each agent's connection check accepts.
type agentExpectation struct {
	settingsFile   string
	backupGlob     string
	nativeMode     string
	anthropicReply string
	responsesReply string
}

func expectationFor(agentName string) agentExpectation {
	if agentName == agent.Codex {
		return agentExpectation{
			settingsFile:   "config.toml",
			backupGlob:     "config.toml.yukino-codex-proxy.*.bak",
			nativeMode:     "proxy",
			anthropicReply: `{"type":"message","content":[{"type":"text","text":"OK"}],"id":"msg_check","role":"assistant","model":"native-model","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
			responsesReply: `{"object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}]}`,
		}
	}
	return agentExpectation{
		settingsFile:   "settings.json",
		backupGlob:     "settings.json.yukino-claude-proxy.*.bak",
		nativeMode:     "direct",
		anthropicReply: `{"type":"message","content":[],"id":"msg_check","role":"assistant","model":"native-model","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
		responsesReply: `{"status":"completed","output":[]}`,
	}
}

func runAgentCLIAndMCP(t *testing.T, agentName string) {
	t.Helper()
	expect := expectationFor(agentName)
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.yaml")
	settingsDir := filepath.Join(root, agentName)
	stateDir := filepath.Join(root, "state")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] == "bad-model" {
			w.WriteHeader(401)
			_, _ = fmt.Fprint(w, `{"error":{"message":"bad-secret-key"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/anthropic/v1/messages":
			_, _ = fmt.Fprint(w, expect.anthropicReply)
		case "/v1/chat/completions":
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}]}`)
		case "/v1/responses":
			_, _ = fmt.Fprint(w, expect.responsesReply)
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	text := fmt.Sprintf("providers:\n  - name: shared\n    protocol: anthropic\n    base_url: %s/anthropic\n    model: native-model\n    api_key: native-test-key\n  - name: shared\n    protocol: openai-compat\n    base_url: %s/v1\n    model: chat-model-first\n    api_key: chat-test-key\n  - name: shared\n    protocol: openai-compat\n    base_url: %s/v1\n    model: chat-model-default\n    api_key: chat-test-key\n  - name: responses\n    protocol: openai\n    base_url: %s/v1\n    model: responses-model\n    api_key: responses-key\n  - name: bad\n    protocol: openai-compat\n    base_url: %s/v1\n    model: bad-model\n    api_key: bad-secret-key\ndefault_provider: 2\n", upstream.URL, upstream.URL, upstream.URL, upstream.URL, upstream.URL)
	if err := os.WriteFile(configPath, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	common := []string{"--agent", agentName, "--config", configPath, "--agent-dir", settingsDir, "--state-dir", stateDir, "--listen", "127.0.0.1:0"}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	call := func(command string, flags ...string) ([]byte, error) {
		args := append([]string{command}, common...)
		args = append(args, flags...)
		cmd := exec.CommandContext(ctx, executable, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if bytes.Contains(stderr.Bytes(), []byte("bad-secret-key")) {
			t.Fatal("API key leaked in CLI diagnostics")
		}
		if err != nil {
			t.Logf("%s: %s", command, stderr.String())
		}
		return output, err
	}
	defer func() { _, _ = call("shutdown") }()
	start := func(flags ...string) daemon.Status {
		output, err := call("start", flags...)
		if err != nil {
			t.Fatal(err)
		}
		var status daemon.Status
		if json.Unmarshal(output, &status) != nil || !status.Running {
			t.Fatalf("invalid CLI status: %s", output)
		}
		return status
	}
	currentStatus := func() daemon.Status {
		output, err := call("status")
		if err != nil {
			t.Fatal(err)
		}
		var status daemon.Status
		if err := json.Unmarshal(output, &status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	first := start()
	if first.Agent != agentName || first.Mode != "proxy" || first.Model != "chat-model-default" {
		t.Fatal("default_provider did not select the exact array index")
	}
	repeated := start()
	if repeated.PID != first.PID || repeated.BackupPath != first.BackupPath {
		t.Fatal("repeated start was not idempotent")
	}
	if _, err := call("start", "--protocol", "openai", "--name", "shared"); err == nil {
		t.Fatal("mismatched protocol/name filters succeeded")
	}
	if status := currentStatus(); status.PID != first.PID {
		t.Fatal("invalid selection stopped the existing service")
	}
	named := start("--name", "shared")
	if named.Mode != expect.nativeMode || named.Model != "native-model" {
		t.Fatal("name-only selection did not use the first duplicate across protocols")
	}
	second := start("--protocol", "openai-compat")
	if second.Model != "chat-model-first" {
		t.Fatal("protocol-only selection did not use the first match")
	}
	paired := start("--protocol", "openai-compat", "--name", "shared")
	if paired.PID != second.PID || paired.Model != "chat-model-first" {
		t.Fatal("combined filters did not use the first pair")
	}
	settingsPath := filepath.Join(settingsDir, expect.settingsFile)
	before, _ := os.ReadFile(settingsPath)
	if bytes.Contains(before, []byte("chat-test-key")) {
		t.Fatal("upstream key leaked to proxied settings")
	}
	for _, filters := range [][]string{{"--name", "missing"}, {"--name", "bad"}, {"--protocol", ""}, {"--name", ""}} {
		if _, err := call("start", filters...); err == nil {
			t.Fatal("invalid or unreachable selection succeeded")
		}
		after, _ := os.ReadFile(settingsPath)
		if !bytes.Equal(before, after) {
			t.Fatal("failed selection or connection changed settings")
		}
		status := currentStatus()
		if !status.Running || status.PID != second.PID {
			t.Fatal("failed selection or connection stopped the service")
		}
	}
	if _, err := call("shutdown"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(settingsPath)
	if !bytes.Equal(before, after) {
		t.Fatal("shutdown changed settings")
	}

	args := append([]string{"mcp"}, common...)
	transport := &mcp.CommandTransport{Command: exec.CommandContext(ctx, executable, args...)}
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 3 {
		t.Fatal("stdio MCP discovery failed")
	}
	for _, tc := range []struct {
		args  map[string]any
		model string
	}{
		{map[string]any{}, "chat-model-default"},
		{map[string]any{"name": "shared"}, "native-model"},
		{map[string]any{"protocol": "openai-compat"}, "chat-model-first"},
		{map[string]any{"protocol": "openai-compat", "name": "shared"}, "chat-model-first"},
	} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "start_proxy", Arguments: tc.args})
		if err != nil || result.IsError {
			t.Fatalf("MCP start failed: %v %+v", err, result)
		}
		data, _ := json.Marshal(result.StructuredContent)
		var status daemon.Status
		if json.Unmarshal(data, &status) != nil || !status.Running || status.Model != tc.model {
			t.Fatalf("MCP selected wrong provider: %s", data)
		}
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if status := currentStatus(); !status.Running || status.Model != "chat-model-first" {
		t.Fatal("proxy did not survive MCP exit")
	}
	if _, err := call("shutdown"); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(settingsDir, expect.backupGlob))
	if err != nil || len(backups) != 6 {
		t.Fatalf("expected one backup per actual switch: %v", backups)
	}
}

func TestAgentFlagValidation(t *testing.T) {
	if err := run([]string{"status", "--agent", "gemini"}); err == nil {
		t.Fatal("unknown agent was accepted")
	}
}
