<div align="center">

# yukino_agent

**An AI OnCall assistant — RAG chat, plan-execute-replan agents, and a Prometheus bridge.**

A Go agent backend built on [CloudWeGo Eino](https://github.com/cloudwego/eino): retrieval-augmented chat with tool calling, an autonomous alert-analysis pipeline, a Redis Stack vector knowledge base, and end-to-end browser observability that turns frontend reports into Prometheus metrics.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-yukino__agent-blue)](go.mod)

</div>

---

## Overview

yukino_agent exposes an HTTP API and two agentic pipelines:

1. **Chat pipeline** — a context-aware RAG assistant. It classifies each question, optionally retrieves internal documentation from a Redis vector store, and calls tools (current time, log search, Prometheus alerts, MySQL CRUD) in a ReAct-style loop.
2. **Plan-Execute-Replan pipeline** — given a goal (by default, "analyze all active alerts"), it builds a step list, executes each step with the tool set, and replans until the objective is met, producing a structured operations report.

On top sits a monitoring bridge: the browser SDK (`@yukino.js/sentry`) posts reports to `POST /api/log`, which are converted into Prometheus metrics served from `GET /api/metrics`.

## Architecture

```text
                       ┌──────────────────────────────────────────────┐
   HTTP (yukino_http)  │  POST /api/chat           (RAG chat)         │
   ──────────────────> │  POST /api/chat_stream    (SSE)              │
                       │  POST /api/upload         (index docs)       │
                       │  POST /api/ai_ops         (plan-exec-replan) │
                       │  POST /api/log            (sentry bridge)    │
                       │  GET  /api/metrics        (Prometheus)       │
                       └───────────────┬──────────────────────────────┘
                                       │
                    ┌──────────────────┴───────────────────┐
                    V                                      V
        ┌───────────────────────┐              ┌────────────────────────┐
        │   chat_pipeline       │              │ plan_execute_replan    │
        │ classify → retrieve  │              │  planner → executor   │
        │ → ReAct tools → LLM │              │  → replanner (loop)   │
        └──────────┬────────────┘              └───────────┬────────────┘
                   └──────────────┬────────────────────────┘
                                  V
                    ┌─────────────────────────────┐
                    │  tools                      │
                    │  get_current_time           │
                    │  log MCP tool (SSE)         │
                    │  query_prometheus_alerts    │
                    │  query_internal_docs        │
                    │  mysql_crud                 │
                    └──────────┬──────────────────┘
                               V
        ┌────────────────────────────────────────────┐
        │  Redis Stack (RediSearch) vector knowledge │
        │  idx:biz · key prefix biz: · COSINE/HNSW   │
        └────────────────────────────────────────────┘
```

### Model roles

| Model               | Used for                                  | Config key         |
| ------------------- | ----------------------------------------- | ------------------ |
| **Think model**     | Planning and replanning (deep reasoning). | `think_chat_model` |
| **Quick model**     | Chat responses and tool execution.        | `quick_chat_model` |
| **Embedding model** | Vectorizing documents and queries.        | `embedding_model`  |

Both chat models accept an OpenAI-compatible endpoint or Anthropic; the embedding model supports OpenAI-compatible only. The vector dimension is probed from the live provider at startup, so it never needs to be configured.

## Features

- **RAG with deduplication** — documents are split, embedded, and stored in Redis Stack with a `_source` tag; re-indexing a file first removes prior chunks with the same source.
- **Tool calling** — a `get_current_time` tool, an MCP-backed log-search tool over SSE, a Prometheus alerts tool, an internal-docs retriever, and a MySQL CRUD tool. Each tool lives in its own file under `internal/ai/tools`.
- **Graceful degradation** — a missing MCP server or embedder disables that capability instead of failing startup.
- **Plan-Execute-Replan** — JSON-only planner/replanner prompts keep the loop parseable; the executor runs steps against the bound tool set.
- **Conversation memory** — per-session memory keeps follow-up questions coherent.
- **Native Float32 vectors** — embeddings are stored as Float32 with COSINE similarity and an HNSW index, giving higher retrieval fidelity than binary + Hamming.
- **Browser observability bridge** — every SDK report type except ScreenRecord maps to a Prometheus metric with names, labels, and buckets identical to a sibling Next.js bridge, so one Prometheus and one rule file cover both.

## Quick start

### With Docker (Redis Stack + monitoring)

```bash
docker compose up -d          # redis-stack, prometheus, grafana
```

### With Homebrew (macOS)

```bash
# Redis Stack (includes the RediSearch module)
brew tap redis-stack/redis-stack
brew install --cask redis-stack
brew services stop redis                 # plain redis also uses :6379
redis-stack-server --daemonize yes       # casks are not managed by brew services

# Optional monitoring
brew install prometheus grafana
cp prometheus.rules.yml /opt/homebrew/etc/prometheus.rules.yml
brew services start prometheus grafana
```

> [!NOTE]
> Prometheus runs without `--web.enable-lifecycle`, so `POST /-/reload` returns `403`. Restart it to pick up rule changes.

### Configure & run

```bash
cp config.example.jsonc config.json      # then edit config.json
go run .
```

The Go backend reads `config.json` (not environment variables). `config.example.jsonc` documents every field and its default.

```bash
make run        # go run .
make dev        # hot reload with air
make build      # bin/yukino-agent
make test       # go test -race -cover ./...
```

The server listens on `:8123` by default.

## API

| Method | Path               | Description                                                                 |
| ------ | ------------------ | --------------------------------------------------------------------------- |
| `POST` | `/api/chat`        | Non-streaming RAG chat. Body: `{ "id", "question" }`.                       |
| `POST` | `/api/chat_stream` | Streaming chat over SSE; emits `connected`, token, and error events.        |
| `POST` | `/api/upload`      | Upload a `.txt` / `.md` file into the knowledge base.                       |
| `POST` | `/api/ai_ops`      | Run the plan-execute-replan alert analysis; returns `{ result, detail[] }`. |
| `POST` | `/api/log`         | Monitoring report sink (the browser SDK `dsn`).                             |
| `GET`  | `/api/metrics`     | Prometheus exposition.                                                      |

```bash
curl -s localhost:8123/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"id":"session-1","question":"What is the cause of a service going offline?"}'
```

Responses use the `{ "message", "data" }` envelope. `data` is `null` on errors, which the bundled frontend relies on.

## Standalone commands

`cmd/` contains focused entry points for exercising each pipeline:

| Command                  | Purpose                                                  |
| ------------------------ | -------------------------------------------------------- |
| `go run ./cmd/chat`      | Interactive RAG chat, two turns, demonstrating memory.   |
| `go run ./cmd/ai_ops`    | Run the alert-analysis plan-execute-replan agent.        |
| `go run ./cmd/knowledge` | Batch-index every `.md` file under `file_dir`.           |
| `go run ./cmd/recall`    | Query the Redis retriever and print retrieved documents. |
| `go run ./cmd/llm_tool`  | Test tool binding with the quick model.                  |

> [!TIP]
> On first use, upload a document via the `/api/upload` endpoint (or the frontend's "…" menu) so the knowledge base has content; otherwise retrieval returns empty.

## Configuration

| Field                               | Default                 | Notes                                                         |
| ----------------------------------- | ----------------------- | ------------------------------------------------------------- |
| `server_addr`                       | `:8123`                 | HTTP listen address.                                          |
| `model_provider`                    | `openai`                | `openai` or `anthropic`.                                      |
| `think_chat_model`                  | --                      | `{ api_key, base_url, model, max_tokens, thinking }`.         |
| `quick_chat_model`                  | --                      | Same shape; used for chat and tool calls.                     |
| `embedding_model`                   | --                      | `provider` (`openai`only), OpenAI fields.                     |
| `file_dir`                          | `./data/docs`           | Upload and indexing directory.                                |
| `mcp_url`                           | --                      | MCP log-tool SSE endpoint.                                    |
| `prometheus_url`                    | `http://127.0.0.1:9090` | Empty disables `query_prometheus_alerts`.                     |
| `log_topic_region` / `log_topic_id` | --                      | Injected into the chat system prompt when both are non-empty. |
| `redis`                             | `localhost:6379`        | `{ addr, password, db }` for Redis Stack.                     |

> [!IMPORTANT]
> For Anthropic, `base_url` must **not** include `/v1` — the SDK appends `/v1/messages`. For OpenAI-compatible endpoints, `base_url` is used as-is and typically does include `/v1`.

Constants aligned with a sibling Next.js deployment live in `internal/consts/consts.go`:

```
REDIS_INDEX_NAME = idx:biz
REDIS_KEY_PREFIX = biz:
REDIS_VECTOR_FIELD = vector
MAX_CONTENT_LENGTH = 8192
```

## Monitoring

```text
yukino-sentry browser SDK
        │  POST /api/log
        V
internal/app/sentry_metrics_handler.go
        │
        V
GET /api/metrics  ──>  Prometheus  ──>  Grafana
        ^
        └── prometheus.rules.yml (alert rules)
```

The bridge covers every SDK report type except ScreenRecord: errors and framework crashes, resource failures, HTTP, Web Vitals, navigation and resource timing, long tasks, browser memory, clicks, exposure, white screen, page views and dwell, and custom events.

- **Contract with AI Ops** — alert names are an API. The pipeline calls `query_prometheus_alerts`, then `query_internal_docs` with the alert name, so every rule needs a matching heading in `data/docs/alert-handling-guide.md`.
- **Runtime coverage** — the Go collector is opted into `runtime/metrics` for GC, memory, scheduler, CPU-class, sync, and cgo families (`go_sched_latencies_seconds`, `go_gc_pauses_seconds`, `go_memory_classes_*`, …). `/godebug/*` is excluded as always-zero noise.
- **Derived gauges** — `yukino_go_memory_limit_bytes` and `yukino_go_heap_used_ratio` fill what the collectors lack.
- **Label cardinality** — browser-supplied label values are capped at 50 distinct values; overflow becomes `other`.

```bash
go test ./internal/app/                    # event matrix, per-event values, malformed payloads
promtool check rules prometheus.rules.yml
```

## Project layout

```
yukino_agent/
├── main.go                 # HTTP server bootstrap
├── config.example.jsonc    # fully documented configuration reference
├── docker-compose.yml      # redis-stack + prometheus + grafana
├── prometheus.yml          # scrape config
├── prometheus.rules.yml    # alert rules (names match the docs guide)
├── data/docs/              # knowledge base (includes alert-handling-guide.md)
├── cmd/                    # standalone entry points (chat, ai_ops, knowledge, …)
├── internal/
│   ├── ai/
│   │   ├── agent/chat_pipeline/            # classify → retrieve → tools → LLM
│   │   ├── agent/plan_execute_replan/      # planner / executor / replanner
│   │   ├── agent/knowledge_index_pipeline/ # load → transform → index
│   │   ├── tools/                          # one file per tool
│   │   ├── embedder/ indexer/ loader/ retriever/ models/
│   ├── app/                # yukino_http routes + handlers (chat, upload, ai_ops, metrics)
│   ├── config/             # config loading + defaults
│   ├── consts/             # shared constants (Redis schema, limits)
│   └── utility/            # logging, memory, redis, callbacks
└── fe/                     # (legacy) React frontend; the Lit app lives in packages/yukino-agent
```

## Testing

```bash
make test        # go test -race -cover ./...
make vet         # go vet ./...
```

## Appendix: prompts

The agent's behavior is defined by these prompts.

### 1. Chat system prompt

Source: `internal/ai/agent/chat_pipeline/prompt.go` — `buildSystemPrompt()`.

```md
# Role: Conversational Assistant

## Core Capabilities

- Context-aware conversation and dialogue
- Web search for information retrieval

## Interaction Guidelines

- Before responding, ensure you:
  - Fully understand the user's needs and questions; ask for clarification if unclear
  - Consider the most appropriate solution approach
    %s
- When providing assistance:
  - Use clear and concise language
  - Provide practical examples when appropriate
  - Reference documentation when helpful
  - Suggest improvements or next steps when applicable
- If a request exceeds your capabilities:
  - Clearly state your limitations and suggest alternative approaches
- For complex or compound questions, think step by step rather than rushing to a low-quality answer.

## Output Requirements

- Readable and well-structured with line breaks where necessary
- Output markdown only

## Context Information

- Current date: {date}
- Related documents: |-
  ==== Documents Start ====
  {documents}
  ==== Documents End ====
```

### 2. AI Ops query

Source: `internal/ai/agent/plan_execute_replan/query.go` — `AIOnOpsQuery`.

```md
1. You are an intelligent service alert analysis assistant. First, call the tool query_prometheus_alerts to retrieve all active alerts.
2. For each alert, call the tool query_internal_docs by alert name to retrieve the corresponding handling procedure.
3. Strictly follow the internal documentation for queries and analysis; do not use any information outside the documentation.
4. For any time-related parameters, first call the tool get_current_time to obtain the current time, then pass parameters according to the tool's time requirements.
5. For log queries, first use the log tool to retrieve relevant log information; parameters must include the region and log topic.
6. Summarize and analyze the information retrieved for each alert, then generate an alert operations analysis report in Chinese (中文) in the following format:

告警分析报告

---

# 告警处理详情

## 活跃告警列表

## 告警归因 N (第 N 个告警)

## 处理流程 N (第 N 个告警)

## 结论
```

### 3. Planner prompt

Source: `internal/ai/agent/plan_execute_replan/planner.go` — `NewPlanner()` / `genInputFn`.

```md
Break down the following task into concrete steps.

Task:
%s

Respond with ONLY a JSON object in this exact format:
{
"steps": ["step 1 description", "step 2 description", ...]
}

Do not include any other text, explanations, or markdown formatting. Only output the JSON object.
```

### 4. Replanner prompt

Source: `internal/ai/agent/plan_execute_replan/replan.go` — `customReplanner.Run()`.

```md
You are a replanning agent reviewing execution progress toward an objective. Analyze the completed steps and their outcomes to decide whether the objective is fully achieved or further action is required.

Task:
%s

Original Plan:
%s

Completed Steps:
%s

Results So Far:
%s

Based on the progress above, respond with ONLY a JSON object matching this schema:
{
"done": <boolean>,
"remaining": ["<step>", ...],
"summary": "<final report when done, otherwise empty string>"
}

Set "done" to true and provide a comprehensive summary only when the objective is fully achieved. Otherwise, set "done" to false and list only the remaining steps. Do not include any text, explanations, or markdown formatting outside the JSON object.
```

## License

[MIT](../LICENSE) © hangtiancheng
