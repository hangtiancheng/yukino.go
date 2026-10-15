---
name: yukino-cache-go
description: >
  Build, modify, or debug the Go module github.com/hangtiancheng/yukino.go/libs/yukino_cache.
  Use for its read-through cache groups, two-level LRU store, consistent hashing,
  etcd peer discovery, gRPC synchronization, and WebSocket dashboard.
---

# yukino_cache

Source root: `libs/yukino_cache/`. Import
`github.com/hangtiancheng/yukino.go/libs/yukino_cache`; generated gRPC types
are in its `pb` subpackage. Read the relevant implementation before changing
behavior; dependency and toolchain requirements come from `go.mod` and the
repository's `go.work`.

## Source guide

| Work                                                        | Read                                                                                                                                                                      |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Group lifecycle, reads, writes, peer forwarding, statistics | [group.go](../../../libs/yukino_cache/group.go), [byte_view.go](../../../libs/yukino_cache/byte_view.go), [single_flight.go](../../../libs/yukino_cache/single_flight.go) |
| Cache options, lazy initialization, storage, expiration     | [cache.go](../../../libs/yukino_cache/cache.go), [store.go](../../../libs/yukino_cache/store.go), [lru.go](../../../libs/yukino_cache/lru.go)                             |
| Peer interfaces, discovery, gRPC client                     | [peers.go](../../../libs/yukino_cache/peers.go), [client.go](../../../libs/yukino_cache/client.go)                                                                        |
| Server, service registration, leases                        | [server.go](../../../libs/yukino_cache/server.go), [register.go](../../../libs/yukino_cache/register.go), [pb/yukino.proto](../../../libs/yukino_cache/pb/yukino.proto)   |
| Ring configuration and node selection                       | [consistent_hash.go](../../../libs/yukino_cache/consistent_hash.go), [config.go](../../../libs/yukino_cache/config.go)                                                    |
| Dashboard endpoint and messages                             | [dashboard.go](../../../libs/yukino_cache/dashboard.go); use `yukino-http` when changing its HTTP or WebSocket handling                                                   |

## Groups and value ownership

- Construct with `NewGroup(name, cacheBytes, getter, opts...)`. A nil
  `Getter` or duplicate name panics. Names belong to a process-wide registry;
  `GetGroup` returns nil for an unknown name.
- `Get(ctx, key) (ByteView, error)` checks the local cache, coalesces misses
  with `SingleFlightGroup.Do`, checks the cache again inside the callback,
  then tries the selected remote owner or the local `Getter`. A peer failure
  is logged and falls back to the getter. Successful results are cached locally.
- `SingleFlightGroup` has a usable zero value. It shares an overlapping call's
  result and error, converts a callback panic to an error, and has no
  context-aware wait or cancellation API.
- `Set(ctx, key, value) error` and `Delete(ctx, key) error` change the local
  cache before starting asynchronous owner synchronization. `Clear()` clears
  only the local cache. `Delete` returns nil for an absent key.
- `Get`, `Set`, and `Delete` return `ErrGroupClosed` after closure and
  `ErrKeyRequired` for an empty key. `Set` also rejects empty values with
  `ErrValueRequired`; a getter may return an empty value without error.
- Local storage clones bytes from `Set`, the getter, and peer reads.
  `ByteView.ByteSlice()` returns a copy; `String()` returns the value as a
  string. The asynchronous `Set` path passes the original input slice to
  `Peer.Set`: do not assume the entire distributed write owns a defensive
  copy, or mutate that slice while synchronization may still use it.
- `WithExpiration(d)` enables expiration when `d > 0`.
  `WithCacheOptions(opts)` replaces the cache, including the byte budget:
  set `opts.MaxBytes` explicitly rather than relying on `cacheBytes`.
- Register peers with `WithPeers(picker)` or `RegisterPeers(picker)`.
  `RegisterPeers` panics if a non-nil picker is already installed.
- `Close() error` is idempotent, closes the group's cache, and unregisters
  the name. It neither closes the picker/server nor joins pending peer writes.
  `DestroyGroup` and `DestroyAllGroups` close groups outside the registry lock.

## Storage semantics

`Cache` stores `ByteView`; `Store` accepts a `Value` with `Len() int`.
`NewCache` creates the store on its first write. `NewStore` starts a cleanup
goroutine immediately; close stores and caches when their owners shut down.

`DefaultCacheOptions()` and `NewStoreOptions()` supply an 8 MiB byte budget,
16 buckets, L1/L2 capacities of 512/256 entries per bucket, and a one-minute
cleanup interval. A bare options struct leaves `MaxBytes <= 0`, disabling
byte-budget eviction; `NewStore` supplies defaults for zero capacities and
a non-positive cleanup interval.

- Buckets round up to a power of two and use `HashBKRD(key) & mask`.
  Each bucket has its own mutex and two LRU levels.
- Writes enter L1 and invalidate the L2 copy. A live L1 read moves the value
  into L2 with the same expiration; L2 reads update recency.
- Byte accounting uses `len(key) + value.Len()`, excluding allocation and
  data-structure overhead. The budget is divided per bucket, with a minimum
  of one byte per bucket when a positive budget is smaller than the bucket
  count. Capacity eviction and bucket skew also affect retained entries.
- Expiration uses `Now()`, an atomic clock advanced in 100 ms steps and
  periodically reset from wall time. Expired values are rejected on reads
  and removed by sweeps. `Len` and `Walk` inspect resident slots and can
  include expired entries that have not yet been read or swept.
- `OnEvicted` also runs for explicit deletion, clearing, and expiration.
  Callbacks run under the bucket lock; avoid re-entering the same store.
  `Walk` also invokes its callback under that lock; returning false stops
  the current level's walk, not the whole store traversal.
- `Entry` contains `Key`, value-only `Size`, nanosecond `ExpireAt`
  (`math.MaxInt64` for no expiration), and `Level` (0 for L1, 1 for L2).
- `Group.Stats()` prefixes cache statistics with `cache_`. Group
  `hit_rate` and `avg_load_time_ms` appear only when their denominators
  are positive. `Clear` resets cache hit/miss counters, not group counters.

## Peers and consistency

`PeerPicker.PickPeer(key)` returns `(peer, ok, self)`: self ownership is
`(nil, true, true)`; no usable owner is `(nil, false, false)`.
`Peer` exposes `Get(group, key)`, `Set(ctx, group, key, value)`,
`Delete(group, key)`, and `Close()`. Only `Set` accepts a context.

- Synchronization sends one best-effort write to the selected remote owner.
  Failures are logged; there is no acknowledgement to the original caller,
  retry queue, or invalidation broadcast. Other cached copies can stay stale.
  Use a TTL when bounded cache lifetime is needed; this is not a
  strongly consistent store.
- The built-in client's `Get` and `Delete` use independent three-second
  background timeouts. `Group.Get` cancellation does not cancel a peer read.
  Asynchronous `Set` gets a three-second background context. A custom
  `Peer.Delete` has no context parameter and is not bounded by that context.
- Server handlers mark requests as peer-originated, preventing another
  ownership lookup or write broadcast. Each receiving process must have
  the requested group registered.
- `NewClientPicker(addr, WithServiceName(name))` defaults to service
  `yukino_cache`. It keeps self in the ring, discovers
  `/services/<service>/<addr>`, resynchronizes snapshots, and watches from
  the following etcd revision. `Close` cancels discovery and closes clients.
- Picker construction and exported `Register` use
  `DefaultRegisterConfig`; server registration uses its own
  `WithEtcdEndpoints`/`WithDialTimeout` options. Configure both paths to
  the same etcd cluster and service name before starting them.
- `:port` is advertised using the first non-loopback IPv4 address.
  Explicit reachable addresses avoid dependence on interface enumeration.
  Registration grants a ten-second lease, keeps it alive, and attempts
  re-registration after keepalive closure. `Server.Start` logs registration
  failures separately from its listener/serve return value.
- `NewServer(addr, service, opts...)` builds the gRPC server;
  `Start() error` serves it and `Stop()` performs graceful shutdown.
  The built-in client uses insecure gRPC credentials.
  `ServerOptions.MaxMsgSize` configures server receive size only.
- The ring uses `DefaultReplicas` and `HashFunc` from `ConHashConfig`;
  `MinReplicas`, `MaxReplicas`, and `LoadBalanceThreshold` are not used
  by its selection algorithm. Configurations are held by pointer.
  `GetStats` uses cumulative selections; removing a node removes its
  counter without subtracting from the total.

## Dashboard

`CacheOptions.DashboardAddr != ""` both includes a group in dashboard
snapshots and makes `NewGroup` call `StartDashboard(addr)`.
The first call starts `GET /dashboard/ws`; a package-level `sync.Once`
prevents additional listeners, and no dashboard shutdown API is exposed.

`DashboardHandler()` can be mounted on a `yukino_http` application.
Mounting alone does not enable groups: the non-empty `DashboardAddr`
condition still applies and also triggers automatic startup during group
creation. Snapshots have `type: "snapshot"` and `groups` containing
`name`, `stats`, and `entries`. The supported command is
`{"action":"delete","group":"scores","key":"player:42"}`.
The handler configures neither authentication nor an origin check.

## Example and verification

```go
package cacheexample

import (
    "context"
    "time"

    cache "github.com/hangtiancheng/yukino.go/libs/yukino_cache"
)

func NewScores(loader cache.Getter) *cache.Group {
    return cache.NewGroup("scores", 64<<20, loader,
        cache.WithExpiration(5*time.Minute))
}

func Score(ctx context.Context, g *cache.Group, key string) (string, error) {
    view, err := g.Get(ctx, key)
    return view.String(), err
}
```

Avoid duplicate live group names and close each group through its owner.
From `libs/yukino_cache`, run `go test ./...`.
Existing tests use fake peers and fake `pb.YukinoCacheClient` implementations;
they do not require etcd. For registry-sensitive tests, use distinct names
and close the groups. TTL assertions must allow for the coarse clock.
