package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	httpapp "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/agent"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/daemon"
)

type recordingTransport struct {
	base         http.RoundTripper
	mu           sync.Mutex
	accepted     bool
	sessionID    string
	streamOpened chan struct{}
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.base.RoundTrip(req)
	if err == nil {
		t.mu.Lock()
		if res.StatusCode == http.StatusAccepted {
			t.accepted = true
		}
		if id := res.Header.Get("Mcp-Session-Id"); id != "" {
			t.sessionID = id
		}
		t.mu.Unlock()
		if req.Method == http.MethodGet && res.StatusCode == http.StatusOK && res.Header.Get("Content-Type") == "text/event-stream" {
			select {
			case t.streamOpened <- struct{}{}:
			default:
			}
		}
	}
	return res, err
}

func TestMCPHTTPTransports(t *testing.T) {
	for _, transport := range []string{"http", "sse"} {
		for _, name := range agent.Names() {
			t.Run(transport+"/"+name, func(t *testing.T) {
				options := daemon.Options{Agent: name, StateDir: filepath.Join(t.TempDir(), "state")}
				mcpServer := New(options)
				defer func() {
					for session := range mcpServer.Sessions() {
						_ = session.Close()
					}
				}()
				app, path, err := newHTTPApp(mcpServer, transport)
				if err != nil {
					t.Fatal(err)
				}
				app.Use(func(ctx *httpapp.Context, next func()) {
					ctx.Set("X-Middleware", "preserved")
					next()
					ctx.String("deferred body must not be appended")
				})
				var logs bytes.Buffer
				server := httptest.NewUnstartedServer(app)
				server.Config.ErrorLog = log.New(&logs, "", 0)
				server.Start()
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				client := server.Client()
				recorder := &recordingTransport{base: client.Transport}
				client.Transport = recorder
				var wire mcp.Transport = &mcp.StreamableClientTransport{
					Endpoint: server.URL + path, HTTPClient: client, DisableStandaloneSSE: true,
				}
				if transport == "sse" {
					wire = &mcp.SSEClientTransport{Endpoint: server.URL + path, HTTPClient: client}
				}
				cs, err := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "1"}, nil).Connect(ctx, wire, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
				if err != nil {
					t.Fatal(err)
				}
				defer cs.Close()
				recorder.mu.Lock()
				id := recorder.sessionID
				recorder.mu.Unlock()
				if transport == "http" {
					if id == "" {
						t.Fatal("SDK session header was lost")
					}
					// Establish a hanging GET while POST tool requests continue.
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Accept", "text/event-stream")
					req.Header.Set("Mcp-Session-Id", id)
					req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
					res, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer res.Body.Close()
					if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
						t.Fatalf("GET stream = %d %v", res.StatusCode, res.Header)
					}
				}
				listed, err := cs.ListTools(ctx, nil)
				if err != nil || len(listed.Tools) != 3 {
					t.Fatalf("tool discovery failed: %+v, %v", listed, err)
				}
				result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "proxy_status"})
				if err != nil || result.IsError {
					t.Fatalf("HTTP tool call failed: %+v, %v", result, err)
				}
				data, err := json.Marshal(result.StructuredContent)
				if err != nil {
					t.Fatal(err)
				}
				var status daemon.Status
				if err := json.Unmarshal(data, &status); err != nil || status.Agent != name || status.Running {
					t.Fatalf("wrong HTTP tool result: %s, %v", data, err)
				}
				req, err := http.NewRequestWithContext(ctx, http.MethodPut, server.URL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				res, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if err != nil || res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Allow") == "" || bytes.Contains(body, []byte("deferred")) {
					t.Fatalf("SDK error response = %d %q, %v", res.StatusCode, body, err)
				}
				if res.Header.Get("X-Middleware") != "preserved" {
					t.Fatal("middleware header was lost")
				}
				_ = cs.Close()
				cancel()
				server.Close()
				recorder.mu.Lock()
				accepted := recorder.accepted
				recorder.mu.Unlock()
				if !accepted {
					t.Fatal("expected SDK POST acknowledgements with status 202")
				}
				if logs.Len() != 0 {
					t.Fatalf("unexpected HTTP server log: %s", logs.String())
				}
			})
		}
	}
}

func TestMCPHTTPShutdownWithActiveStream(t *testing.T) {
	for _, transport := range []string{"http", "sse"} {
		t.Run(transport, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clientCtx, clientCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer clientCancel()
			mcpServer := New(daemon.Options{Agent: agent.Claude, StateDir: t.TempDir()})
			finished := make(chan error, 1)
			go func() { finished <- serveHTTP(ctx, listener, mcpServer, transport) }()
			stopped := false
			waitForShutdown := func() {
				stopped = true
				cancel()
				select {
				case err := <-finished:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(7 * time.Second):
					t.Error("MCP HTTP server did not shut down")
				}
				for range mcpServer.Sessions() {
					t.Error("MCP session survived shutdown")
				}
			}
			defer func() {
				if !stopped {
					waitForShutdown()
				}
			}()
			url := "http://" + listener.Addr().String()
			recorder := &recordingTransport{base: http.DefaultTransport, streamOpened: make(chan struct{}, 1)}
			client := &http.Client{Transport: recorder}
			var wire mcp.Transport = &mcp.StreamableClientTransport{Endpoint: url + "/mcp", HTTPClient: client, MaxRetries: -1}
			if transport == "sse" {
				wire = &mcp.SSEClientTransport{Endpoint: url + "/sse", HTTPClient: client}
			}
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "shutdown-test", Version: "1"}, nil).Connect(clientCtx, wire, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			if _, err := cs.ListTools(clientCtx, nil); err != nil {
				t.Fatal(err)
			}
			select {
			case <-recorder.streamOpened:
			case <-clientCtx.Done():
				t.Fatal("client did not establish its GET stream")
			}
			// Close the server while the client's GET stream remains active.
			waitForShutdown()
		})
	}
}

func TestMCPHTTPRequiresLoopback(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:0", "[::]:0", "localhost:0", "invalid"} {
		if err := RunHTTP(context.Background(), daemon.Options{}, "http", listen); err == nil {
			t.Errorf("accepted MCP listen address %q", listen)
		}
	}
}
