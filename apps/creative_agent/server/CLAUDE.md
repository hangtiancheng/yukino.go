# yukino-creative-agent — Agent Conversation + Content Creation / Risk-Control Foundation

A single-binary service merging go-learn (agent conversation: ReAct engine + SEDA graph scheduling + Streams async pipeline + multi-agent subtask orchestration) and base-server (creation flow / risk / points / monitor offline jobs). The frontend is an independent React app (`../client`), deployed with frontend/backend separation: this service provides the API only; the frontend is built and hosted independently.

## Tech stack

- **Web framework**: yukino_http (routing + middleware + SSE)
- **ORM**: GORM + MySQL (AutoMigrate; users + cg\_\* + creation/risk/points tables)
- **Message queue**: Redis Streams (go-redis/v9, consumer group + ACK); falls back to an in-process queue without Redis
- **Cache/locks**: Redis + yukino_cache (ChunkStore/StreamQueue/Locker all have Redis and in-process dual backends)
- **Auth**: JWT (golang-jwt)
- **LLM**: eino ChatModel (OpenAI-compatible / Anthropic Claude; falls back to FakeLLM when unconfigured)
- **Observability**: OpenTelemetry → Jaeger (full chain HTTP → Worker → subtask → LLM) + Prometheus /metrics + sentry-go
- **Logging**: Zap (structured JSON)
- **Testing**: standard-library testing + pkg/testx (assertions) + pkg/testx/suite (suites); sqlite cases need CGO_ENABLED=1
- **Configuration**: JSON + go tags (env vars > secrets.json > config.json > defaults)

## Quick start

```bash
# Start MySQL + Redis + Jaeger before local development
docker compose up -d mysql redis jaeger

# Secrets (MySQL password / LLM key / Sentry DSN)
cp config/secrets.example.json config/secrets.json

# Then start the service
make run
make test                     # run all tests (CGO_ENABLED=1)
make build                    # build into build/creative-agent
make ping                     # remote MySQL/Redis connectivity probe
```

## Directory structure

```
cmd/api/main.go       # Single entry point (load config → app.New → app.Run)
cmd/checkdb/main.go   # Debug tool: print task_run/task_step state per session
config/               # Config loading (env vars > secrets.json > config.json > defaults)
internal/
  core/                  # [Engine layer] Pure algorithms, zero business: SEDA graph engine + ReAct loop
    seda/                #   Graph engine: Flow scheduling + Slice streaming channel + registry
    react/               #   ReAct loop: Supervisor five-branch + component interfaces + checkpoint
  appkit/                # [Reusable skeleton layer] The machinery that runs an agent (no business prompts)
    runner/              #   SupervisorRunner (ReAct run flow + SSE frames + PlanOutput)
    provider/            #   Injection layer (split into sub-packages by capability)
      llm/               #     LLM implementations (eino ChatModel + DefaultRealLLM/DefaultManagerLLM injection points)
      tools/             #     Tool system core (ToolRegistry/ToolsProvider/built-in tools/composite)
      skill/             #     SKILL.md dynamic tool source (SkillProvider/FSRepository)
      fake/              #     Test stubs (FakeLLM/FakeStrategy/FakeHooks + DefaultLLM factory)
  business/              # [Business layer] Conversation and subtasks
    conversation/        #   Domain 1: conversation main chain (sync graph + async worker)
      flow.go            #     Flow graph definition + operator registration (business entry facade)
      ops/               #     SEDA operators: con_reader / supervisor_agent / con_writer / con_end
      manager/           #     Manager constraint chain (generate_response protocol / validation / manager prompt)
      memory/            #     IntentMemory + FlowStage
      fastpath/          #     Fast path (HIL resume / Retry)
      worker/            #     Conversation async worker (Consumer / Recovery / heartbeat / attach)
    subtask/             #   Domain 2: subtask execution and scheduling (purely async)
      pipeline.go        #     Pipeline definitions (mix_video / article)
      executor/          #     TaskExecutor / SubAgent / Submitter
      taskflow/          #     Consumer / Poller / CronTaskPoller
  auth/                  # [base-server] Auth DDD module (not wired into the router, kept for reference)
  creation/              # [base-server] Creation flow (pre-create/create/post-process/callback/compensation)
  risk/                  # [base-server] Risk audit
  points/                # [base-server] Points accounting
  monitor/               # [base-server] Offline jobs (timeout/compensation/risk-offline jobs)
  model/                 # [Infrastructure] GORM models + Snowflake ID (standard-library implementation)
  repository/            # [Infrastructure] Data access layer (interfaces + GORM implementations)
  infra/                 # [Infrastructure] ChunkStore / StreamQueue / Locker (Redis and in-process dual backends)
  service/               # User/auth business (JWT issuance)
  handler/               # [Boundary] HTTP handlers (auth / conversation / card confirmation / callback)
  router/                # [Boundary] Route registration (API + /aigc + /inner + /metrics)
  middleware/            # [Boundary] Middleware (RequestID → OTel → Logger → CORS → Recovery → JWT)
  database/              # MySQL initialization + AutoMigrate
  app/                   # [Boundary] Startup orchestration + layer wiring + graceful shutdown + async worker startup
pkg/
  sse/                # SSE Writer + event types
  otelx/              # OTel initialization + GORM zap logger + GORM SQL tracing plugin
  tracex/             # Cross-process traceparent propagation + logging helpers
  cache/              # Redis client
  logger/             # Zap wrapper
  response/           # Unified response format + error codes
  testx/              # Standard-library assertion helpers + suite runner (replacing testify)
skills/               # SKILL.md dynamic tool sources (e.g. weather)
Makefile              # Build/test/run/connectivity probe
Dockerfile            # Multi-stage build
docker-compose.yml    # MySQL + Redis + Jaeger
.env.example          # Env-var template
```

## Architecture highlights

- **One ReAct engine reused in three places**: `core/react` engine + `appkit/runner` assembly; sync path / async Worker / sub-agents differ only in the injected LLM/Strategy/Hooks.
- **Manager constraint chain**: `business/conversation/manager/manager_strategy.go` enforces function calling + tool-name whitelist + schema validation + failure closure (Retry fallback).
- **LLM decides the agent chain**: `business/subtask/pipeline.go` defines pipelines → valid agent sets; the LLM fills the agents array and the server only translates (validate / fill inputs / drop idea_king).
- **Async reliability**: persist before dispatch + NX lock (Redis/memory) + MySQL CAS claim + heartbeat + RecoveryScanner zombie recovery + lastSeq breakpoint resume.
- **Multi-agent advancement**: conversation path = card confirmation → FlowStage (intent_memory) → fast-path redispatch; cron path = CronTaskPoller auto-advances every 30s.
- **Real-LLM switch**: enabled only when `llm.api_key`/`llm.base_url` are configured (manager forces tool_choice=required, sub-agents auto); otherwise falls back to FakeLLM table-lookup simulation.
- **Dual-backend degradation**: when Redis is unreachable, app.New automatically switches to MemoryChunkStore (yukino_cache) / MemoryStreamQueue / MemoryLocker, so the single binary runs without Redis.

## Development guidelines

| Rule              | Description                                                                                              |
| ----------------- | -------------------------------------------------------------------------------------------------------- |
| Layering          | Handlers only bind parameters and call services; business logic lives in the service layer               |
| Middleware order  | RequestID → OTel → Logger → CORS → Recovery → business middleware (JWT)                                  |
| Configuration     | JSON + go tags; env vars > secrets.json > config.json > defaults                                         |
| Testing           | Standard-library testing + pkg/testx + pkg/testx/suite; test files use external `_test` package names; sqlite cases need CGO_ENABLED=1 |
| Logging           | All runtime logging goes through zap (JSON); do not use `log.Printf` directly                            |
| Language          | All documentation, comments, and log messages are written in English                                     |

## API

| Method   | Path                                              | Description                                                                          |
| -------- | ------------------------------------------------- | ------------------------------------------------------------------------------------ |
| GET      | /api/v1/ping                                      | Health check (public)                                                                |
| POST     | /api/v1/register                                  | User registration (public)                                                           |
| POST     | /api/v1/login                                     | Login to obtain a JWT (public)                                                       |
| GET      | /api/v1/example                                   | Example (requires Authorization: Bearer token)                                       |
| POST     | /api/v1/logout                                    | Logout, blacklist the token (requires auth)                                          |
| POST/GET | /aigc/conversation                                | Agent conversation (SSE stream; GET for EventSource, with breakpoint resume)         |
| POST     | /aigc/agent/submit_clarification                  | Card confirmation (advances FlowStage)                                               |
| POST     | /aigc/callback/task_detail                        | External task callback (JWT-exempt)                                                  |
| POST     | /inner/creation/\*                                | Creation flow (pre_create / create / sync_post_create / async_post_create / handle_error) |
| POST     | /inner/callback/task_detail, /inner/callback/risk | Creation/risk callbacks                                                              |
| GET      | /metrics                                          | Prometheus metrics                                                                   |

> See [README.md](README.md) for the full README.
