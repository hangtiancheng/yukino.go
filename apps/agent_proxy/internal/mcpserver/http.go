package mcpserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	httpapp "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/daemon"
)

func newHTTPApp(server *mcp.Server, transport string) (*httpapp.Application, string, error) {
	getServer := func(*http.Request) *mcp.Server { return server }
	var handler http.Handler
	var path string
	switch transport {
	case "http":
		path = "/mcp"
		handler = mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
			SessionTimeout: 5 * time.Minute,
		})
	case "sse":
		path = "/sse"
		handler = mcp.NewSSEHandler(getServer, nil)
	default:
		return nil, "", fmt.Errorf("unsupported MCP HTTP transport %q; expected http or sse", transport)
	}
	handler = http.NewCrossOriginProtection().Handler(handler)
	app := httpapp.New()
	app.Use(httpapp.Recovery())
	app.All(path, func(ctx *httpapp.Context, _ func()) {
		ctx.TakeOver()
		handler.ServeHTTP(ctx.Writer, ctx.Request)
	})
	return app, path, nil
}

// RunHTTP serves MCP in the foreground over Streamable HTTP or legacy SSE.
// The MCP listener is separate from the coding agents' proxy listeners.
func RunHTTP(ctx context.Context, options daemon.Options, transport, listen string) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("MCP listen must be a loopback IP address and port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("MCP listen must use a loopback IP address")
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen for MCP requests: %w", err)
	}
	return serveHTTP(ctx, listener, New(options), transport)
}

func serveHTTP(ctx context.Context, listener net.Listener, mcpServer *mcp.Server, transport string) error {
	defer listener.Close()
	defer func() {
		for session := range mcpServer.Sessions() {
			_ = session.Close()
		}
	}()
	app, path, err := newHTTPApp(mcpServer, transport)
	if err != nil {
		return err
	}
	baseCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{
		Handler:           app,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}
	log.Printf("MCP %s listening on http://%s%s", transport, listener.Addr(), path)
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case err := <-finished:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("drain MCP requests: %w", err)
	}
	return nil
}
