<div align="center">

# time_wheel

**Efficient delayed scheduling — in memory, and across a cluster.**

Two scheduling primitives in one module: a classic in-process **timing wheel** for millions of local timers, and a **Redis-backed delay queue** that fires HTTP callbacks at second-level precision.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-components%2Ftime__wheel-blue)](go.mod)

</div>

---

## Why a timing wheel?

A heap of `time.Timer`s costs `O(log n)` per insert and gives you one goroutine (or one timer) per task. A timing wheel buckets tasks by slot and advances a cursor each tick — inserts are `O(1)`, and a single goroutine drives every task.

```text
slot:   [0]   [1]   [2]   [3]   [4]   [5]   [6]   [7]   [8]   [9]
              ^
           curSlot ──> tick every `interval`

task scheduled at now + 3*interval     -> slot 3, cycle 0
task scheduled at now + 12*interval    -> slot 2, cycle 1
```

Tasks that wrap past the end of the wheel carry a `cycle` counter and only fire when the cursor reaches them on the correct revolution.

## Features

### `TimeWheel` — in-process

- **O(1) insert and remove** via a slot array of linked lists, plus a `key → element` index for cancellation.
- **Cycle-aware scheduling** — `executeAt` is converted to `(pos, cycle)` so arbitrarily far-future tasks work with a small wheel.
- **Panic-isolated tasks** — the driver goroutine recovers and logs instead of dying, keeping the wheel alive.
- **Idempotent `Stop`** — guarded by `sync.Once`; stops the ticker and closes the run loop.

### `RedisDelayQueue` — distributed

- **Minute-sharded Redis zsets** — tasks land in `yukino_time_wheel_task_{minute}` scored by their execution-time unix seconds. Old shards age out naturally.
- **Lua-atomic add / remove / pop** — adding clears any delete marker, removing sets one, and popping returns due tasks _and_ removes them in a single script, so multiple delay queue instances do not double-fire.
- **Delete markers with TTL** — cancelled tasks are recorded in a companion set that expires after 120s, which bounds memory while covering clock skew.
- **HTTP callback execution** — each due task is a `RedisDelayTask` describing an HTTP request; tasks run concurrently with panic isolation.

## Install

```bash
go get github.com/hangtiancheng/yukino.go/components/time_wheel
```

## Quick start

### In-process wheel

```go
wheel := time_wheel.NewTimeWheel(10, 500*time.Millisecond) // 10 slots, 500ms per tick
defer wheel.Stop()

wheel.AddTask("send-reminder", func() {
	log.Println("reminder fired")
}, time.Now().Add(3*time.Second))

// cancel before it fires
wheel.RemoveTask("send-reminder")
```

### Redis delay queue

```go
client := redis.NewClient("tcp", "localhost:6379", "")

delayQueue := time_wheel.NewRedisDelayQueue(client, time_wheel_http.NewClient())
defer delayQueue.Stop()

ctx := context.Background()

err := delayQueue.AddTask(ctx, "order-42-timeout", &time_wheel.RedisDelayTask{
	CallbackURL: "https://internal.example.com/orders/42/timeout",
	Method:      http.MethodPost,
	Req:         map[string]any{"orderID": 42},
	Header:      map[string]string{"X-Source": "time-wheel"},
}, time.Now().Add(30*time.Minute))
```

```go
// cancel the task before it fires
err = delayQueue.RemoveTask(ctx, "order-42-timeout", time.Now().Add(30*time.Minute))
```

> [!IMPORTANT]
> `RemoveTask` on `RedisDelayQueue` needs the **same `executeAt`** used in `AddTask`, because the delete marker lives in the minute shard for that instant. Store the scheduled time alongside the task key.

## API

### `TimeWheel`

| Method                            | Description                                                     |
| --------------------------------- | --------------------------------------------------------------- |
| `NewTimeWheel(slotNum, interval)` | Build and start the wheel. Defaults: `10` slots, `1s` interval. |
| `AddTask(key, task, executeAt)`   | Schedule `task` under `key`.                                    |
| `RemoveTask(key)`                 | Cancel by key.                                                  |
| `Stop()`                          | Stop the wheel.                                                 |

### `RedisDelayQueue`

| Method                                   | Description                                |
| ---------------------------------------- | ------------------------------------------ |
| `NewRedisDelayQueue(redisClient, httpClient)` | Build and start the delay queue.            |
| `AddTask(ctx, key, task, executeAt)`     | Validate and enqueue a callback task.      |
| `RemoveTask(ctx, key, executeAt)`        | Mark the task deleted in its minute shard. |
| `Stop()`                                 | Stop the delay queue.                      |

`RedisDelayTask` fields: `Key`, `CallbackURL`, `Method` (`GET`/`POST` only), `Req`, `Header`.

### Lua scripts

The `LuaAddTasks`, `LuaDeleteTask`, and `LuaZrangeTasks` constants encode the atomic operations. `LuaZrangeTasks` is the important one: it reads the delete set, pops due tasks, and removes them from the zset in one round trip — the property that makes the delay queue safe to run on multiple instances.

## Testing

```bash
go test ./...
```

The local-wheel test runs without dependencies. `Test_RedisDelayQueue` needs a reachable Redis and a callback endpoint — fill in the constants in `time_wheel_test.go`.

## Layout

```
time_wheel/
├── time_wheel.go        # in-process timing wheel
├── redis_delay_queue.go # Redis-backed delay queue
├── time_wheel_lua.go    # atomic Lua scripts
└── pkg/
    ├── http/            # small JSON HTTP client used for callbacks
    ├── redis/           # minimal Redis client surface
    └── util/            # time formatting helpers
```

## License

[MIT](../../LICENSE) © hangtiancheng
