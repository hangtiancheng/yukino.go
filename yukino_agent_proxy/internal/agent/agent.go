// Package agent selects between the supported coding-agent proxies.
package agent

import (
	"context"
	"fmt"

	httpapp "github.com/hangtiancheng/yukino.go/yukino_http"

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/claude"
	claudeproxy "github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/claude/proxy"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/codex"
	codexproxy "github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/codex/proxy"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/upstream"
)

const (
	Claude = "claude"
	Codex  = "codex"
)

// Agent binds one coding agent to its settings, gateway, and route checks.
type Agent interface {
	Name() string
	SettingsLabel() string
	DefaultDir() (string, error)
	DefaultListen() string
	StateDirName() string
	Configure(dir string, p config.Provider, url string) (string, error)
	NewApp(p config.Provider) *httpapp.Application
	Check(ctx context.Context, client *upstream.Client) error
	Mode(p config.Provider) string
	BaseURL(p config.Provider, url string) string
}

// Names lists the supported agents in deterministic order.
func Names() []string { return []string{Claude, Codex} }

// Get resolves an agent by name; the empty name reports every supported agent.
func Get(name string) (Agent, error) {
	switch name {
	case Claude:
		return claudeAgent{}, nil
	case Codex:
		return codexAgent{}, nil
	case "":
		return nil, fmt.Errorf("--agent is required; expected %q or %q", Claude, Codex)
	default:
		return nil, fmt.Errorf("unknown agent %q; expected %q or %q", name, Claude, Codex)
	}
}

type claudeAgent struct{}

func (claudeAgent) Name() string          { return Claude }
func (claudeAgent) SettingsLabel() string { return "Claude Code" }
func (claudeAgent) DefaultDir() (string, error) {
	return claude.DefaultDir()
}
func (claudeAgent) DefaultListen() string { return "127.0.0.1:17861" }
func (claudeAgent) StateDirName() string  { return "claude-proxy" }
func (claudeAgent) Configure(dir string, p config.Provider, url string) (string, error) {
	return claude.Configure(dir, p, url)
}
func (claudeAgent) NewApp(p config.Provider) *httpapp.Application { return claudeproxy.New(p) }
func (claudeAgent) Check(ctx context.Context, client *upstream.Client) error {
	return claude.Check(ctx, client)
}
func (claudeAgent) Mode(p config.Provider) string {
	if p.Protocol == config.Anthropic {
		return "direct"
	}
	return "proxy"
}
func (claudeAgent) BaseURL(p config.Provider, url string) string {
	if p.Protocol == config.Anthropic {
		return p.BaseURL
	}
	return url
}

type codexAgent struct{}

func (codexAgent) Name() string          { return Codex }
func (codexAgent) SettingsLabel() string { return "Codex" }
func (codexAgent) DefaultDir() (string, error) {
	return codex.DefaultDir()
}
func (codexAgent) DefaultListen() string { return "127.0.0.1:17862" }
func (codexAgent) StateDirName() string  { return "codex-proxy" }
func (codexAgent) Configure(dir string, p config.Provider, url string) (string, error) {
	return codex.Configure(dir, p, url)
}
func (codexAgent) NewApp(p config.Provider) *httpapp.Application { return codexproxy.New(p) }
func (codexAgent) Check(ctx context.Context, client *upstream.Client) error {
	return codex.Check(ctx, client)
}
func (codexAgent) Mode(config.Provider) string { return "proxy" }
func (codexAgent) BaseURL(_ config.Provider, url string) string {
	return url + "/v1"
}
