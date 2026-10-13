package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPTransportFlags(t *testing.T) {
	for _, tc := range []struct {
		args      []string
		transport string
		listen    string
	}{
		{[]string{"mcp"}, "stdio", "127.0.0.1:17863"},
		{[]string{"mcp", "--transport=stdio"}, "stdio", "127.0.0.1:17863"},
		{[]string{"--transport=http", "mcp", "--mcp-listen=127.0.0.1:0"}, "http", "127.0.0.1:0"},
		{[]string{"mcp", "--transport=sse"}, "sse", "127.0.0.1:17863"},
	} {
		command, _, options, err := parseOptions(tc.args)
		if err != nil || command != "mcp" || options.transport != tc.transport || options.mcpListen != tc.listen {
			t.Fatalf("parse %v = %s %+v, %v", tc.args, command, options, err)
		}
	}
	for _, args := range [][]string{
		{"mcp", "--transport=invalid"},
		{"mcp", "--mcp-listen=127.0.0.1:0"},
		{"start", "--transport=http"},
		{"status", "--mcp-listen=127.0.0.1:0"},
	} {
		if _, _, _, err := parseOptions(args); err == nil {
			t.Errorf("accepted invalid MCP flags %v", args)
		}
	}
}

func startMCPHTTPCLI(t *testing.T, ctx context.Context, transport string, extraArgs ...string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	processCtx, stop := context.WithCancel(ctx)
	args := append([]string{"mcp", "--transport=" + transport, "--mcp-listen=127.0.0.1:0"}, extraArgs...)
	cmd := exec.CommandContext(processCtx, executable, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stop()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		stop()
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	logsDone := make(chan struct{})
	var logs bytes.Buffer
	go func() {
		defer close(logsDone)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logs.WriteString(line + "\n")
			if _, endpoint, ok := strings.Cut(line, "listening on "); ok {
				ready <- endpoint
			}
		}
	}()
	t.Cleanup(func() {
		stop()
		_ = cmd.Wait()
		<-logsDone
		if stdout.Len() != 0 || strings.Contains(logs.String(), "superfluous response.WriteHeader") {
			t.Errorf("unexpected MCP HTTP output: stdout=%s stderr=%s", stdout.String(), logs.String())
		}
	})
	select {
	case endpoint := <-ready:
		return endpoint
	case <-logsDone:
		t.Fatalf("MCP HTTP command exited before startup: %s", logs.String())
	case <-ctx.Done():
		t.Fatal("MCP HTTP command did not start")
	}
	return ""
}

func TestMCPHTTPCLI(t *testing.T) {
	for _, transport := range []string{"http", "sse"} {
		t.Run(transport, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			endpoint := startMCPHTTPCLI(t, ctx, transport, "--agent=codex", "--state-dir", t.TempDir())
			var wire mcp.Transport = &mcp.StreamableClientTransport{Endpoint: endpoint}
			if transport == "sse" {
				wire = &mcp.SSEClientTransport{Endpoint: endpoint}
			}
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "cli-test", Version: "1"}, nil).Connect(ctx, wire, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			listed, err := cs.ListTools(ctx, nil)
			if err != nil || len(listed.Tools) != 3 {
				t.Fatalf("CLI HTTP discovery failed: %+v, %v", listed, err)
			}
			result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "proxy_status"})
			if err != nil || result.IsError {
				t.Fatalf("CLI HTTP status failed: %+v, %v", result, err)
			}
		})
	}
}
