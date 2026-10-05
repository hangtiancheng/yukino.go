package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/agent"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/daemon"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/mcpserver"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "yukino-agent-proxy:", err)
		os.Exit(1)
	}
}

func parseOptions(args []string) (string, daemon.Options, bool, error) {
	selected, _ := agent.Get(agent.Claude)
	opts, err := daemon.Defaults(selected)
	if err != nil {
		return "", opts, false, err
	}
	command := "start"
	hasCommand := len(args) > 0 && args[0] != "" && args[0][0] != '-'
	if hasCommand {
		command = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("yukino-agent-proxy", flag.ContinueOnError)
	flags.StringVar(&opts.Agent, "agent", opts.Agent, "Coding agent to proxy: claude or codex")
	flags.StringVar(&opts.Protocol, "protocol", "", "Optional protocol filter: anthropic, openai, or openai-compat")
	flags.StringVar(&opts.Name, "name", "", "Optional provider name filter; names may repeat")
	flags.StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "Yukino provider configuration path")
	flags.StringVar(&opts.AgentDir, "agent-dir", opts.AgentDir, "Agent configuration directory (Claude Code or Codex)")
	flags.StringVar(&opts.StateDir, "state-dir", opts.StateDir, "Proxy process state directory")
	flags.StringVar(&opts.Listen, "listen", opts.Listen, "Local loopback listen address")
	foreground := flags.Bool("foreground", false, "Run the proxy in the foreground")
	if command == "_serve" {
		flags.StringVar(&opts.ExpectedProvider, "expected-provider", "", "Validated provider fingerprint")
	}
	flags.Usage = func() {
		if a, err := agent.Get(opts.Agent); err == nil {
			if defaults, err := daemon.Defaults(a); err == nil {
				flags.Lookup("agent-dir").DefValue = defaults.AgentDir
				flags.Lookup("state-dir").DefValue = defaults.StateDir
				flags.Lookup("listen").DefValue = defaults.Listen
			}
		}
		fmt.Fprintln(flags.Output(), "Usage: yukino-agent-proxy [start|shutdown|status|mcp] [--agent=claude|codex] [options]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return "", opts, false, err
	}
	// Also accept flags before the command, as in --agent=codex status.
	if !hasCommand && flags.NArg() > 0 {
		command = flags.Arg(0)
		if err := flags.Parse(flags.Args()[1:]); err != nil {
			return "", opts, false, err
		}
	}
	if flags.NArg() != 0 {
		return "", opts, false, fmt.Errorf("unexpected positional argument %q", flags.Arg(0))
	}
	var emptyFilter string
	explicit := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
		if (f.Name == "protocol" || f.Name == "name") && f.Value.String() == "" {
			emptyFilter = f.Name
		}
	})
	if emptyFilter != "" {
		return "", opts, false, fmt.Errorf("--%s must not be empty; omit both filters to use default_provider", emptyFilter)
	}
	selected, err = agent.Get(opts.Agent)
	if err != nil {
		return "", opts, false, err
	}
	defaults, err := daemon.Defaults(selected)
	if err != nil {
		return "", opts, false, err
	}
	// Resolve defaults from the final parsed agent, preserving explicit overrides.
	if !explicit["agent-dir"] {
		opts.AgentDir = defaults.AgentDir
	}
	if !explicit["listen"] {
		opts.Listen = defaults.Listen
	}
	if !explicit["state-dir"] {
		opts.StateDir = defaults.StateDir
	}
	for _, value := range []*string{&opts.ConfigPath, &opts.AgentDir, &opts.StateDir} {
		if *value == "" {
			return "", opts, false, fmt.Errorf("configuration and state paths must not be empty")
		}
		path, err := filepath.Abs(*value)
		if err != nil {
			return "", opts, false, err
		}
		*value = path
	}
	return command, opts, *foreground, nil
}

func run(args []string) error {
	command, opts, foreground, err := parseOptions(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	selected, _ := agent.Get(opts.Agent)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	manager := daemon.Manager{Options: opts}
	switch command {
	case "start":
		if foreground {
			return manager.Serve(ctx, true)
		}
		status, err := manager.Start(ctx)
		if err != nil {
			return err
		}
		return printJSON(status)
	case "_serve":
		return manager.Serve(ctx, false)
	case "shutdown":
		if err := manager.Shutdown(ctx); err != nil {
			return err
		}
		fmt.Printf("Proxy stopped. %s settings and backups were left unchanged.\n", selected.SettingsLabel())
		return nil
	case "status":
		status, err := manager.Status(ctx)
		if err != nil {
			return err
		}
		return printJSON(status)
	case "mcp":
		return mcpserver.New(opts).Run(ctx, &mcp.StdioTransport{})
	default:
		return fmt.Errorf("unknown command %q; expected start, shutdown, status, or mcp", command)
	}
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
