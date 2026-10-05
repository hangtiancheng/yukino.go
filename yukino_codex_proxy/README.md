# yukino-codex-proxy

Run Codex against ordered providers from `~/.yukino/config.yaml`. The CLI validates the selected provider, starts a persistent local Responses gateway, backs up Codex configuration, and points Codex to that gateway. CLI and MCP tools manage the same background process.

| Protocol | Upstream API | Gateway behavior |
| --- | --- | --- |
| `openai` | OpenAI Responses | Forward Responses requests and events |
| `openai-compat` | OpenAI Chat Completions | Convert Responses to Chat Completions and back |
| `anthropic` | Anthropic Messages | Convert Responses to Messages and back |

## Build

Requires Go 1.26.4 or newer and the sibling `../yukino_http` module. The module-local `go.work` includes both modules; a local `replace` also permits builds with `GOWORK=off`. No parent workspace changes are required.

```sh
make build
bin/yukino-codex-proxy --help

# Optional: install into GOBIN, or GOPATH/bin when GOBIN is unset.
make install
```

Build or install from the local checkout. A version-qualified remote `go install ...@latest` cannot resolve the sibling replacement. macOS, Linux and Windows process management are supported.

To build all six platform artifacts, use Node.js 20 or newer:

```sh
node build.mjs
# Equivalent Make target.
make build-all
# Optional: limit concurrent builds (default: up to three).
BUILD_CONCURRENCY=2 node build.mjs
```

The script works from any working directory and writes into this module's `bin` directory:

```text
bin/yukino-codex-proxy-linux-x64
bin/yukino-codex-proxy-linux-arm64
bin/yukino-codex-proxy-darwin-x64
bin/yukino-codex-proxy-darwin-arm64
bin/yukino-codex-proxy-win32-x64
bin/yukino-codex-proxy-win32-arm64
bin/yukino-codex-proxy -> artifact for the current OS and architecture
```

`build.mjs` uses `@ts-check` and JSDoc types, disables CGO, strips debug symbols, and limits each Go build to two compiler processes by default. It uses `GOWORK=off` unless explicitly overridden, relying on the sibling module replacement. Failed compiles retain the previous artifact, and the native link is updated only after all six builds succeed. On Windows, a hard link is used if symlinks require additional privileges. Windows artifacts are PE executables without a filename extension; copy or rename the selected artifact to `yukino-codex-proxy.exe` before running it on Windows.

## Release

To publish this proxy, authenticate with `gh auth login`, commit and push the source, then run from this module:

```sh
make release
```

The repository-root [release.js](../release.js) owns publishing for both proxies. It builds every selected project before changing GitHub. Release tags and titles are fixed at the binary names, and all six asset names have no version or timestamp suffix. Existing releases replace same-name assets, update the remote tag to the current HEAD, and refresh their metadata. New releases use the same fixed names. Native executable links are excluded. Push HEAD before publishing.

From the repository root:

```sh
# Build and publish both proxies.
node release.js
# Publish just one proxy.
node release.js yukino-claude-proxy
node release.js yukino-codex-proxy
# Build and inspect GitHub, then print planned writes without applying them.
node release.js --dry-run
# Test publishing behavior without accessing GitHub.
node --test release.test.js
```

The root package also provides `pnpm release` and `pnpm test:release`. The script uses `@ts-check` and JSDoc types, works from any current directory, and requires Node.js 20 or newer, Go, and an authenticated GitHub CLI. GitHub lookup errors abort instead of being treated as missing releases. Remote publishing uses multiple API calls; an interrupted publish can be rerun to replace the same fixed assets.

## CLI and provider selection

```sh
# providers[default_provider]; an omitted default_provider uses index 0.
yukino-codex-proxy

# First matching protocol.
yukino-codex-proxy --protocol=openai
yukino-codex-proxy --protocol=openai-compat
yukino-codex-proxy --protocol=anthropic

# First matching name across protocols.
yukino-codex-proxy --name=ds-openai

# First matching protocol/name pair.
yukino-codex-proxy --protocol=openai-compat --name=ds-openai

yukino-codex-proxy status
yukino-codex-proxy shutdown
```

| Filters supplied | Selected provider |
| --- | --- |
| Protocol only | First provider with that protocol |
| Name only | First provider with that name, across protocols |
| Both | First provider matching both |
| Neither | `providers[default_provider]`, default index `0` |

Names may repeat. Array order determines the first match. `default_provider` must be an unquoted integer; its range is checked when default selection is used. Empty provider lists, missing matches, invalid selected providers, and failed connections cause a nonzero exit. An invalid first match is never skipped.

Every start sends a small inference request to check the selected endpoint, model and credentials before stopping an existing service or changing Codex files. The check has a 128-token output budget and a 20-second deadline, and requires a valid terminal response or stream. It can incur an API charge. Starting the same provider again returns the current status without another backup. Changing providers replaces the background service. Restart Codex after a switch to load its updated configuration.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--config` | `$HOME/.yukino/config.yaml` | Provider configuration |
| `--codex-dir` | `$CODEX_HOME`, otherwise `$HOME/.codex` | Codex configuration directory |
| `--state-dir` | `codex-proxy` beside the selected config file | Process state, lock and log directory |
| `--listen` | `127.0.0.1:17862` | Loopback IP and port; port `0` selects a free port |
| `--foreground` | `false` | Run until Ctrl-C instead of detaching |

Put flags after a subcommand. Use the same `--state-dir` when controlling a service. `start` is an optional explicit subcommand.

If your shell routes HTTP through a network proxy, include `127.0.0.1,localhost,::1` in `NO_PROXY` before launching Codex so it can reach the local gateway.

## Provider configuration

See [config.example.yaml](config.example.yaml). Other Yukino YAML sections are ignored. Only the selected provider is validated, so unused providers may use other protocols.

`name`, `protocol`, `base_url`, and `model` are required. Base URLs must use HTTP(S) without embedded credentials, query parameters or fragments. `api_key` supports environment expansion; an omitted key falls back to `ANTHROPIC_API_KEY` for Messages, or `OPENAI_API_KEY` for either OpenAI protocol.

Host-only OpenAI URLs receive `/v1`; existing paths such as `/compatible-mode/v1` are retained. Full `/responses` and `/chat/completions` endpoints are accepted. Anthropic URLs may end at the provider root, `/v1`, or the full `/v1/messages` endpoint.

`max_output_tokens` caps upstream requests. `context_window` sets the Codex context window and model catalog entry; when omitted, the catalog uses 128,000 tokens. Set these values to the endpoint's actual limits. Neither value may be negative.

`thinking` accepts `none`, `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, or YAML booleans. Request reasoning settings take precedence. Converted requests default to high effort when neither specifies it. Messages uses adaptive thinking for recognized recent Claude models and budgeted thinking otherwise. On models that require thinking, `none` maps to low adaptive effort and forced tool selection returns a clear request error; other models disable thinking for forced selection. These mappings follow the official [thinking configuration rules](https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting) and [effort rules](https://platform.claude.com/docs/en/build-with-claude/effort). DeepSeek Chat requests use its `thinking` extension. Other Chat endpoints receive `reasoning_effort` when enabled and must support that field.

## Codex configuration and manual recovery

Before each actual start or switch, the original `config.toml` bytes are saved in the Codex directory:

```text
config.toml.yukino-codex-proxy.<UTC timestamp>.bak
```

The CLI writes a `yukino-codex-proxy` model provider with `wire_api = "responses"`, the local base URL and a placeholder bearer token. It also writes `yukino-codex-proxy-models.json` so Codex can use custom model IDs and client-side shell tools. Upstream keys stay in the Yukino configuration and proxy process; `auth.json` is untouched.

Unrelated TOML settings, projects and MCP servers are preserved. Routing/model overrides in legacy inline profile tables are cleared so those profiles inherit the proxy. Overrides of reserved built-in provider IDs are removed. Re-encoding TOML changes formatting and removes comments; the backup retains the original bytes. Separately layered profile files and command-line overrides can still supersede these settings.

**Shutdown never restores or edits Codex configuration.** Codex continues pointing at the stopped gateway until it is restarted or a backup is restored. Status includes the backup path. If the original configuration did not exist, its backup is an empty file.

```sh
yukino-codex-proxy shutdown
# Choose the backup you want to restore.
cp ~/.codex/config.toml.yukino-codex-proxy.<timestamp>.bak ~/.codex/config.toml
```

Configuration, catalog, backups, state and logs use mode `0600` on POSIX. Windows access follows directory ACLs.

## MCP

```sh
yukino-codex-proxy mcp
```

The official Go MCP SDK exposes these stdio tools:

| Tool | Arguments | Behavior |
| --- | --- | --- |
| `start_proxy` | Optional `protocol` and `name` | Apply the same four selection rules, validate and start/switch |
| `proxy_status` | None | Return service and provider metadata without credentials |
| `shutdown_proxy` | None | Stop service and retain Codex settings and backups |

Omitting both `start_proxy` arguments selects `default_provider`, even if the MCP command has CLI filters. Empty strings are rejected. The proxy survives MCP disconnection.

For Codex, register the executable with its absolute path:

```toml
[mcp_servers.yukino_codex_proxy]
command = "/absolute/path/to/yukino-codex-proxy"
args = ["mcp"]
```

Generic MCP client configuration:

```json
{
  "mcpServers": {
    "yukino-codex-proxy": {
      "command": "/absolute/path/to/yukino-codex-proxy",
      "args": ["mcp"]
    }
  }
}
```

## Protocol coverage

The bridge handles text, images, data-URL documents, function calls/results, parallel calls, namespaced tools, custom text tools wrapped as functions, and client-side tool search. Anthropic signed and redacted thinking blocks survive Responses history replay. Chat reasoning is retained in assistant history.

Converted providers use a bounded process-local history cache for `previous_response_id` and for recovering tool calls omitted from result-only inputs. History is lost on restart; resend the full input history when a continuation ID is unavailable. The cache retains at most 128 responses and 64 MiB.

Native Responses `/responses/compact` is forwarded. For converted protocols, that route and Codex compaction triggers request a handoff summary and encode it in a replayable compaction item. These envelopes belong to this proxy and are not OpenAI encrypted state.

The gateway accepts `/responses`, `/v1/responses`, their `/compact` routes, `/models`, `/v1/models`, and `/health`. It supports JSON and SSE, including upstream JSON fallback for streaming requests and SSE fallback for nonstreaming requests. It accepts gzip and zstd compressed request bodies. Client cancellation cancels the upstream call; failed or truncated streams produce a failure instead of successful empty output.

Current limits:

- HTTP/SSE transport only; WebSocket requests return HTTP 426.
- Hosted web search is disabled in Codex configuration and omitted for converted protocols. Other hosted tools are unsupported by those protocols.
- Uploaded OpenAI file IDs and encrypted state from another provider cannot be translated into Anthropic documents or thinking signatures.
- Tool-result media in Chat Completions is serialized as text; image support depends on the selected endpoint.
- Gemini, OAuth routing, automatic failover and automatic retries are outside this tool's scope.

The service binds only to loopback. Administrative routes require a private control token. Request and buffered response bodies are limited to 32 MiB, and inference requests to ten minutes.

## Development

```sh
make test
make race
make vet

# Small billable requests; does not modify your Codex configuration.
go test ./internal/proxy -run '^TestLiveProviders$' -v -count=1 -args -live

# Installed Codex CLI, real providers, temporary Codex directories,
# and a read-only sample file.
go test ./cmd/yukino-codex-proxy -run '^TestLiveCodex$' -v -count=1 -args -live-codex
```

Tests cover selection and failure behavior, exact backups, CLI process switching, MCP stdio, official downstream SDK calls, tool continuation, signed thinking, incremental SSE, compression, cancellation and credential redaction. Live checks use the first configured provider for each tested protocol.

```text
cmd/yukino-codex-proxy  CLI entry point
internal/config        Ordered YAML provider selection
internal/codex         Codex TOML, model catalog and exact backups
internal/daemon        Persistent process and local control
internal/bridge        Request, response, history and SSE conversion
internal/upstream      Official OpenAI and Anthropic SDK transports
internal/proxy         yukino_http gateway
internal/mcpserver     Official MCP SDK lifecycle tools
```
