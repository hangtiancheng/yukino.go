<div align="center">

# redis_lock

**Distributed locking with an ownership-checked watchdog.**

A Redis-backed distributed lock that is safe by construction: token-owned keys, Lua-checked release and renewal, an automatic watchdog for long critical sections, and a multi-node RedLock for when a single Redis is not enough.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-apps%2Fredis__lock-blue)](go.mod)

</div>

---

## Design

A lock is a Redis key holding a unique **token** (`process + goroutine id`). Every mutating operation is a Lua script that compares the stored token first, so a client can never release or renew a lock it does not own — even if its lease expired and someone else acquired it.

```text
Lock()   : SET key token NX EX ttl
Unlock() : Lua { if get(key) == token then del(key) }       -- atomic
Renew()  : Lua { if get(key) == token then expire(key) }    -- atomic
```

## Features

- **Ownership-safe by construction** — acquire, release, and renew all use compare-and-act Lua scripts.
- **Watchdog auto-renewal** — if you do not specify a TTL, the lock starts a watchdog that renews every 10 seconds and a 30-second default lease. Long-running work is protected without guessing a TTL.
- **Blocking or non-blocking** — `WithBlock()` polls every 50 ms until acquired or the wait budget elapses; without it, `Lock` fails fast with `ErrLockAcquiredByOthers`.
- **RedLock across N nodes** — `RedLock` requires at least 3 independent Redis instances and succeeds only when a majority acknowledge within a per-node timeout.
- **Retry-aware errors** — `IsRetryableErr` distinguishes "busy, try again" from real failures.

## Install

```bash
go get github.com/hangtiancheng/yukino.go/apps/redis_lock
```

## Quick start

### Simple lock

```go
client := redis_lock.NewClient("tcp", "localhost:6379", "")

lock := redis_lock.NewRedisLock("order:42", client,
	redis_lock.WithExpireSeconds(30),
)

if err := lock.Lock(ctx); err != nil {
	log.Fatal(err)
}
defer lock.Unlock(ctx)

// critical section
```

Omit `WithExpireSeconds` to enable the watchdog:

```go
lock := redis_lock.NewRedisLock("order:42", client) // 30s lease, auto-renewed
```

### Blocking lock

```go
lock := redis_lock.NewRedisLock("order:42", client,
	redis_lock.WithBlock(),
	redis_lock.WithBlockWaitingSeconds(10),
)

if err := lock.Lock(ctx); errors.Is(err, redis_lock.ErrLockAcquiredByOthers) {
	log.Println("gave up after 10s")
}
```

### RedLock

```go
redLock, err := redis_lock.NewRedLock("order:42", []*redis_lock.SingleNodeConf{
	{Network: "tcp", Address: "redis-1:6379"},
	{Network: "tcp", Address: "redis-2:6379"},
	{Network: "tcp", Address: "redis-3:6379"},
},
	redis_lock.WithSingleNodesTimeout(50*time.Millisecond),
	redis_lock.WithRedLockExpireDuration(30*time.Second),
)
if err != nil {
	log.Fatal(err)
}

if err := redLock.Lock(ctx); err != nil {
	log.Fatal(err)
}
defer redLock.Unlock(ctx)
```

## API

### `RedisLock`

| Method                               | Description                                                        |
| ------------------------------------ | ------------------------------------------------------------------ |
| `NewRedisLock(key, client, opts...)` | Build a lock for `key`. The token identifies the owning goroutine. |
| `Lock(ctx)`                          | Acquire once, or poll if blocking mode is on.                      |
| `Unlock(ctx)`                        | Release only if the token still matches; stops the watchdog.       |
| `DelayExpire(ctx, seconds)`          | Owner-checked TTL extension.                                       |
| `IsRetryableErr(err)`                | Reports whether `err` is `ErrLockAcquiredByOthers`.                |

### `LockOptions`

| Option                       | Default          | Description                                    |
| ---------------------------- | ---------------- | ---------------------------------------------- |
| `WithExpireSeconds(n)`       | watchdog (`30s`) | Fixed lease. Setting it disables the watchdog. |
| `WithBlock()`                | off              | Poll until acquired instead of failing fast.   |
| `WithBlockWaitingSeconds(n)` | `5`              | Upper bound on blocking waits.                 |

### `RedLock`

| Method                            | Description                                        |
| --------------------------------- | -------------------------------------------------- |
| `NewRedLock(key, confs, opts...)` | Requires ≥ 3 nodes; validates the timeout budget.  |
| `Lock(ctx)`                       | Acquire on a majority within the per-node timeout. |
| `Unlock(ctx)`                     | Release on every node.                             |

| Option                         | Default | Description                               |
| ------------------------------ | ------- | ----------------------------------------- |
| `WithSingleNodesTimeout(d)`    | `50ms`  | Max time for one node's acquire to count. |
| `WithRedLockExpireDuration(d)` | `0`     | Lock TTL across nodes.                    |

## Notes

> [!IMPORTANT]
> `RedisLock` is **not reentrant**. Acquiring the same key twice from the same goroutine will fail (or block) because the token is per-goroutine. If you need nesting, track it at a layer above the lock.

> [!TIP]
> Prefer the watchdog (omit `WithExpireSeconds`) when the critical section's duration is unknown. A fixed TTL shorter than your work is the classic way to lose mutual exclusion.

> [!WARNING]
> `RedLock` reduces but does not eliminate the risk of split-brain in the presence of clock drift and network partitions. For strong guarantees, coordinate through a consensus system such as etcd or ZooKeeper.

## Testing

```bash
go test ./...
```

Edit the Redis connection constants in `lock_test.go` before running the integration tests.

## License

[MIT](../../LICENSE) © hangtiancheng
