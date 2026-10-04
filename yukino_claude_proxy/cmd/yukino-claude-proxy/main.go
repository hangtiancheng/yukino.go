package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/daemon"
	"github.com/hangtiancheng/yukino.go/yukino_claude_proxy/internal/mcpserver"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "yukino-claude-proxy:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := daemon.Defaults()
	if err != nil {
		return err
	}
	command := "start"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("yukino-claude-proxy", flag.ContinueOnError)
	flags.StringVar(&opts.Protocol, "protocol", "", "Optional protocol filter: anthropic, openai, or openai-compat")
	flags.StringVar(&opts.Name, "name", "", "Optional provider name filter; names may repeat")
	flags.StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "Yukino provider configuration path")
	flags.StringVar(&opts.ClaudeDir, "claude-dir", opts.ClaudeDir, "Claude Code configuration directory")
	flags.StringVar(&opts.StateDir, "state-dir", opts.StateDir, "Proxy process state directory")
	flags.StringVar(&opts.Listen, "listen", opts.Listen, "Local loopback listen address")
	foreground := flags.Bool("foreground", false, "Run the proxy in the foreground")
	if command == "_serve" {
		flags.StringVar(&opts.ExpectedProvider, "expected-provider", "", "Validated provider fingerprint")
	}
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: yukino-claude-proxy [start|shutdown|status|mcp] [options]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument %q", flags.Arg(0))
	}
	var emptyFilter string
	flags.Visit(func(f *flag.Flag) {
		if (f.Name == "protocol" || f.Name == "name") && f.Value.String() == "" {
			emptyFilter = f.Name
		}
	})
	if emptyFilter != "" {
		return fmt.Errorf("--%s must not be empty; omit both filters to use default_provider", emptyFilter)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	manager := daemon.Manager{Options: opts}
	switch command {
	case "start":
		if *foreground {
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
		fmt.Println("Proxy stopped. Claude Code settings and backups were left unchanged.")
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
