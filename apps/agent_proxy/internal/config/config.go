package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	Anthropic    = "anthropic"
	OpenAI       = "openai"
	OpenAICompat = "openai-compat"
)

type Provider struct {
	Name            string `yaml:"name" json:"name"`
	Protocol        string `yaml:"protocol" json:"protocol"`
	BaseURL         string `yaml:"base_url" json:"base_url"`
	Model           string `yaml:"model" json:"model"`
	APIKey          string `yaml:"api_key" json:"-"`
	Thinking        string `yaml:"thinking" json:"thinking,omitempty"`
	ContextWindow   int64  `yaml:"context_window" json:"context_window,omitempty"`
	MaxOutputTokens int64  `yaml:"max_output_tokens" json:"max_output_tokens,omitempty"`
}

type Config struct {
	Providers       []Provider `yaml:"providers"`
	DefaultProvider int        `yaml:"default_provider"`
}

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "default_provider" && node.Content[i+1].Tag != "!!int" {
			return fmt.Errorf("default_provider must be an integer")
		}
	}
	type plainConfig Config
	return node.Decode((*plainConfig)(c))
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".yukino", "config.yaml"), nil
}

func ValidProtocol(protocol string) bool {
	return protocol == Anthropic || protocol == OpenAI || protocol == OpenAICompat
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read provider configuration: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid YAML in provider configuration %s", path)
	}
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("provider configuration has no providers")
	}
	return &cfg, nil
}

func (c *Config) Select(protocol, name string) (Provider, error) {
	if protocol != "" && !ValidProtocol(protocol) {
		return Provider{}, fmt.Errorf("invalid protocol %q; expected anthropic, openai, or openai-compat", protocol)
	}
	if len(c.Providers) == 0 {
		return Provider{}, fmt.Errorf("provider configuration has no providers")
	}
	if protocol == "" && name == "" {
		index := c.DefaultProvider
		if index < 0 || index >= len(c.Providers) {
			return Provider{}, fmt.Errorf("default_provider index %d is out of range for %d providers", index, len(c.Providers))
		}
		return c.Providers[index].Validate()
	}
	for _, p := range c.Providers {
		if (protocol != "" && p.Protocol != protocol) || (name != "" && p.Name != name) {
			continue
		}
		return p.Validate()
	}
	if protocol != "" && name != "" {
		return Provider{}, fmt.Errorf("no provider matches protocol %q and name %q", protocol, name)
	}
	if name != "" {
		return Provider{}, fmt.Errorf("no provider matches name %q", name)
	}
	return Provider{}, fmt.Errorf("no provider matches protocol %q", protocol)
}

func (p Provider) Validate() (Provider, error) {
	if !ValidProtocol(p.Protocol) {
		return Provider{}, fmt.Errorf("selected provider has invalid protocol %q; expected anthropic, openai, or openai-compat", p.Protocol)
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Model) == "" {
		return Provider{}, fmt.Errorf("selected provider requires a name and model")
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Provider{}, fmt.Errorf("selected provider requires an HTTP(S) base_url without credentials, query, or fragment")
	}
	if p.MaxOutputTokens < 0 || p.ContextWindow < 0 {
		return Provider{}, fmt.Errorf("selected provider token limits cannot be negative")
	}
	p.Thinking = strings.ToLower(p.Thinking)
	switch p.Thinking {
	case "enabled", "adaptive":
		p.Thinking = "high"
	case "", "off", "none", "minimal", "low", "medium", "high", "xhigh", "max", "true", "false":
	default:
		return Provider{}, fmt.Errorf("selected provider has an invalid thinking level")
	}
	p.APIKey = os.ExpandEnv(p.APIKey)
	if p.APIKey == "" {
		key := "OPENAI_API_KEY"
		if p.Protocol == Anthropic {
			key = "ANTHROPIC_API_KEY"
		}
		p.APIKey = os.Getenv(key)
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return Provider{}, fmt.Errorf("selected provider has no API key; set api_key or the protocol's API key environment variable")
	}
	return p, nil
}
