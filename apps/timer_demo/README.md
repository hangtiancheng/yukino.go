# timer_demo

**A distributed, second-precision timer service.**

Cron-style timer definitions are expanded into concrete tasks ahead of time, fanned out across Redis-sharded time slices, and executed by a horizontally scalable worker fleet — with per-slice distributed locking, deduplication, and Prometheus metrics.

## Architecture

```text
                 ┌─────────────┐
   MySQL ───────>│  Migrator   │  expands cron timers -> task records
   (defs+tasks)  └──────┬──────┘  and warms the Redis task cache
                        │
                        V
                 ┌─────────────┐     per (minute, bucket) lock
                 │  Scheduler  │<──── redis_lock ────┐
                 └──────┬──────┘                     │
                        │ trigger.Work(sliceKey)     │
                        V                            │
                 ┌─────────────┐                     │
                 │   Trigger   │  ZRANGE due tasks ──┘
                 └──────┬──────┘
                        V
                 ┌─────────────┐   HTTP callback    ┌────────────┐
                 │  Executor   │───────────────────>│  target    │
                 └──────┬──────┘                    └────────────┘
                        │ status + bloom filter dedupe
                        V
                 ┌─────────────┐
                 │   Monitor   │  Prometheus metrics
                 └─────^───────┘
                       |
                 ┌─────────────┐
                 │  WebServer  │  timer/task CRUD REST API (yukino_http)
                 └─────────────┘
```

## Roles

| Role          | Responsibility                                                                                                                                                                                     |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Migrator**  | Every `migrateStepMinutes`, evaluates enabled cron expressions and materializes task rows for the upcoming window, then loads them into the Redis cache. Guarded by a per-hour `RedisLock`.        |
| **Scheduler** | Ticks every `tryLockGapMilliSeconds`, iterates `bucketsNum` shards for the current and previous minute, and acquires a per-`(minute, bucket)` distributed lock before handing work to the trigger. |
| **Trigger**   | Pulls due tasks from a slice by second-level score window and dispatches them to the executor pool.                                                                                                |
| **Executor**  | Runs the configured HTTP callback (`GET`/`POST`/`PUT`/`PATCH`/`DELETE`), records the outcome, and sets a daily bloom-filter marker to prevent duplicate execution.                                 |
| **Monitor**   | Reports counts of un-executed tasks (last minute) and enabled timers to Prometheus through a per-minute lock.                                                                                      |
| **WebServer** | REST API for timer definitions and task records, built on `yukino_http`.                                                                                                                           |

## Key design points

- **Pre-materialization** — cron expressions are not evaluated per tick. The migrator expands them into task rows ahead of time, which keeps the hot path a simple sorted-set read.
- **Distributed locking** — every shard of work is protected by a `redis_lock`. Lock TTLs differ between the acquire window (`tryLockSeconds`) and the success lease (`successExpireSeconds`) so slow work is not stolen.
- **Bloom-filter deduplication** — each executed `(timerID, unix)` pair is recorded in a per-day bloom filter with a 24h TTL; a hit falls back to a database status check before skipping.
- **Bounded worker pools** — every role runs through a `pool.WorkerPool`, so a burst of due tasks cannot spawn unbounded goroutines.
- **Dependency injection** — all wiring lives in `app/provider.go` using `go.uber.org/dig`, making the role graph explicit and testable.

## Configuration

Configuration is read from `conf.yml`:

```yaml
mysql:
  dsn: your-dsn

redis:
  address: localhost:6379
  password: ""

scheduler:
  workersNum: 100
  bucketsNum: 20
  tryLockSeconds: 70
  tryLockGapMilliSeconds: 100
  successExpireSeconds: 130

trigger:
  zrangeGapSeconds: 1
  workersNum: 10000

migrator:
  workersNum: 1000
  migrateStepMinutes: 60
  migrateTryLockMinutes: 20
  migrateSuccessExpireMinutes: 120
  timerDetailCacheMinutes: 2

webserver:
  port: 8092
```

All sections except `mysql` and `redis` fall back to defaults when omitted.

## Run

```bash
# 1. edit conf.yml with your MySQL and Redis endpoints
# 2. start
./start.sh
# or
go run .
```

The process starts the migrator, scheduler, monitor, and web server together, and exposes a pprof server on `:9999` and Prometheus metrics for scraping.

## HTTP API

```
/api/timer/v1/def        GET | POST | DELETE | PATCH   timer definition CRUD
/api/timer/v1/defs       GET                           list timers for an app
/api/timer/v1/defsByName GET                           search timers by name
/api/timer/v1/enable     POST                          enable a timer
/api/timer/v1/unable     POST                          disable a timer
/api/task/v1/records     GET                           task execution records
/api/mock/v1/mock        ALL                           echo endpoint for testing callbacks
```

## Layout

```
timer_demo/
├── app/                # dig providers + role bootstrapping
│   ├── migrator/  scheduler/  monitor/  webserver/
├── service/            # role implementations
│   ├── executor/  migrator/  monitor/  scheduler/  trigger/  webserver/
├── dao/                # MySQL (gorm) + Redis task cache
├── common/             # config, models, constants, utils
└── pkg/                # bloom, cron, hash, mysql, redis, pool, xhttp, prometheus
```

## Testing

```bash
go test ./...
```

> [!WARNING]
> This is a reference/demo service. The dynamic bucket-scaling logic is present but disabled (`getValidBucket` returns the static config value), and the schema/index setup is left to the operator.

## License

[MIT](../../LICENSE) © hangtiancheng
