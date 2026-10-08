package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	httpapp "github.com/hangtiancheng/yukino.go/libs/yukino_http"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/upstream"
)

func newControlToken() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "control_" + hex.EncodeToString(b[:])
}

func (m *Manager) Serve(ctx context.Context, lockStartup bool) error {
	a, err := m.agent()
	if err != nil {
		return err
	}
	p, err := m.selected()
	if err != nil {
		return err
	}
	var unlock func()
	if lockStartup {
		var err error
		unlock, err = m.lock(ctx)
		if err != nil {
			return err
		}
		defer func() {
			if unlock != nil {
				unlock()
			}
		}()
		if current, err := m.readState(); err == nil && processAlive(current.PID) {
			return fmt.Errorf("proxy is already running")
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if m.Options.ExpectedProvider != "" {
		if m.Options.ExpectedProvider != fingerprint(p) {
			return fmt.Errorf("selected provider changed after its connection was checked; start again")
		}
	} else if err := a.Check(ctx, upstream.New(p)); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(m.Options.Listen)
	if err != nil {
		return fmt.Errorf("listen must be a loopback IP address and port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen must use a loopback IP address")
	}
	listener, err := net.Listen("tcp", m.Options.Listen)
	if err != nil {
		return fmt.Errorf("listen for proxy requests: %w", err)
	}
	defer listener.Close()
	url := "http://" + listener.Addr().String()
	app := a.NewApp(p)
	stop := make(chan struct{}, 1)
	s := &state{Status: Status{Running: true, PID: os.Getpid(), Agent: a.Name(), Protocol: p.Protocol, Name: p.Name, Model: p.Model, Mode: a.Mode(p), GatewayURL: url, BaseURL: a.BaseURL(p, url), StartedAt: time.Now().UTC()}, Token: newControlToken(), Fingerprint: fingerprint(p), Listen: m.Options.Listen}
	s.AgentDir = m.Options.AgentDir
	app.Use(func(c *httpapp.Context, next func()) {
		if c.Path == "/_yukino/status" || c.Path == "/_yukino/shutdown" {
			auth := c.Request.Header.Get("Authorization")
			if subtle.ConstantTimeCompare([]byte(auth), []byte("Bearer "+s.Token)) != 1 {
				c.SetStatus(401)
				c.JSON(map[string]any{"error": "Unauthorized control request."})
				return
			}
		}
		next()
	})
	app.Get("/_yukino/status", func(c *httpapp.Context, _ func()) { c.JSON(s.Status) })
	app.Post("/_yukino/shutdown", func(c *httpapp.Context, _ func()) {
		c.JSON(s.Status)
		select {
		case stop <- struct{}{}:
		default:
		}
	})
	backup, err := a.Configure(m.Options.AgentDir, p, url)
	if err != nil {
		return err
	}
	s.BackupPath = backup
	if err := os.MkdirAll(m.Options.StateDir, 0700); err != nil {
		return err
	}
	if err := m.writeState(s); err != nil {
		return err
	}
	defer func() {
		if current, err := m.readState(); err == nil && current.Token == s.Token {
			_ = os.Remove(m.statePath())
		}
	}()
	if unlock != nil {
		unlock()
		unlock = nil
	}
	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &http.Server{Handler: app, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, BaseContext: func(net.Listener) context.Context { return baseCtx }}
	errors := make(chan error, 1)
	go func() { errors <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case <-stop:
	case err := <-errors:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
	cancel() // Interrupt in-flight upstream streams before draining HTTP handlers.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("drain proxy requests: %w", err)
	}
	return nil
}
