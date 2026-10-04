// Package daemon manages the persistent proxy process shared by CLI and MCP.
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/claude"
	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/upstream"
)

const DefaultListen = "127.0.0.1:17861"

type Options struct {
	Protocol, Name                                      string
	ConfigPath, ClaudeDir, StateDir, Listen, Executable string
	ExpectedProvider                                    string
}

func Defaults() (Options, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return Options{}, err
	}
	dir, err := claude.DefaultDir()
	if err != nil {
		return Options{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return Options{}, err
	}
	return Options{ConfigPath: path, ClaudeDir: dir, StateDir: filepath.Join(filepath.Dir(path), "claude-proxy"), Listen: DefaultListen, Executable: executable}, nil
}

type Status struct {
	Running       bool      `json:"running"`
	PID           int       `json:"pid,omitempty"`
	Protocol      string    `json:"protocol,omitempty"`
	Name          string    `json:"name,omitempty"`
	Model         string    `json:"model,omitempty"`
	Mode          string    `json:"mode,omitempty"`
	GatewayURL    string    `json:"gateway_url,omitempty"`
	ClaudeBaseURL string    `json:"claude_base_url,omitempty"`
	BackupPath    string    `json:"backup_path,omitempty"`
	StartedAt     time.Time `json:"started_at,omitempty"`
}

type state struct {
	Status
	Token       string `json:"control_token"`
	Fingerprint string `json:"provider_fingerprint"`
	Listen      string `json:"requested_listen"`
	ClaudeDir   string `json:"claude_dir"`
}

type Manager struct{ Options Options }

func (m *Manager) statePath() string { return filepath.Join(m.Options.StateDir, "state.json") }
func (m *Manager) readState() (*state, error) {
	data, err := os.ReadFile(m.statePath())
	if err != nil {
		return nil, err
	}
	var value state
	if json.Unmarshal(data, &value) != nil || value.Token == "" || value.GatewayURL == "" {
		return nil, fmt.Errorf("invalid proxy state file: %s", m.statePath())
	}
	return &value, nil
}
func (m *Manager) writeState(value *state) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return claude.WritePrivate(m.statePath(), append(data, '\n'))
}

func fingerprint(p config.Provider) string {
	// APIKey is excluded from Provider's public JSON representation.
	data, _ := json.Marshal(struct {
		Provider config.Provider
		Key      string
	}{p, p.APIKey})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (m *Manager) selected() (config.Provider, error) {
	cfg, err := config.Load(m.Options.ConfigPath)
	if err != nil {
		return config.Provider{}, err
	}
	return cfg.Select(m.Options.Protocol, m.Options.Name)
}

func control(ctx context.Context, s *state, method, path string) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.GatewayURL+path, nil)
	if err != nil {
		return Status{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return Status{}, fmt.Errorf("proxy control endpoint is unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Status{}, fmt.Errorf("proxy control endpoint returned HTTP %d", response.StatusCode)
	}
	var status Status
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&status); err != nil {
		return Status{}, fmt.Errorf("invalid proxy control response")
	}
	return status, nil
}

func (m *Manager) Status(ctx context.Context) (Status, error) {
	s, err := m.readState()
	if os.IsNotExist(err) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	status, err := control(ctx, s, http.MethodGet, "/_yukino/status")
	if err != nil && !processAlive(s.PID) {
		s.Running = false
		return s.Status, nil
	}
	return status, err
}

func (m *Manager) Start(ctx context.Context) (Status, error) {
	p, err := m.selected()
	if err != nil {
		return Status{}, err
	}
	if err := upstream.New(p).Check(ctx); err != nil {
		return Status{}, err
	}
	unlock, err := m.lock(ctx)
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	if current, err := m.readState(); err == nil {
		if status, err := control(ctx, current, http.MethodGet, "/_yukino/status"); err == nil {
			if current.Fingerprint == fingerprint(p) && current.Listen == m.Options.Listen && current.ClaudeDir == m.Options.ClaudeDir {
				return status, nil
			}
			if err := m.shutdown(ctx); err != nil {
				return Status{}, err
			}
		} else if processAlive(current.PID) {
			return Status{}, fmt.Errorf("an existing proxy process is unreachable; inspect %s before restarting", m.Options.StateDir)
		} else if err := os.Remove(m.statePath()); err != nil {
			return Status{}, err
		}
	} else if !os.IsNotExist(err) {
		return Status{}, err
	}
	logPath := filepath.Join(m.Options.StateDir, "proxy.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return Status{}, err
	}
	defer logFile.Close()
	if err := logFile.Chmod(0600); err != nil {
		return Status{}, err
	}
	args := []string{"_serve", "--config", m.Options.ConfigPath, "--claude-dir", m.Options.ClaudeDir, "--state-dir", m.Options.StateDir, "--listen", m.Options.Listen, "--expected-provider", fingerprint(p)}
	if m.Options.Protocol != "" {
		args = append(args, "--protocol", m.Options.Protocol)
	}
	if m.Options.Name != "" {
		args = append(args, "--name", m.Options.Name)
	}
	cmd := exec.Command(m.Options.Executable, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return Status{}, fmt.Errorf("start proxy process: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	ready := false
	defer func() {
		if !ready {
			interrupt(cmd.Process)
		}
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if s, err := m.readState(); err == nil && s.PID == cmd.Process.Pid {
			if status, err := control(ctx, s, http.MethodGet, "/_yukino/status"); err == nil {
				ready = true
				return status, nil
			}
		}
		select {
		case <-ctx.Done():
			return Status{}, ctx.Err()
		case <-deadline.C:
			return Status{}, fmt.Errorf("proxy did not become ready; inspect %s", logPath)
		case <-exited:
			return Status{}, fmt.Errorf("proxy process exited during startup; inspect %s", logPath)
		case <-tick.C:
		}
	}
}

func (m *Manager) Shutdown(ctx context.Context) error {
	unlock, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return m.shutdown(ctx)
}
func (m *Manager) shutdown(ctx context.Context) error {
	s, err := m.readState()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !processAlive(s.PID) {
		return os.Remove(m.statePath())
	}
	if _, err := control(ctx, s, http.MethodPost, "/_yukino/shutdown"); err != nil {
		return err
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(m.statePath()); os.IsNotExist(err) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("proxy shutdown timed out")
		case <-tick.C:
		}
	}
}
