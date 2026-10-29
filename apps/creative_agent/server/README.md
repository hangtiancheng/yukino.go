# yukino-creative-agent — Agent Conversation + Content Creation / Risk-Control Foundation

Two Go services merged into a **single binary**:

- **go-learn**: agent conversation system (creative-gravity replica) — ReAct loop engine + SEDA graph scheduling + Redis Streams async pipeline + multi-agent subtask orchestration.
- **base-server**: content creation / risk-control foundation — creation flow (pre-create / create / post-process / callback), risk audit, points accounting, monitor offline jobs (timeout / compensation / risk offline).

The frontend is an independent React app (`../client`), deployed with **frontend/backend separation**: this service provides the API only (CORS allows cross-origin requests); the frontend is built independently and hosted by any static server (nginx / Vercel / OSS+CDN, etc.).

## Tech stack

| Category       | Technology                                                                                       | Purpose                                                                    |
| -------------- | ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------- |
| Web framework  | [yukino_http](https://github.com/hangtiancheng/yukino.go/tree/main/libs/yukino_http)             | HTTP routing + middleware + SSE                                            |
| ORM            | GORM + MySQL                                                                                     | AutoMigrate for business tables (users + cg\_\* + creation/risk/points)    |
| Message queue  | Redis Streams (go-redis/v9)                                                                      | Async conversation/subtask delivery (consumer group + ACK); falls back to an in-process queue without Redis |
| Cache/locks    | Redis + [yukino_cache](https://github.com/hangtiancheng/yukino.go/tree/main/libs/yukino_cache)   | Chunk relay, heartbeat NX lock, token blacklist; chunk store supports an in-process yukino_cache backend |
| LLM            | eino (OpenAI-compatible / Anthropic Claude)                                                      | Manager + sub-agents (falls back to FakeLLM when unconfigured)             |
| Observability  | OpenTelemetry → Jaeger + Prometheus `/metrics`                                                   | Full-chain tracing (HTTP → Worker → subtask → LLM) + metrics               |
| Error reporting| sentry-go                                                                                        | panic/error reporting (enabled once a DSN is configured)                   |
| Logging        | Zap (structured JSON)                                                                            | Runtime logs + trace_id correlation                                        |
| Configuration  | JSON + go tags                                                                                   | env vars > secrets.json > config.json > defaults                           |
| ID generation  | Snowflake (standard-library implementation)                                                      | Business primary keys for task_run / task_step / message                   |
| Testing        | Standard-library testing + `pkg/testx`                                                           | Assertion helpers (testx) + suite runner (testx/suite)                     |

## Quick start

```bash
# 1. Start dependencies (MySQL + Redis + Jaeger)
docker compose up -d mysql redis jaeger

# 2. Copy and fill in secrets (MySQL password / LLM key / Sentry DSN)
cp config/secrets.example.json config/secrets.json

# 3. Start the service
make run

# 4. Test / build / connectivity probe (the sqlite test driver requires cgo)
make test          # all unit tests
make test-race     # race detector
make build         # build into build/creative-agent
make ping          # probe remote MySQL/Redis connectivity (integration test)
```

The frontend is an independent app at `../client` (frontend/backend separation, not hosted by this service): use `pnpm dev` for development (the vite proxy forwards /api and /aigc);
for production builds run `VITE_API_BASE_URL=<backend address> pnpm build` and deploy `dist/` to any static server — see its README.

> Database connections are configured in the mysql/redis sections of `config/config.json`. The first `AutoMigrate` can be slow; that is normal.

## Project structure

```
cmd/
  api/main.go            # Single entry point (load config → app.New → app.Run)
  checkdb/main.go        # Debug tool: print task_run/task_step state per session
config/
  config.go              # Config loading (priority: env vars > secrets.json > config.json > defaults)
  config.json            # Public config (host/port, including MySQL/Redis/Jaeger/LLM addresses)
  secrets.example.json   # Secret config template (passwords / API keys, git-ignored)
internal/
  core/                    # [Engine layer] Pure algorithms, zero business: SEDA graph engine + ReAct loop
    seda/                  #   Graph engine: Flow scheduling + Slice streaming channel + registry
    react/                 #   ReAct loop: Supervisor five-branch + component interfaces + checkpoint
  appkit/                  # [Reusable skeleton layer] The machinery that runs an agent (no business prompts)
    runner/                #   SupervisorRunner (ReAct run flow + SSE frames + PlanOutput)
    provider/              #   Injection layer (split into sub-packages by capability)
      llm/                 #     LLM implementations (eino ChatModel + DefaultRealLLM/DefaultManagerLLM injection points)
      tools/               #     Tool system core (ToolRegistry/ToolsProvider/built-in tools/composite)
      skill/               #     SKILL.md dynamic tool source (SkillProvider/FSRepository)
      fake/                #     Test stubs (FakeLLM/FakeStrategy/FakeHooks + DefaultLLM factory)
  business/                # [Business layer] Conversation and subtasks
    conversation/          #   Domain 1: conversation main chain (sync graph + async worker)
      flow.go              #     Flow graph definition + operator registration (business entry facade)
      ops/                 #     SEDA operators: con_reader / supervisor_agent / con_writer / con_end
      manager/             #     Manager constraint chain (generate_response protocol / validation / manager prompt)
      memory/              #     IntentMemory + FlowStage
      fastpath/            #     Fast path (HIL resume / Retry)
      worker/              #     Conversation async worker (Consumer / Recovery / heartbeat / attach)
    subtask/               #   Domain 2: subtask execution and scheduling (purely async)
      pipeline.go          #     Pipeline definitions (mix_video / article)
      executor/            #     TaskExecutor / SubAgent / Submitter
      taskflow/            #     Consumer / Poller / CronTaskPoller
  auth/                    # [base-server] Auth DDD module (not wired into the router, kept for reference)
  creation/                # [base-server] Creation flow (pre-create/create/post-process/callback/compensation)
  risk/                    # [base-server] Risk audit
  points/                  # [base-server] Points accounting
  monitor/                 # [base-server] Offline jobs (timeout/compensation/risk-offline jobs)
  model/                   # [Infrastructure] GORM models + Snowflake ID (standard-library implementation)
  repository/              # [Infrastructure] Data access layer (interfaces + GORM implementations)
  infra/                   # [Infrastructure] ChunkStore / StreamQueue / Locker (Redis and in-process dual backends)
  service/                 # User/auth business (JWT issuance)
  handler/                 # [Boundary] HTTP handlers (auth / conversation / card confirmation / callback)
  router/                  # [Boundary] Route registration (API + /aigc + /inner + /metrics)
  middleware/              # [Boundary] Middleware (RequestID → OTel → Logger → CORS → Recovery → JWT)
  database/                # MySQL initialization + AutoMigrate
  app/                     # [Boundary] Startup orchestration + layer wiring + graceful shutdown + async worker startup
pkg/
  sse/                   # SSE Writer + event types
  otelx/                 # OTel initialization (memory/OTLP exporter) + GORM zap logger + GORM SQL tracing plugin
  tracex/                # Cross-process traceparent propagation + logging helpers (trace_id correlation)
  cache/                 # Redis client
  logger/                # Zap wrapper
  response/              # Unified response format + error codes
  testx/                 # Standard-library assertion helpers (replacing testify) + suite runner (testx/suite)
skills/                  # SKILL.md dynamic tool sources (e.g. weather)
```

## Architecture overview

```
                    ┌────────────────────────────────────────────────────┐
                    │                HTTP layer (yukino_http)            │
                    │            POST/GET /aigc/conversation             │
                    └──────────────┬─────────────────────────────────────┘
                                   │ body + trace
                    ┌──────────────▼─────────────────────────────────────┐
                    │      SEDA graph scheduling (core/seda, flow.go)    │
                    │  con_reader → supervisor_agent → con_writer → end  │
                    │      (fork / join / short-circuit / Slice stream)  │
                    └──────────────┬──────────────────┬──────────────────┘
                                   │ sync             │ async
                                   │                  │ push to Redis Stream
                    ┌─────────────────────┐   ┌──────────────────────────┐
                    │     ReAct engine    │   │  Worker (conversation/   │
                    │    core/react +     │   │         worker)          │
                    │    appkit/runner    │   │  CAS claim + heartbeat + │
                    │   five branches:    │   │  recovery; runs the same │
                    │ Continue/Terminal/  │   │  ReAct engine; chunks to │
                    │  Interrupt/Relay/   │   │  store → frontend attach │
                    │        Retry        │   │  resumes from breakpoint │
                    └──────────┬──────────┘   └───────────────┬──────────┘
                               │                              │ new_agents_plan
                    ┌──────────▼──────────────────────────────▼──────────┐
                    │       Subtask system (business/subtask)            │
                    │  manager produces plan → Submitter dispatches →    │
                    │  TaskExecutor runs sub-agents → Poller streams     │
                    │  task_step progress back                           │
                    └──────────┬──────────────────────────┬──────────────┘
                               │ conversation-driven      │ cron-driven
                               │ card confirm → fast path │ CronTaskPoller every 30s
                               └──────────────────────────┘

Creation / risk-control foundation (base-server domain, same process):
  /inner/creation/*  pre-create (risk pre-check + points freeze) → create → sync/async post-process → callback
  /inner/callback/*  external task callbacks (task_detail / risk) → advance task_run/task_step
  monitor jobs       timeout scan / compensation / risk offline audit
```

### Reuse of the three core engines

The same ReAct engine (`core/react`) is assembled by `appkit/runner` and **reused in three places**, injecting different components only:

| Call site       | Injected LLM          | Strategy                       | Responsibility                 |
| --------------- | --------------------- | ------------------------------ | ------------------------------ |
| Sync supervisor | FakeLLM / real LLM    | FakeStrategy / ManagerStrategy | Reply directly within request  |
| Async Worker    | Same as above         | Same as above + FakeHILStrategy| Manager decision, produce plan |
| Sub-agent       | FakeSubLLM / real LLM | Continue while tools exist     | Execute a single pipeline task |

### Data flow: one "generate a 15s short video for me" request

```
1. User POST /aigc/conversation
2. con_reader: create session/message_record/async_request (Pending), write to Stream
3. Worker CAS claim (Pending→InProgress) → heartbeat → run manager ReAct
4. Manager LLM calls generate_response(new_agents_plan, agents_pipeline=mix_video)
5. Server "only translates": validate agents belong to the pipeline → expand plan_steps (drop idea_king)
6. Submitter persists task_run(Pending) first, then pushes to Stream
7. TaskExecutor consumes → runs sub-agent (gold_scriptwriter) → creates task_step
8. Poller polls task_step and pushes progress frames back to the frontend (task-step / task-done)
9. When everything completes → message-complete → chat_text persisted → frontend released
```

## Core design points

### 1. ReAct engine (`internal/core/react/supervisor.go`)

A zero-business loop engine: `call LLM → Strategy decides → dispatch by Action`. Five branches:

- **Continue**: execute the tool, feed the result back as an observation, keep looping
- **Terminal**: terminate normally, output friendly_reply
- **Interrupt**: HIL breakpoint; save the checkpoint and return (the frontend can resume with "continue")
- **Relay**: server-side self Q&A; inject a new query and rebuild the prompt
- **Retry**: non-conforming output; inject a correction hint and redo without consuming a step

All business behavior (prompt / tools / decisions / hooks) is injected via LLM, Tools, Strategy, and Hooks.

### 2. Manager constraint chain (`internal/business/conversation/manager/manager_strategy.go`)

The manager LLM must communicate with the server through the `generate_response` tool, with four layers of constraints:

1. **Forced function calling**: no tool_calls → immediate Retry
2. **Tool-name whitelist**: only generate_response / idea_king_api allowed
3. **Schema validation**: task_type enum + agent whitelist + JSON repair for arguments
4. **Failure closure**: every failure injects a correction hint (the engine retries at most twice)

### 3. Multi-agent pipelines (`internal/business/subtask/pipeline.go` + `pipeline_registry.go`)

**The LLM decides how to assemble the chain; the server only translates**:

- Pipeline templates are fixed: `mix_video` (idea_king → gold_scriptwriter → edit_master), `article`
- The LLM decides the chain order/composition in `task_arguments.agents`
- The server only: validates agents belong to the selected pipeline (prevents overreach), fills missing inputs, drops the topic-selection agent (handled by the idea_king_api tool)
- Chain advancement: the **conversation path** relies on "card confirmation → FlowStage advance → fast-path redispatch"; the **cron path** relies on CronTaskPoller auto-advancing

### 4. Async pipeline reliability

| Mechanism             | Description                                                                                      |
| --------------------- | ------------------------------------------------------------------------------------------------ |
| Persist before send   | task_run is persisted as soon as it is created Pending; delivery failure marks it Failed without blocking the main flow |
| NX lock               | `cg:hb:{msgID}` prevents duplicate execution (Redis SetNX / in-process MemoryLocker)             |
| MySQL CAS claim       | async_request Pending→InProgress, AttemptCount atomically +1                                     |
| Heartbeat             | heartbeat_at refreshed every 5s; considered dead after 15s                                       |
| Zombie recovery       | RecoveryScanner scans expired InProgress records every 30s: has EndTurn → complete it; otherwise → reset to Pending and redispatch |
| Breakpoint resume     | Frontend resumes from the chunk store with a lastSeq cursor (no frames lost on reconnect)        |

### 5. Dual-backend infrastructure (`internal/infra`)

ChunkStore / StreamQueue / Locker each have two implementations; `app.New` picks automatically based on Redis availability:

- **Redis backend**: cross-process deployment (production)
- **In-process backend**: MemoryChunkStore (yukino_cache Group) / MemoryStreamQueue / MemoryLocker (single binary, no Redis, unit tests)

### 6. Tool system (`internal/appkit/provider/tools/` + `internal/appkit/provider/skill/`)

- **ToolRegistry** (`provider/tools/tools.go`): compile-time Register in Go code (built-in tools: search / generate_image / create_video_task / dispatch_agent)
- **SkillProvider** (`provider/skill/skill.go`): scans `skills/*/SKILL.md`; after lazy loading via `skill_load`, APIs enter the tool list dynamically; `${VAR}` env-var expansion; `injected: true` parameters are injected server-side
- **CompositeProvider** (`provider/tools/composite.go`): merges built-in + dynamic tool sources
- Tool failures do not break the loop; the error string is fed back as an observation so the LLM decides what to do

### 7. Observability

Full-chain tracing: HTTP span → `supervisor.react` → LLM call (otelhttp outbound) → traceparent propagated through Stream → Worker `worker.process` → subtask `task.process`. Logs are correlated via trace_id through `tracex.Logger`; `/metrics` exposes Prometheus metrics; panics and errors are reported via sentry-go (once a DSN is configured).

## API

### REST (`/api/v1`)

| Method | Path               | Description                | Auth         |
| ------ | ------------------ | -------------------------- | ------------ |
| GET    | `/api/v1/ping`     | Health check               | Public       |
| POST   | `/api/v1/register` | User registration          | Public       |
| POST   | `/api/v1/login`    | Login to obtain a JWT      | Public       |
| GET    | `/api/v1/example`  | Protected example          | Bearer token |
| POST   | `/api/v1/logout`   | Logout (blacklist token)   | Bearer token |

### Conversation (`/aigc`)

| Method   | Path                               | Description                                                                  |
| -------- | ---------------------------------- | ---------------------------------------------------------------------------- |
| POST/GET | `/aigc/conversation`               | Agent conversation (SSE stream; GET for EventSource, reconnect/lastSeq resume)|
| POST     | `/aigc/agent/submit_clarification` | Card confirmation (writes back to intent_memory, advances FlowStage)         |
| POST     | `/aigc/callback/task_detail`       | External task callback (JWT-exempt, server-to-server)                        |

### Creation / risk (`/inner`, base-server domain)

| Method | Path                                | Description                                |
| ------ | ----------------------------------- | ------------------------------------------ |
| POST   | `/inner/creation/pre_create`        | Pre-create (risk pre-check + points freeze)|
| POST   | `/inner/creation/create`            | Create a creation task                     |
| POST   | `/inner/creation/sync_post_create`  | Synchronous post-processing                |
| POST   | `/inner/creation/async_post_create` | Asynchronous post-processing               |
| POST   | `/inner/creation/handle_error`      | Error handling (refund/compensation)       |
| POST   | `/inner/callback/task_detail`       | Creation task callback                     |
| POST   | `/inner/callback/risk`              | Risk callback                              |

### Operations

| Method | Path       | Description                                          |
| ------ | ---------- | ---------------------------------------------------- |
| GET    | `/metrics` | Prometheus metrics (when `metrics.enabled=true`)     |

Request example (conversation POST):

```bash
curl -N -X POST http://localhost:8080/aigc/conversation \
  -H "Content-Type: application/json" -H "Authorization: Bearer <token>" \
  -d '{"sessionId":"s1","agentId":"demo","userId":1,"query":"帮我生成一条15s的混剪短视频"}'
```

The response is an SSE event stream:

```
data: {"type":"message-start","msgId":"2084621017589747712"}

data: {"type":"text-delta","content":"好的，我将为你生成","textId":"t1"}

data: {"type":"task-step","msgId":"...","agentId":"gold_scriptwriter","status":"running"}

data: {"type":"message-complete","msgId":"2084621017589747712"}
```

SSE event types:

| event                                  | Description                                        |
| -------------------------------------- | -------------------------------------------------- |
| `message-start`                        | Conversation started, carries msgId                |
| `text-delta`                           | Streaming text delta                               |
| `tool-call-start` / `tool-call-result` | Tool call started / result                         |
| `interrupt`                            | HIL breakpoint (frontend shows a "Continue" button)|
| `task-step` / `task-done`              | Subtask progress / completion                      |
| `message-complete`                     | Conversation completed                             |
| `error`                                | Execution error                                    |

## Configuration

Loading priority: **env vars > `config/secrets.json` > `config/config.json` > `config.go` defaults**.
Env vars are derived from the JSON path (uppercase + underscores); the `env` tag can override (e.g. `IDEA_KING_BASE_URL`).

| Env var                                        | Default                         | Description                                          |
| ---------------------------------------------- | ------------------------------- | ---------------------------------------------------- |
| `SERVER_PORT`                                  | `8080`                          | HTTP port                                            |
| `JWT_SECRET`                                   | `dev-secret-change-me`          | JWT signing secret                                   |
| `MYSQL_HOST` / `MYSQL_PORT` / `MYSQL_DATABASE` | `127.0.0.1:3306` / `all_in_one` | MySQL                                                |
| `REDIS_HOST` / `REDIS_PORT` / `REDIS_DB`       | `127.0.0.1:6379` / `0`          | Redis (falls back to in-process backends when unreachable) |
| `ASYNC_ENABLED`                                | `true`                          | Async pipeline switch (off = pure sync mode)         |
| `LLM_PROVIDER`                                 | `openai`                        | `openai` (OpenAI-compatible) / `claude` (Anthropic)  |
| `LLM_BASE_URL` / `LLM_API_KEY` / `LLM_MODEL`   | —                               | Real LLM. Enabled only when configured, otherwise FakeLLM |
| `CRON_POLL_INTERVAL`                           | `30s`                           | Cron multi-agent auto-advance interval               |
| `OTEL_ENABLED` / `OTEL_EXPORTER_ENDPOINT`      | `false` / `localhost:4317`      | OpenTelemetry (OTLP gRPC)                            |
| `SENTRY_DSN`                                   | —                               | Sentry error reporting (disabled when empty)         |
| `METRICS_ENABLED`                              | `true`                          | Prometheus `/metrics` switch                         |
| `LOG_LEVEL`                                    | `info`                          | Log level                                            |

See [config/config.go](config/config.go) for the full field list.

## Development guidelines

| Rule              | Description                                                                                     |
| ----------------- | ----------------------------------------------------------------------------------------------- |
| Layering          | Handlers only bind parameters and call services; business logic lives in the service layer      |
| Middleware order  | RequestID → OTel → Logger → CORS → Recovery → business middleware (JWT)                         |
| Configuration     | JSON + go tags; env vars > secrets.json > config.json > defaults                                |
| Testing           | Standard-library testing + `pkg/testx` (assertions) + `pkg/testx/suite` (suites); sqlite cases need `CGO_ENABLED=1` |
| Logging           | All runtime logging goes through zap (JSON); do not use `log.Printf` directly                   |
| Language          | All documentation, comments, and log messages are written in English                            |
| Commits           | Write git commit messages in English                                                            |
