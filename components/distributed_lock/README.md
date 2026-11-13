<div align="center">

# distributed_lock

**Distributed locking with an ownership-checked watchdog.**

A Redis-backed distributed lock that is safe by construction: token-owned keys, Lua-checked release and renewal, an automatic watchdog for long critical sections, and a multi-node RedLock for when a single Redis is not enough.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-components%2Fdistributed__lock-blue)](go.mod)

</div>

---

## Design

A lock is a Redis key holding a unique **token** (24 random bytes, hex encoded). Every mutating operation is a Lua script that compares the stored token first, so a client can never release or renew a lock it does not own — even if its lease expired and someone else acquired it.

```text
Lock()        : SET key token NX EX ttl
Unlock()      : Lua { if get(key) == token then del(key) }       -- atomic
DelayExpire() : Lua { if get(key) == token then expire(key) }    -- atomic
```

## Features

- **Ownership-safe by construction** — acquire, release, and renew all use compare-and-act Lua scripts.
- **Watchdog auto-renewal** — if you do not specify a TTL, the lock starts with a 30-second lease and a watchdog that renews every 10 seconds (setting a 15-second renewal TTL). It renews until `Unlock`, independent of the context passed to `Lock`.
- **Blocking or non-blocking** — `WithBlock()` polls every 50 ms until acquired or the wait budget elapses; without it, `Lock` fails fast with `ErrLockAcquiredByOthers`.
- **RedLock across N nodes** — `RedLock` requires at least 3 distinct Redis instances (duplicate addresses are rejected) and succeeds only when a majority acknowledge within a per-node timeout.
- **Retry-aware errors** — `IsRetryableErr` distinguishes "busy, try again" from real failures.

## Install

```bash
go get github.com/hangtiancheng/yukino.go/components/distributed_lock
```

## Quick start

### Simple lock

```go
client := distributed_lock.NewClient("tcp", "localhost:6379", "")

lock := distributed_lock.NewRedisLock("order:42", client,
	distributed_lock.WithExpireSeconds(30),
)

if err := lock.Lock(ctx); err != nil {
	log.Fatal(err)
}
defer lock.Unlock(ctx)

// critical section
```

Omit `WithExpireSeconds` to enable the watchdog:

```go
lock := distributed_lock.NewRedisLock("order:42", client) // 30s lease, auto-renewed
```

### Blocking lock

```go
lock := distributed_lock.NewRedisLock("order:42", client,
	distributed_lock.WithBlock(),
	distributed_lock.WithBlockWaitingSeconds(10),
)

if err := lock.Lock(ctx); errors.Is(err, distributed_lock.ErrLockAcquiredByOthers) {
	log.Println("gave up after 10s")
}
```

### RedLock

```go
redLock, err := distributed_lock.NewRedLock("order:42", []*distributed_lock.SingleNodeConf{
	{Network: "tcp", Address: "redis-1:6379"},
	{Network: "tcp", Address: "redis-2:6379"},
	{Network: "tcp", Address: "redis-3:6379"},
},
	distributed_lock.WithSingleNodesTimeout(50*time.Millisecond),
	distributed_lock.WithRedLockExpireDuration(30*time.Second),
)
if err != nil {
	log.Fatal(err)
}

if err := redLock.Lock(ctx); err != nil {
	log.Fatal(err)
}
defer redLock.Close()
```

## API

### `Client`

| Method                                           | Description                                  |
| ------------------------------------------------ | -------------------------------------------- |
| `NewClient(network, address, password, opts...)` | Build a go-redis client for the lock.        |
| `NewUniversalClient(redisClient)`                | Wrap an existing go-redis `UniversalClient`. |
| `Close()`                                        | Close the underlying Redis client.           |

| Option                      | Default | Description                         |
| --------------------------- | ------- | ----------------------------------- |
| `WithMaxActive(n)`          | `100`   | Maximum pool size.                  |
| `WithMaxIdle(n)`            | `20`    | Maximum idle connections.           |
| `WithIdleTimeoutSeconds(n)` | `10`    | Idle connection timeout in seconds. |

The `Client` also exposes `Get`, `Set`, `SetNX`, and `Del` helpers for downstream components.

### `RedisLock`

| Method                               | Description                                                         |
| ------------------------------------ | ------------------------------------------------------------------- |
| `NewRedisLock(key, client, opts...)` | Build a lock for `key`. Each `Lock` call uses a fresh random token. |
| `Lock(ctx)`                          | Acquire once, or poll if blocking mode is on.                       |
| `Unlock(ctx)`                        | Release only if the token still matches; stops the watchdog.        |
| `DelayExpire(ctx, seconds)`          | Owner-checked TTL extension.                                        |
| `IsRetryableErr(err)`                | Reports whether `err` is `ErrLockAcquiredByOthers`.                 |

### `LockOptions`

| Option                       | Default          | Description                                    |
| ---------------------------- | ---------------- | ---------------------------------------------- |
| `WithExpireSeconds(n)`       | watchdog (`30s`) | Fixed lease. Setting it disables the watchdog. |
| `WithBlock()`                | off              | Poll until acquired instead of failing fast.   |
| `WithBlockWaitingSeconds(n)` | `5`              | Upper bound on blocking waits.                 |

### `RedLock`

| Method                            | Description                                                         |
| --------------------------------- | ------------------------------------------------------------------- |
| `NewRedLock(key, confs, opts...)` | Requires ≥ 3 distinct nodes; validates the TTL and timeout budget.  |
| `Lock(ctx)`                       | Try all nodes in parallel; succeed when a majority acquire in time. |
| `Unlock(ctx)`                     | Release the nodes acquired by the last successful `Lock`.           |
| `Close()`                         | Unlock and close all underlying clients.                            |

| Option                         | Default | Description                                                        |
| ------------------------------ | ------- | ------------------------------------------------------------------ |
| `WithSingleNodesTimeout(d)`    | `50ms`  | Max time for one node's acquire to count.                          |
| `WithRedLockExpireDuration(d)` | `30s`   | Lock TTL across nodes; must be a positive whole number of seconds. |

## Notes

> [!IMPORTANT]
> `RedisLock` is **not reentrant**. A second `Lock` on the same instance fails with "already held", and releasing from another instance fails the token check. If you need nesting, track it at a layer above the lock.

> [!TIP]
> Prefer the watchdog (omit `WithExpireSeconds`) when the critical section's duration is unknown. A fixed TTL shorter than your work is the classic way to lose mutual exclusion.

> [!WARNING]
> `RedLock` reduces but does not eliminate the risk of split-brain in the presence of clock drift and network partitions. For strong guarantees, coordinate through a consensus system such as etcd or ZooKeeper.

## Testing

```bash
go test ./...
```

Integration tests are skipped unless these environment variables are set:

- `distributed_lock_ADDR` — Redis address for the single-node tests.
- `distributed_lock_PASSWORD` — optional Redis password.
- `distributed_lock_ADDRS` — comma-separated addresses (at least 3) for `Test_redLock`.

## License

[MIT](../../LICENSE) © hangtiancheng
