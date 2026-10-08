# Operations

## Configuration and startup

The Go service loads a YAML file and resolves secrets through named environment variables. Compose supplies those variables from `.env`. Keep each Taskflow installation on its own MySQL schema and Redis database/cluster; internal key names identify one deployment.

Use a unique node ID per instance, or the default hostname/process ID. Give every instance a writable report path, trace path, and local journal path. Container images create these paths for the non-root server user. A cluster can serve reports from MySQL even when a local report file is absent.

Set API and internal callback tokens for a network-accessible deployment. Health and readiness checks are public. Other API endpoints require the configured bearer token. Internal callbacks require their token, or loopback access when no token is configured. Place public traffic behind TLS termination. The browser stores its API token for the current tab session.

Readiness checks SQL and Redis connectivity. Cache and replica health are shown separately. An unavailable SQL replica does not prevent primary-backed execution. A configured distributed cache registry must be available for startup discovery.

## MySQL replication

The cluster overlay configures MySQL 8.4 with distinct server IDs, ROW binlogs, GTIDs, and durable commit settings. The initializer snapshots the application schema into an empty replica, sets its GTID position, starts replication, and verifies both threads. It refuses an existing unconfigured replica with application tables. An existing channel is retained on restart.

The replica becomes persistently read-only after initialization. Its health, errors, and lag appear in `/api/v1/monitor/storage`. Replication is native MySQL asynchronous replication. Do not promote a lagging replica: fence the previous primary, verify the intended GTID set is applied, point the service at the promoted primary, and restart the instances. The application deliberately avoids automatic MySQL promotion and divergent dual-primary writes.

Back up source records, execution keys, transaction records, change events, and outbox state together. Deleting execution keys also deletes the durable deduplication history. Audit tables can be archived after the reporting retention policy permits it; never remove unprocessed changes or unpublished outbox entries.

## Redis replication and failover

The cluster overlay supplies one Redis primary, two replicas, and three persistent Sentinels. Clients discover the current primary through Sentinel. Redis uses AOF and noeviction. Health checks accept a promoted replica as a healthy primary. Internal component clients share the same DB, credentials, topology, and connection pool.

For an existing Redis Cluster, configure `redis.mode: cluster`, `redis.addresses`, and database 0. Related time-wheel keys share a hash tag. Definition-cache data and disable markers also hash to the same slot. Ordinary model key operations stay in the tool namespace. Redis Cluster's SCAN is node-scoped; use direct known keys when a complete cross-shard listing matters.

Redis failover can lose an unreplicated Stream entry or fast claim. Pending and queued executions remain in SQL and are recovered by the monitor. A node that dies after claiming running is failed after the stuck-running threshold. It is not restarted automatically.

## Capture coverage and schema changes

Startup installs INSERT/DELETE capture triggers on existing supported business tables in the selected schema, including tables without primary keys. Taskflow control tables are excluded from business watch definitions to prevent recursive task generation. The default `executions` table is audited for inspection but cannot be watched by a condition task.

A new condition definition instruments its watched table. After creating an additional table used only by the daily inspection, restart the service to install its capture. Changes before installation have no audit baseline. Table identifiers must be simple identifiers in the selected schema.

For a source table column change, pause source writes, remove its Taskflow capture triggers, apply the schema change, and restart the service to regenerate the captured JSON layout. Resume writes after both INSERT and DELETE triggers exist. Do not remove capture triggers during normal traffic. Ordinary row triggers do not capture TRUNCATE, DROP, or foreign-key cascade mutations; these operations require separate operational audit evidence and must not be presented as complete row counts.

## Tool permissions and report review

SQL reads are enabled by default; business writes require `llm.allow_sql_writes`. Tools cannot modify execution, task, outbox, transaction, or dead-letter control tables. Use a least-privilege database account for the configured application scope. Startup migrations and trigger installation need their corresponding privileges.

The policy rejects multi-statement SQL, administrative verbs, SQL comments, system-schema access, and locking functions. It is an application policy, not a SQL privilege replacement. Redis model tools operate only under the configured prefix. Do not use that prefix for internal coordination or unrelated secrets.

Prompts instruct the model to treat stored payloads and tool results as untrusted evidence. Model risk verdicts remain evidence-based analyses; reports include matched payloads, limitations, and recommendations. Tool errors, invalid report sections, token exhaustion, and execution deadlines produce failed executions rather than successful partial reports.

## Recovery and monitoring

The configured executor timeout must be shorter than the MQ handler timeout and stuck-running threshold. Keep these budgets aligned when tuning worker counts. Queue entries are bounded; SQL recovery compensates for trimmed or lost pending/queued messages. Abandoned pending entries are reclaimed after a handler-safe idle interval.

Monitor pending change/outbox counts, dead letters, failed executions, SQL replica lag, and Redis replication state. Persistent growth in the outbox indicates publication failure. A terminal execution is never retried by queue redelivery. Explicit manual reruns use a new request key.

Client telemetry is collected at `/api/v1/telemetry/log` by default. Server traces are written to the configured JSONL path, and Sentry Go uses `SENTRY_DSN`. Use external log rotation and retention for persistent trace/event volumes. Verify live model and Sentry endpoints with their own credentials before treating fixture-based tests as live-provider validation.

## Tested scenarios

Automated tests use isolated database names and Redis keys. They exercise transactional rollback, direct SQL changes, concurrent relay/dispatch/consumer calls, outage recovery, abandoned Streams entries, timeout persistence, Bloom behavior, bounded worker lifecycle, and status reconciliation past the first page.

The Docker cluster is validated with two server nodes, replicated MySQL/Redis, distributed report-cache discovery, and a stopped Redis primary followed by Sentinel promotion and server reconnection. These functional tests do not constitute a capacity benchmark or a live-model quality evaluation.
