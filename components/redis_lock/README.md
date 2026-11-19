# redis_lock

Ownership-checked Redis leases for Go 1.26+, aligned with
`@yukino.js/redis-lock`. Each acquisition uses a fresh random 24-byte hex token
and the key prefix `DISTRIBUTED_LOCK_PREFIX_`.

```go
client := redis_lock.NewClient("tcp", "localhost:6379", "")
defer client.Close()
lock := redis_lock.NewRedisLock("order:42", client,
    redis_lock.WithExpireSeconds(30),
    redis_lock.WithBlock(),
    redis_lock.WithBlockWaitingSeconds(5),
)
if err := lock.Lock(ctx); err != nil { return err }
defer lock.Unlock(context.Background())
// Finish the critical section while lock.Held() and lock.Remaining() > 0.
```

`Lock` uses SET NX with expiration; `Unlock` and `DelayExpire(ctx, seconds)`
compare the token in Lua before deleting or renewing. Instances are
non-reentrant and must not be copied. `ErrLockAcquiredByOthers` indicates
contention; `IsRetryableErr` recognizes it. `ErrAlreadyHeld` rejects reentry.
`ErrLockLost` reports lost ownership or expired local validity. `Held` and
`Remaining` are conservative local estimates, not Redis ownership queries.

Omitting `WithExpireSeconds` enables a 30-second watchdog. It renews at TTL/3
using the current renewal TTL, including manual TTL changes. It survives
cancellation of the acquisition context and stops on `Unlock`, failed renewal,
or ownership loss. Unlock stops renewal even on transport errors; the token
is retained so cleanup can be retried. A successful ownership-loss response
clears local state so acquisition can be attempted again.

Each command is bounded by its context and a two-second timeout. Blocking
acquisition polls at 50ms intervals with a default total wait of five seconds.
The wait budget includes the initial attempt; cleanup uses a separate bounded
context and may extend the total call duration. The built-in go-redis adapter
disables command retries and enables context deadlines. Custom `LockClient`
implementations must honor deadlines and must not enqueue a command to execute
later after returning an error. Uncertain writes are cleaned with token checks;
cleanup errors are returned and cleanup can be retried.

## Redlock

```go
lock, err := redis_lock.NewRedLock("order:42", []*redis_lock.SingleNodeConf{
    {Network: "tcp", Address: "redis-a:6379"},
    {Network: "tcp", Address: "redis-b:6379"},
    {Network: "tcp", Address: "redis-c:6379"},
}, redis_lock.WithRedLockExpireDuration(30*time.Second),
   redis_lock.WithSingleNodesTimeout(50*time.Millisecond))
if err != nil { return err }
defer lock.Close()
if err := lock.Lock(ctx); err != nil { return err }
// Finish within lock.Remaining(), or renew via DelayExpire(ctx, seconds).
```

At least three independent Redis masters are required. Duplicate addresses are
rejected, including the equivalent empty-network and `tcp` forms.
`NewRedLockWithClients` borrows existing clients and rejects duplicate comparable
client identities; callers must ensure the clients connect to independent
masters. `Close` only closes clients created by `NewRedLock`.

All masters receive the same token concurrently. Acquisition requires a
majority and subtracts the total attempt time and 1% + 2ms clock drift from the
TTL. Node timeouts must be less than TTL/10; the default is 50ms. Expiration
must be a positive whole number of seconds. Failures clean every node,
including ones whose SET response was lost. Cleanup runs concurrently with
fresh bounded contexts, preserves unresolved tokens for retry, and reports
errors. Redlock has no watchdog; `DelayExpire` requires a fresh majority before
the previous validity window expires. `Unlock` is idempotent for Redlock.

The protocol follows [Redis distributed locks](https://redis.io/docs/latest/develop/clients/patterns/distributed-locks/).
It assumes bounded clock drift and critical sections that end before lease
expiry. These packages do not supply storage-enforced fencing or consensus.
JS uses milliseconds, while the Go single-node expiration API uses seconds.

```sh
go test -race -count=1 ./...
redis_lock_ADDR=127.0.0.1:6379 redis_lock_ADDRS=127.0.0.1:6379,127.0.0.1:6380,127.0.0.1:6381 go test -race -count=1 ./...
```
