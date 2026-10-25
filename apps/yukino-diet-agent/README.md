# yukino-diet-agent

A multi-agent diet recommendation service, migrated from the Java Spring Boot
`Diet-agent` to Go. The backend is built on
[`yukino_http`](https://github.com/hangtiancheng/yukino.go/tree/main/libs/yukino_http)
and MySQL via GORM, with LLM agents powered by DashScope (Qwen) through
agentscope-go.
The frontend is a React 19 + Vite + Tailwind CSS 4 + shadcn/ui single-page app.

## How it works

Every chat turn runs through an orchestrator state machine:

1. **Intent agent** classifies the message: `MEAL_RECOMMENDATION`,
   `CLARIFY_NEEDED`, `MEAL_ADJUST`, `MEAL_PLAN`, `HEALTH_RISK`, or `OTHER`.
2. **Slot filling** extracts a `SlotBundle` from the message and session
   history across seven dimensions: `mealTime`, `mood`, `scene`, `healthGoal`,
   `cuisine`, `taste`, `convenience`.
3. **Clarify agent** decides `ASK` (question the user) or `READY` (recommend).
4. **Recommend / plan / risk services** search the personal or public meal
   library, rank candidates by slot overlap score, and shape the reply.
5. **Response agent** turns the ranked result into natural speech plus display
   blocks (meal cards).
6. **Tracing** records every step (prompts, outputs, latency, tokens) into
   `diet_request_trace`; the evaluation pipeline scores traces with rule
   metrics, an optional LLM judge, and user feedback.

## Repository layout

```
.
├── main.go                  # wiring, config load, graceful shutdown
├── internal/
│   ├── api/                 # yukino_http routes, CORS, error adapter, static
│   ├── config/              # YAML config with env overrides
│   └── diet/                # domain: agents, orchestrator, store, services
│       └── prompts/         # embedded English prompt templates
├── db/diet_db.sql           # schema + seed data (slot options, public meals)
├── frontend/                # React + Vite + shadcn/ui app
├── config.example.yaml      # configuration template
└── config.yaml              # local configuration (git-ignored)
```

## Quick start

### 1. Database

```bash
mysql -u root -p < db/diet_db.sql
```

This creates `diet_db` with the schema, 93 slot options, and seeded public
meals.

### 2. Backend

```bash
cp config.example.yaml config.yaml   # adjust host/port if needed
cp .env.example .env                 # fill in LLM_API_KEY and MYSQL_PASSWORD
go run .
```

The API listens on `http://127.0.0.1:8080` by default. `.env` is loaded
automatically at startup (real environment variables always win), and the
`*_env` fields in the config name the variables that supply secrets and
endpoint overrides, so they never live in the YAML file.

The LLM client speaks the OpenAI-compatible `chat/completions` protocol, so
any compatible provider works — set `LLM_BASE_URL`, `LLM_API_KEY`, and the
model names in `.env`. With an empty `LLM_BASE_URL` it defaults to DashScope
compatible mode.

### 3. Frontend

```bash
cd frontend
pnpm install
pnpm dev        # http://localhost:5173, proxies /api to 127.0.0.1:8080
```

For production, `pnpm build` outputs `frontend/dist`, which the Go server
serves at `/` automatically when `static.dir` points at it.

### Verify

```bash
go build ./... && go vet ./... && go test ./...
curl http://127.0.0.1:8080/healthz
```

## API

Base path `/api/v1/diet`. All requests accept the `X-User-Id` header
(defaults to `1`; a present but malformed value is rejected with `400`).
Bodies and responses are JSON with camelCase keys. Request bodies are capped
at 1 MiB.

| Method | Path | Description |
| --- | --- | --- |
| POST | `/chat` | Run one orchestrator turn (session auto-created when `sessionId` is null) |
| POST | `/sessions` | Create an empty session |
| GET | `/meals/personal` | List the caller's personal meals |
| POST | `/meals/personal` | Create a personal meal |
| PUT | `/meals/personal/:mealId` | Update a personal meal |
| DELETE | `/meals/personal/:mealId` | Delete a personal meal (204) |
| GET | `/meals/public` | List public meals |
| GET | `/slot-options` | Slot dictionary keyed by dimension |
| POST | `/feedback` | Save `LIKE` / `ADOPT` / `DISLIKE` feedback (204) |
| POST | `/evaluations` | Score traces in a time window (rule + optional LLM judge) |
| GET | `/debug/traces` | List traces (`startAt`, `endAt`, `onlyUnlabeled`, `limit`) |
| GET | `/debug/traces/:traceId` | One trace with its full event document |
| GET | `/debug/sessions/:sessionId/traces` | Traces of one session |
| PUT | `/debug/traces/:traceId/label` | Save ground-truth labels for evaluation |
| GET | `/healthz` | Liveness probe |

Business errors render as `400 {"message": "..."}`; unexpected failures render
as `500`.

## Frontend

- **Stack**: React 19, Vite 7, TypeScript (strict), Tailwind CSS 4, shadcn/ui
  components, lucide-react icons, sonner toasts.
- **Data**: TanStack Query for server state, zod for response validation,
  TanStack Virtual for the trace event list, TanStack Store for preferences
  (theme, sidebar), zustand for chat state, ahooks for persisted user id.
- **Pages**: Dashboard, Chat (personal/public source toggle, quick prompts,
  per-meal feedback, trace links), Personal Meals (CRUD dialog with slot
  pickers), Public Meals (filterable grid), Traces (filter bar, virtualized
  event inspector, labeling form), Evaluations (range runner with summary and
  per-trace tables).
- Light and dark themes, collapsible sidebar, and a switchable `X-User-Id` in
  the top bar for multi-user testing.

## Configuration

See `config.example.yaml` for every knob: server host/port, MySQL connection
pool, LLM endpoint and models, the per-call LLM timeout, session history
depth, and the static asset directory. `DIET_CONFIG` overrides the config
file path.

| Variable (`.env`) | Used by | Purpose |
| --- | --- | --- |
| `LLM_API_KEY` | `llm.api_key_env` | Bearer key for the OpenAI-compatible endpoint |
| `LLM_BASE_URL` | `llm.base_url_env` | Endpoint base, e.g. an internal gateway; empty = DashScope default |
| `LLM_MAIN_MODEL` | `llm.main_model_env` | Model for recommendation/plan responses |
| `LLM_LIGHT_MODEL` | `llm.light_model_env` | Model for intent, clarify, and judging |
| `MYSQL_PASSWORD` | `database.password_env` | MySQL password |
