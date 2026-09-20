<div align="center">

# consistent_hash

**Consistent hashing with data migration built in.**

A Go implementation of a consistent hash ring whose nodes live in Redis, where adding or removing a node automatically tells you _which keys_ must move and _where_. No rebalancing surprises, no thundering herds.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-apps%2Fconsistent__hash-blue)](go.mod)

</div>

---

## Why not just hash `% N`?

Modulo hashing remaps almost every key when a node joins. Consistent hashing limits the disruption to the arc between the new node and its predecessor — but _finding_ that arc and moving its data is usually left to the caller. This package does both: it maintains the ring **and** computes the migration set as a side effect of `AddNode` / `RemoveNode`.

```text
AddNode(node_c, weight=1)
        │
        ├─ weight * replicas virtual nodes hashed onto the ring
        ├─ for each virtual node, find the arc it now owns
        ├─ diff the predecessor's data keys against the new arc
        └─ invoke Migrator(dataKeys, from=predecessor, to=node_c)
```

## Features

- **Weighted nodes** — each node contributes `weight × replicas` virtual nodes (`weight` is clamped to `[1, 10]`, `replicas` defaults to `5`), so capacity-proportional placement and ring smoothness are independent knobs.
- **Automatic migration planning** — `AddNode` / `RemoveNode` return the affected key set through a user-supplied `Migrator`, including wrap-around arcs near `0` / `MaxInt32`.
- **Distributed lock** — mutations acquire a ring-wide lock (`WithLockExpireSeconds`, default `15s`) so concurrent topology changes cannot corrupt the ring.
- **Data-key bookkeeping** — every `GetNode` records `data → node` ownership; that index is exactly what migration diffs against.
- **Two ring backends** — an in-process skiplist ring for tests and single-node use, and a Redis zset ring for a shared, durable ring across a fleet.
- **Pluggable hash** — `Encryptor` is an interface; ship FNV and SHA-1 implementations.

## Install

```bash
go get github.com/hangtiancheng/yukino.go/apps/consistent_hash
```

## Quick start

### Local ring (single process, tests)

```go
ring := local.NewSkiplistHashRing()

ch := consistent_hash.NewConsistentHash(
	ring,
	consistent_hash.NewFnvHasher(),
	func(ctx context.Context, keys map[string]struct{}, from, to string) error {
		log.Printf("migrate %d keys: %s -> %s", len(keys), from, to)
		return nil // do the real move here
	},
	consistent_hash.WithReplicas(5),
	consistent_hash.WithLockExpireSeconds(5),
)

ctx := context.Background()
_ = ch.AddNode(ctx, "node_a", 2) // weight 2
_ = ch.AddNode(ctx, "node_b", 1)

node, err := ch.GetNode(ctx, "user:42")
log.Printf("user:42 -> %s", node)

_ = ch.RemoveNode(ctx, "node_a") // migrator fires with the reclaimed keys
```

### Redis ring (shared across a fleet)

```go
client := redis.NewClient("tcp", addr, password)
ring := redis.NewRedisHashRing("orders:hashring", client)

ch := consistent_hash.NewConsistentHash(ring, consistent_hash.NewFnvHasher(), migrator)
```

## API

### `ConsistentHash`

| Method                         | Description                                                              |
| ------------------------------ | ------------------------------------------------------------------------ |
| `AddNode(ctx, nodeID, weight)` | Add a node, place its virtual nodes, and migrate the keys it takes over. |
| `RemoveNode(ctx, nodeID)`      | Remove a node and migrate its keys to their new owners.                  |
| `GetNode(ctx, dataKey)`        | Resolve the owning node and record the `data → node` mapping.            |

### Options

| Option                     | Default | Description                                                                 |
| -------------------------- | ------- | --------------------------------------------------------------------------- |
| `WithReplicas(n)`          | `5`     | Virtual nodes per weight unit. Higher = smoother distribution, more memory. |
| `WithLockExpireSeconds(n)` | `15`    | Ring-wide lock lease. Set it above your slowest migration.                  |

### Interfaces

```go
// HashRing is the storage-agnostic ring behind the algorithm.
type HashRing interface {
	Lock(ctx context.Context, expireSeconds int) error
	Unlock(ctx context.Context) error
	Add(ctx context.Context, virtualScore int32, nodeID string) error
	Ceiling(ctx context.Context, virtualScore int32) (int32, error)
	Floor(ctx context.Context, virtualScore int32) (int32, error)
	Rem(ctx context.Context, virtualScore int32, nodeID string) error
	Nodes(ctx context.Context) (map[string]int, error)
	// ...replica and data-key bookkeeping
}

// Migrator performs the physical move. Migration tasks run concurrently and
// are panic-isolated, so one bad key cannot abort the batch.
type Migrator func(ctx context.Context, dataKeys map[string]struct{}, from, to string) error
```

## How migration works

1. `AddNode` acquires the global lock and rejects duplicates.
2. For each virtual node it computes `nodeID_i`'s score and inserts it on the ring.
3. `migrateIn` looks at the ring predecessor, reads its tracked data keys, and selects those now falling inside the new node's arc.
4. Ownership indexes are updated atomically with the ring insertion.
5. `RemoveNode` does the mirror image via `migrateOut`, walking successor nodes until it finds a different owner.
6. All migration callbacks are batched and executed concurrently — with `recover()` per task — after the ring state is consistent.

> [!NOTE]
> Wrap-around arcs are handled explicitly. A virtual node with a low score can own the segment that crosses `MaxInt32 → 0`; the code normalizes with signed 32-bit arithmetic instead of assuming a monotonic ring.

## Layout

```
consistent_hash/
├── consistent_hash.go   # ring operations + migration orchestration
├── migration.go         # migrateIn / migrateOut / successor search
├── hash_ring.go         # HashRing interface
├── encryptor.go         # Encryptor interface + FNV / SHA-1
├── option.go            # WithReplicas, WithLockExpireSeconds
├── local/               # in-process skiplist ring
└── redis/               # redis zset ring
```

## Testing

```bash
go test ./...          # local skiplist ring
go test -run Redis     # requires a reachable Redis (edit the constants in example_test.go)
```

## License

[MIT](../../LICENSE) © hangtiancheng
