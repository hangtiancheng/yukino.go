# Taskflow

Conditional and scheduled AI tasks, delivered by a distributed Go monolith. The light-mode workspace uses Lit, light DOM, @yukino.js/lit-jsx, Tailwind CSS, lucide-static, Jotai, @lit-labs/router, and @yukino.js/sentry. The visual design uses a neutral sidebar, white workspace, rounded controls, and compact English labels.

Task definitions contain a prompt and an optional model. The openai/openai-go agent calls MySQL and Redis tools, then writes a Markdown report with execution metadata, evidence, recommendations, and a tool log.

## Run with Docker

```sh
cp .env.example .env
# Set OPENAI_API_KEY, TASKFLOW_API_TOKEN, and TASKFLOW_INTERNAL_TOKEN.
docker compose up --build -d
```

Open http://localhost:8080. When an API token is configured, enter it in the connection dialog. The server API listens on port 8090. MySQL 8.4 and Redis 7 use persistent volumes and remain on the private Compose network. The application can start without an LLM key; model executions will fail until credentials are configured.

For two server instances, a MySQL GTID replica, Redis replication with three Sentinels, and a three-member etcd registry for the distributed report cache:

```sh
docker compose -f docker-compose.yml -f docker-compose.cluster.yml up --build -d
```

The cluster client load-balances API traffic across both server instances. Node journals and trace files use separate volumes. Reports use a shared volume and are also persisted in MySQL, so every node can serve them.

`SERVER_PORT` and `CLIENT_PORT` change the host ports. `MYSQL_PASSWORD` is shared by the provisioned database and server. `REPLICATION_PASSWORD` initializes the replication account. Compose reads `.env`; the Go process reads YAML and the environment variables named by its `*_env` fields.

## Use existing local MySQL and Redis

```sh
cd server
cp conf.example.yml conf.yml
export MYSQL_PASSWORD=pass
export OPENAI_API_KEY=your-key
export OPENAI_BASE_URL=https://api.openai.com/v1
export OPENAI_MODEL=gpt-4o-mini
go run ./cmd/taskflow --config conf.yml
```

The example uses localhost:3306 and localhost:6379 and creates the `taskflow` schema. Change `mysql.database` to select another schema. Startup migrates control tables and installs transactional capture on existing business tables. A condition definition also installs capture on its watched table. Startup needs schema migration and TRIGGER privileges.

```sh
cd client
pnpm install --ignore-workspace --frozen-lockfile
pnpm dev
```

The development UI runs on port 5173 and proxies `/api` to port 8090. `VITE_SERVER_BASE_URL` can change the API origin. `VITE_SENTRY_DSN` changes the client event collector. Production assets are built with `pnpm build` and served by Nginx.

## Included tasks

- **Daily MySQL inspection** runs at 10:00 in Asia/Shanghai. It counts base tables and compares transactional insert/delete events in the previous two daily windows. Window boundaries come from the planned fire time, including when execution was delayed.
- **Insert security audit** analyzes each captured `risk_records` insertion for stored XSS and other persistence risks. Direct SQL inserts are captured, including records subsequently deleted before execution.

Use Risk records to insert a sample or your own payload. The UI renders payloads as escaped text. Scheduled and condition definitions support creation, editing, enabling, disabling, deletion, and explicit manual test runs.

Capture starts when triggers are installed. Reports must state their audited scope and missing historical baseline. `TRUNCATE`, table drops, and foreign-key cascade changes do not produce ordinary row-trigger audit events. See [Operations](docs/operations.md) for schema-change and replication procedures.

## Delivery and idempotency

The durable execution key is unique in MySQL:

| Trigger              | Key                                                      |
| -------------------- | -------------------------------------------------------- |
| Scheduled            | `sched:{taskId}:{plannedUnixSecond}`                     |
| Captured insertion   | `cond:{taskId}:change:{eventUUID}`                       |
| Manual run           | `manual:{taskId}:{SHA256(Idempotency-Key)}`              |
| Explicit sample test | `condtest:{taskId}:{recordId}:{SHA256(Idempotency-Key)}` |

The browser supplies an Idempotency-Key for manual actions. API clients should reuse the same key when retrying an action. Requests without one create a new explicit run. Each committed source insertion is a distinct event; identical records inserted twice are two events.

SQL triggers write a change record in the business transaction. A rollback removes the change record. Concurrent relays use `FOR UPDATE SKIP LOCKED` to create one pending execution and an outbox entry together. The outbox is marked published after Redis accepts the message. A publication crash may redeliver the message.

Handwritten red_mq queues provide at-least-once delivery. Redis claims and a bounded Bloom filter accelerate dispatch. The MySQL unique key and conditional updates are the authority: the executor claims `queued -> running` only for the transaction that owns the row. Repeated messages, callback retries, Redis failover, and concurrent server nodes cannot claim the same execution again while its durable row is retained.

TCC reserves the execution and publishes its command. Its SQL transaction log allows confirm/cancel recovery. Pending executions and queued commands can be recovered after wheel or queue loss. Abandoned Redis pending entries can be reclaimed. A crashed execution already in `running` becomes failed; it is not automatically restarted because an external side effect may already have happened. This is an at-most-once execution claim, not a promise of exactly-once external API side effects.

Prompt/model fields are snapshotted when executions are materialized. Existing planned executions retain their prompt snapshot. Scheduled fires are checked against the current enabled definition and cron before dispatch. Explicit manual runs can test a disabled task. Reports are written using a temporary file, fsync, and rename; the report body and terminal status are stored together with an ownership check.

## Components

| Repository component        | Use                                                                               |
| --------------------------- | --------------------------------------------------------------------------------- |
| libs/yukino_http            | API routing and middleware                                                        |
| libs/yukino_cache           | Byte-budgeted report cache, singleflight, peer discovery and gRPC in cluster mode |
| components/red_mq           | Execution and condition Streams, retry, dead letters, abandoned-message recovery  |
| components/time_wheel       | Distributed scheduled callbacks and local monitor ticks                           |
| components/redis_lock       | Scheduler, monitor, and transaction recovery locks                                |
| components/consistent_hash  | Live-node ring and singleton role placement                                       |
| components/consistent_cache | Redis task-definition cache with invalidation and drift repair                    |
| components/tcc              | SQL-backed reserve/publish transactions and recovery                              |
| components/timer            | Cron parsing, two-bit Bloom precheck, bounded handwritten worker pool             |
| components/lsm_tree         | Persistent node-local execution journal                                           |
| components/raft             | Single-member, node-local ordered decision ledger                                 |

The Raft ledger is diagnostic and node-local. Execution ownership is coordinated through MySQL, not this ledger. Redis standalone, Sentinel, and Cluster clients share the same configured pool across the handwritten components. MySQL reads used for ownership always go to the configured primary; replicas are monitored instead of serving potentially stale ownership decisions.

## Tools and observability

`mysql_tool` accepts one statement. Reads are enabled by default. Set `llm.allow_sql_writes: true` to enable business INSERT/UPDATE/DELETE. Administrative statements, stacked statements, comments, control-table mutations, and locking queries are rejected. Query duration, rows, and returned data are bounded.

`redis_tool` provides get/set/del/exists/ttl/incr/decr/hget/hset/hgetall/keys within `llm.redis_key_prefix`, default `taskflow:tools:`. Listings use bounded SCAN/HSCAN; internal locks, queues, and claims are outside this namespace.

Sentry Go captures execution errors and panics when `SENTRY_DSN` is configured. OpenTelemetry carries W3C context through HTTP, dispatch, MQ, execution, LLM, and tools. The configured file exporter writes JSONL traces. Report frontmatter contains the trace ID. The browser SDK captures errors, performance, page navigation, and annotated clicks; Vite supplies the development collector and the server handles production ingestion.

Cluster monitor shows node membership, execution states, dead letters, report-cache statistics, replica health/lag, and pending change/outbox counts. MySQL-to-Redis status reconciliation uses a durable paginated cursor with a fixed scan boundary and version checks. It repeatedly revisits old rows and prunes old terminal mirror entries; the mirror does not determine execution ownership.

## Validation

```sh
cd server
go test -race ./...
TASKFLOW_INTEGRATION=1 MYSQL_PASSWORD=pass go test -race -tags integration ./internal/engine -count=1
```

Integration tests create and remove unique databases and namespaced Streams in Redis database 15. They cover rollback capture, direct SQL inserts/deletes, concurrent relays, 32 duplicate dispatches/deliveries, timeout finalization, Redis publication outage/recovery, abandoned pending messages, Bloom membership/memory limits, and reconciliation beyond one page. Model protocol tests use an in-process OpenAI-compatible fixture, so these tests do not require a paid API key.

With the server running on port 8090:

```sh
cd client
pnpm exec playwright install chromium
pnpm test
pnpm build
```

Browser tests use a dedicated port and verify navigation, light DOM, mobile width, real form submission, stored-payload escaping, task editing, and repeated manual request keys. The manual-run browser check can make a model request to the configured provider. The live provider and Sentry delivery need configured credentials and endpoints.

Set `TASKFLOW_TEST_URL=http://localhost:8080 pnpm test` to check the built Docker workspace instead of the development server.

See [Architecture](docs/architecture.md) and [Operations](docs/operations.md).
