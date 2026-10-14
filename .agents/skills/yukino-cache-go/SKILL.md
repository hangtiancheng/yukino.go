---
name: yukino-cache-go
description: >
  Distributed groupcache-style cache for Go (module
  github.com/hangtiancheng/yukino.go/libs/yukino_cache, release tag
  libs/yukino_cache/v0.0.2): read-through Group with a byte-budgeted two-level
  LRU store, singleflight dedup, consistent hashing, etcd service discovery,
  gRPC transport, and a WebSocket dashboard. Use when working with Group,
  NewGroup, GetGroup, DestroyGroup, Getter, GetterFunc, ByteView, Cache,
  CacheOptions, Store, StoreOptions, NewStore, Entry, Value,
  SingleFlightGroup, ConsistentHashMap, NewConsistentHash, PeerPicker, Peer,
  ClientPicker, NewClientPicker, Client, NewClient, Server, NewServer,
  Register, DashboardHandler, StartDashboard, WithExpiration, WithPeers,
  WithCacheOptions, WithServiceName, ErrKeyRequired, ErrGroupClosed, HashBKRD,
  ValidPeerAddr, or the pb package (YukinoCacheClient, YukinoCacheServer,
  Request, ResponseForGet, ResponseForDelete); cache topology, peer sync, or
  byte eviction. Do NOT use for groupcache, bigcache, ristretto, or
  Redis/Memcached client code.
---

# yukino_cache

Distributed, groupcache-aligned cache: gRPC transport, etcd discovery, consistent hashing over a fixed-replica ring, singleflight coalescing, a bucketed two-level LRU store with a real byte budget, an optional WebSocket dashboard. Unlike strictly read-through groupcache, `Set`/`Delete` also propagate to the owning peer with best-effort, eventually consistent semantics.

- Module: `github.com/hangtiancheng/yukino.go/libs/yukino_cache`; source root `libs/yukino_cache/`; go directive `go 1.26.0` (`go.mod:3`); release tag `libs/yukino_cache/v0.0.2`
- One flat `yukino_cache` package; `pb/` is the only sub-package (generated code, `package pb`).
- Load `yukino-http` for `dashboard.go`/`DashboardHandler()` work (uses `yukino_http.New`, `Application.Get/Listen`, `Context.Upgrade`, `UpgradeOptions`, `WSConn` `WriteJSON/ReadJSON/Heartbeat/Closed/Close`); `yukino-orm` when a `Getter` reads misses from MongoDB (no compile-time dep); `yukino-rpc` only when wrapping groups in a yukino_rpc service — the cache's own transport is plain gRPC.

## Architecture

```
process-global registry  groups map[string]*Group (groupsMu sync.RWMutex)
                         NewGroup / GetGroup / GetAllGroups / ListGroups / DestroyGroup / DestroyAllGroups
Group (namespace; duplicate name panics): name, getter, expiration (WithExpiration; 0 = no TTL),
closed atomic.Int32, stats groupStats (atomic.Int64), peersMu/peers PeerPicker,
loader *SingleFlightGroup, mainCache *Cache (mu guards store pointer; hits/misses atomic)
  `-- store Store, lazily built on first write -> *lruStore
        |-- locks []sync.Mutex, caches [][2]*cache (L1 CapPerBucket = write entry
        |   point, L2 Level2Cap = read-promotion target), maxBucketBytes = MaxBytes/(mask+1)
        `-- cleanupTick + closeCh + closeOnce (cleanupLoop exits on Close)
ClientPicker: selfAddr (":port" -> "<localIPv4>:port", permanently in ring), svcName,
consHash *ConsistentHashMap, clients map[string]*Client, etcdCli from DefaultRegisterConfig;
watch /services/<svc>/ with snapshot-revision continuation, 1s backoff, full resync on break
Server: embeds pb.UnimplementedYukinoCacheServer (by value); grpc.Server (MaxRecvMsgSize =
opts.MaxMsgSize); health SERVING under svcName; etcdCli from opts.EtcdEndpoints; stopCh +
stopOnce; RPCs resolve GetGroup(req.Group) with the peer-request marker, never re-forwarded
Dashboard: StartDashboard(addr) GET /dashboard/ws (sync.Once); DashboardHandler() mountable;
per connection 2s snapshots + 30s heartbeat
```

## Read and write paths

Read (`Group.Get`): `ErrGroupClosed` when closed, `ErrKeyRequired` on empty key; bump `gets`; a `mainCache.Get` hit bumps `local_hits`; else bump `local_misses` and `load` → `loader.Do(key, fn)`. Callback (leader only): re-check `mainCache.Get` (singleflight dedups overlapping, not serial calls), bump `loads_deduped`, run `loadData`, store the `ByteView` via `AddWithExpiration(key, view, time.Now().Add(expiration))` when `expiration > 0`, else `Add`. `loadData`: with peers registered and a non-peer request, `PickPeer(key)`; remote owner → `peer.Get` (bump `peer_hits`; failure → `peer_misses`, log, fall through); else `getter.Get(ctx, key)` (wrap `failed to get data: %w`, bump `loader_hits`, clone into `ByteView`). `load` adds elapsed ns to `loadDuration`, bumps `loads`, bumps `loader_errors` on failure, and asserts the result to `ByteView` with an `ok` check (foreign type → `unexpected load result type %T`, never a panic).

Write (`Set`/`Delete`): reject closed, empty key, and (`Set` only) empty value (`ErrValueRequired`); update the local cache synchronously (`Set` clones; `Delete` ignores `mainCache.Delete`'s bool); then, for a non-peer-originated request with a `PeerPicker`, spawn `go syncToPeers(peers, op, key, value)`: `PickPeer`, return when `!ok || isSelf || peer == nil`, else forward on `context.WithTimeout(withPeerRequest(context.Background()), 3*time.Second)` (`peerSyncTimeout`). Failures are logged only; both methods have already returned nil.

Consistency — best-effort, eventually consistent; never for strongly consistent state: one fire-and-forget RPC to the single owning peer (3 s timeout, no retry/ack/rollback; the caller never sees a sync failure); non-owner replicas converge only via TTL, so always set `WithExpiration` in a cluster; no read repair (an unreachable owner falls back to the local `Getter` and caches locally); remote peer values cache unconditionally (no hot-key gate), so non-owner replicas are common; topology churn can briefly repoint an owner; peer-originated requests are always answered locally (`isPeerRequest(ctx)` short-circuits `loadData` and `syncToPeers`), so forwarding loops are impossible.

## Value, Entry, ByteView, Getter

```go
type Value interface{ Len() int } // byte size used for budget accounting
type Entry struct {
    Key      string
    Size     int   // value.Len()
    ExpireAt int64 // ns deadline on the coarse clock; math.MaxInt64 = never
    Level    int   // 0 = L1, 1 = L2
}
type ByteView struct{ b []byte }
func (b ByteView) Len() int          // implements Value
func (b ByteView) ByteSlice() []byte // defensive copy on every call
func (b ByteView) String() string    // []byte -> string (copies)
type Getter interface{ Get(ctx context.Context, key string) ([]byte, error) }
type GetterFunc func(ctx context.Context, key string) ([]byte, error)
func (f GetterFunc) Get(ctx context.Context, key string) ([]byte, error)
```

`ByteView` is immutable by construction (`cloneBytes` on every slice-out); there is no exported constructor — callers only receive values from `Group.Get`. Zero value valid (`Len()==0`, `String()==""`, `ByteSlice()` empty non-nil). Every path wrapping caller bytes clones first: `Group.Set`, `loadData`, `getFromPeer`. `Entry` feeds `Cache.Entries`/`Group.Entries`/`Store.Walk` and maps one-to-one to the dashboard JSON `entrySnapshot` (`key`, `size`, `expire_at`, `level`) — those names/types are load bearing on the wire. A getter panic is recovered by singleflight and surfaces as a `Group.Get` error (`TestGroupGetterPanicReturnsError`); a getter returning zero-length bytes with nil error caches as an empty `ByteView` (asymmetric with `Set`, which rejects empty values).

## Group

```go
var ErrKeyRequired   = errors.New("key is required")
var ErrValueRequired = errors.New("value is required")
var ErrGroupClosed   = errors.New("cache group is closed")
// Returned bare (never wrapped): errors.Is and == both work. Loader failures wrap:
// "failed to get data: %w", "failed to get from peer: %w" (before fallback).

type GroupOption func(*Group)
func WithExpiration(d time.Duration) GroupOption     // 0 = no TTL
func WithPeers(peers PeerPicker) GroupOption         // sets g.peers directly
func WithCacheOptions(opts CacheOptions) GroupOption // replaces g.mainCache

func NewGroup(name string, cacheBytes int64, getter Getter, opts ...GroupOption) *Group
func GetGroup(name string) *Group          // nil for unknown names
func GetAllGroups() map[string]*Group      // copied map, shared *Group values
func ListGroups() []string                 // map order, unsorted
func DestroyGroup(name string) bool        // false when not registered
func DestroyAllGroups()
```

`NewGroup` panics `"nil Getter"` when the getter is nil; starts from `DefaultCacheOptions()` with `MaxBytes` overwritten by `cacheBytes`, applies options, panics `"duplicate registration of group " + name` on a taken name (`TestNewGroupPanicsOnDuplicate`); logs creation and calls `StartDashboard(addr)` when `mainCache.opts.DashboardAddr != ""`. Empty name is accepted as the registry key. `WithPeers` runs pre-publication without `peersMu` and counts as the one allowed registration — `RegisterPeers` then panics. `WithCacheOptions` calls `NewCache(opts)`, replacing `mainCache` and discarding the `MaxBytes = cacheBytes` wiring: set `opts.MaxBytes` yourself. `DestroyGroup` deletes under the write lock then closes outside it; `DestroyAllGroups` swaps the map, closes outside the lock (`TestGroupRegistryOperationsDoNotDeadlock`).

| Method           | Signature                               | Notes                                                                |
| ---------------- | --------------------------------------- | -------------------------------------------------------------------- |
| Get              | `(ctx, key) (ByteView, error)`          | Read-through; peer failure logged + fallback, never surfaced         |
| Set              | `(ctx, key string, value []byte) error` | Local write, then async peer sync; `ErrValueRequired` on empty value |
| Delete           | `(ctx, key) error`                      | Local delete, then async peer sync; nil even when the key was absent |
| Clear            | `()`                                    | No-op when closed; clears local cache only, never propagates         |
| Close            | `() error`                              | CAS-idempotent; closes mainCache, unregisters; always nil            |
| RegisterPeers    | `(peers PeerPicker)`                    | At most once; panics `"RegisterPeers called more than once"`         |
| Stats            | `() map[string]any`                     | Keys below                                                           |
| DashboardEnabled | `() bool`                               | mainCache non-nil with non-empty DashboardAddr                       |
| Entries          | `() []Entry`                            | Live local entries; nil when closed or mainCache nil                 |

`Stats` keys: `name`, `closed`, `expiration` (raw `time.Duration`); int64 `gets` (calls past validation), `loads` (Do invocations, leaders + followers), `loads_deduped` (callbacks that ran `loadData`), `local_hits`, `local_misses`, `peer_hits`, `peer_misses`, `loader_hits`, `loader_errors`, `server_requests` (only `Server.Get` bumps it); `hit_rate` float64 only when `local_hits+local_misses > 0`; `avg_load_time_ms` only when `loads > 0`; every `Cache.Stats` key prefixed `cache_`. `Clear` resets cache hit/miss counters but not group counters. Concurrency-safe: `peersMu` guards `peers`, `closed`/stats atomic, `mainCache` and `getter` never reassigned after construction.

## Cache

```go
type CacheOptions struct {
    MaxBytes      int64                         // byte budget; default 8 MiB
    BucketCount   uint16                        // default 16; rounds up to power of 2
    CapPerBucket  uint16                        // L1 entries/bucket; default 512
    Level2Cap     uint16                        // L2 entries/bucket; default 256
    CleanupTime   time.Duration                 // sweep interval; default 1m
    OnEvicted     func(key string, value Value) // default nil
    DashboardAddr string                        // non-empty makes NewGroup start the dashboard
}
func DefaultCacheOptions() CacheOptions // 8*1024*1024, 16, 512, 256, time.Minute, nil, ""
func NewCache(opts CacheOptions) *Cache
```

`NewCache` allocates no store; `ensureInitialized` builds it on first `Add`/`AddWithExpiration` under the write lock (refused when closed; `CleanupTime` → `StoreOptions.CleanupInterval`). Uninitialized caches read as empty. `DashboardAddr` is the only option `Cache` reads outside store construction.

| Method            | Signature                                | Notes                                                      |
| ----------------- | ---------------------------------------- | ---------------------------------------------------------- |
| Add               | `(key string, value ByteView)`           | Log+return when closed; inits store; logs Set errors       |
| AddWithExpiration | `(key, value, expirationTime time.Time)` | `time.Until`; drops entry when already past                |
| Get               | `(ctx, key) (ByteView, bool)`            | `ctx` accepted but unused; miss counted when uninitialized |
| Delete            | `(key string) bool`                      | False when closed/uninitialized                            |
| Clear             | `()`                                     | Write lock; resets hits/misses; no-op when closed/uninit   |
| Len               | `() int`                                 | Deduplicated count across both LRU levels                  |
| Close             | `()`                                     | CAS-idempotent; closes store, nils it, resets initialized  |
| DashboardEnabled  | `() bool`                                | `opts.DashboardAddr != ""`                                 |
| Entries           | `() []Entry`                             | Via `Store.Walk`; nil when closed/uninitialized            |
| Stats             | `() map[string]any`                      | Always `initialized`, `closed`, `hits`, `misses`           |

When initialized, `Stats` also emits `size` (via `Len`), `hit_rate` (`0.0` when hits+misses == 0), and — for the default `*lruStore` — `bytes` (live key+value bytes) and `evictions` (capacity + byte-budget evictions); `TestStatsAlignmentCounters` asserts `cache_bytes`/`cache_evictions` reach `Group.Stats`. Safe for concurrent use including with `Close`: accessors check flags, lock, nil-check the store, so a racing `Close` degrades to a miss (`TestCacheConcurrentUseWithClose`).

## Store / lruStore (two-level LRU + byte budget)

```go
type Store interface {
    Get(key string) (Value, bool)
    Set(key string, value Value) error
    SetWithExpiration(key string, value Value, expiration time.Duration) error
    Delete(key string) bool
    Clear()
    Len() int
    Walk(fn func(Entry) bool)
    Close()
}
type StoreOptions struct {
    MaxBytes        int64 // <= 0 disables byte-budget eviction entirely
    BucketCount, CapPerBucket, Level2Cap uint16
    CleanupInterval time.Duration
    OnEvicted       func(key string, value Value)
}
func NewStoreOptions() StoreOptions // 8 MiB, 16, 512, 256, 1m, nil
func NewStore(opts StoreOptions) *lruStore
func (s *lruStore) Bytes() int64     // live key+value bytes, all buckets
func (s *lruStore) Evictions() int64 // cumulative capacity + byte evictions
```

`NewStore` zero-fills `BucketCount` 16, `CapPerBucket` 512, `Level2Cap` 256, `CleanupInterval` 1m (when `<= 0`) but never defaults `MaxBytes`: zero/negative leaves only entry-count eviction. `*lruStore` is unexported — hold it via `:=` or as `Store`. `Bytes`/`Evictions` take one bucket lock at a time: snapshots, not atomic across buckets.

- Buckets: `mask = MaskOfNextPowOf2(BucketCount)`, `mask+1` buckets — `BucketCount` rounds up to a power of two (3 → 4, 16 → 16). Key → bucket `HashBKRD(key) & mask` (hash may be negative; the AND is still a valid index). One `sync.Mutex` per bucket; only `Bytes`/`Evictions`/`Clear`/`Len`/`Walk` touch more than one. Each level is a fixed node array + intrusive doubly linked list (`Create(cap)`: `cap` slots, `cap+1` link table) — no per-entry allocation after construction.
- Budget: `maxBucketBytes = MaxBytes/(mask+1)`, clamped up to 1 — **per bucket, not global**.
- Write (`Set` → `SetWithExpiration`, which always returns nil; the error exists for alternative implementations): `expireAt = Now() + expiration.Nanoseconds()` or `math.MaxInt64`; `put` into L1 (full L1 recycles the LRU tail; a live tail decrements bytes, counts an eviction, fires `OnEvicted`); `drop` the L2 copy — L1 is the sole write authority or a stale value could resurface after the L1 slot is recycled (`TestLRUStoreSetInvalidatesL2Copy`); then while `L1.bytes+L2.bytes > maxBucketBytes` evict L1-oldest first, then L2 (`TestLRUStoreMaxBytesEviction`).
- Read (`Get`): `del` logically retires the L1 entry (`expireAt = 0`, bytes subtracted). L1 hit expired (`Now() >= expireAt`) → drop both copies, one `OnEvicted`, report miss (`TestLRUStoreExpiredReadFiresOnEvicted`); live → `put` into L2 with the same deadline, return. L1 miss → peek L2: expired → drop + one `OnEvicted` + miss; live → move to L2 MRU, return. So one `Get` moves an entry L1 → L2, the next serves from L2; promotion into a full L2 evicts L2's tail; promotion adds L2 bytes **without** the budget loop (a bucket can transiently exceed its budget until its next write); a retired L1 slot keeps its key in the level's map until an overflowing `put` recycles it (L1 capacity dips under read churn).
- `Delete` retires in both levels, fires `OnEvicted` once (prefers L1's value), true when either held it. `Clear`/`Len`/`Walk` iterate both levels per bucket with map dedup (O(N) per bucket); `Walk` visits L2 first (`Level: 1`), then unseen L1 (`Level: 0`); callback `false` stops only the current level.
- Sweep: `cleanupLoop` started by `NewStore` deletes keys with `expireAt <= Now()` from both levels each `CleanupInterval` tick; `Close` (under `closeOnce`) stops the ticker and closes `closeCh`; repeat-safe.
- Clock: `Now() int64` reads an atomic coarse clock; a goroutine from package `init()` stores the real time, adds 100 ms nine times (100 ms sleeps), sleeps once more, repeats — ~1 s full resync, 100 ms granularity, never stops.
- Exported plumbing helpers: `HashBKRD(s string) int32` (BKDR, `hash*131 + byte`), `MaskOfNextPowOf2(cap uint16) uint16` (`1 → 0, 2 → 1, 3 → 3`), `Now() int64`, `Create(cap uint16) *cache` — `*cache` is unexported, so `Create` is in-package only; don't build APIs on it.

## SingleFlightGroup

```go
type SingleFlightGroup struct{ m sync.Map } // zero value ready to use
func (g *SingleFlightGroup) Do(key string, fn func() (any, error)) (any, error)
```

Followers wait on the leader's `WaitGroup` and receive its exact `(val, err)` — the same `any`, not a copy, so `fn` must not return a mutable value callers will write to. The leader releases the key when `fn` returns, so serial callers re-execute — why `Group.load` re-checks the cache inside the callback. A panic in `fn` → `singleflight: panic during call: %v`, nil value, key released (`TestSingleFlightPanicIsRecovered`). No `Forget`, no `DoChan`, no context: a slow leader blocks followers for the full `fn` duration.

## Distributed layer

```go
type PeerPicker interface {
    PickPeer(key string) (peer Peer, ok bool, self bool)
    Close() error
}
type Peer interface {
    Get(group string, key string) ([]byte, error)
    Set(ctx context.Context, group string, key string, value []byte) error
    Delete(group string, key string) (bool, error)
    Close() error
}
```

`PickPeer` contract: `(nil,false,false)` ring empty / key empty / owner has no client (fallback to local getter, skip sync); `(nil,true,true)` self owns (load locally, no network hop — `TestGroupSelfPickLoadsLocally` — skip sync); `(peer,true,false)` remote owner. Only `Set` takes a context. `*Client` is the sole `Peer` implementation (`var _ Peer = (*Client)(nil)`).

### ClientPicker

```go
type PickerOption func(*ClientPicker)
func WithServiceName(name string) PickerOption // default "yukino_cache"
func NewClientPicker(addr string, opts ...PickerOption) (*ClientPicker, error)
func (p *ClientPicker) PickPeer(key string) (Peer, bool, bool)
func (p *ClientPicker) PrintPeers()
func (p *ClientPicker) Close() error
```

`NewClientPicker` order: (1) `:port` addresses become `<first non-loopback IPv4>:port` via `getLocalIP()` (mirrors registration so self is recognized; failure → `failed to resolve self address: %v`); (2) build with `svcName = "yukino_cache"` and `NewConsistentHash()` (50 replicas, CRC32-IEEE; the picker's ring is not configurable); (3) apply options; (4) create the etcd client from `DefaultRegisterConfig` — no per-picker endpoints, mutate the global first; (5) add `addr` to the ring permanently; (6) synchronous `fetchAllServices` (3 s prefix `Get` on `/services/<svcName>/`) + `go watchServiceChanges()`; seeding failure cancels ctx, closes etcd, returns the error. Discovery loop: each iteration resyncs via `fetchAllServices` (returns the snapshot revision; also closes and deletes clients whose keys vanished from etcd), then `watchOnce` watches the prefix at `WithRev(rev+1)` — no gap between snapshot and watch; channel close / canceled response / `resp.Err()` end the watch, then a 1 s wait (or exit on ctx done) and a full resync — survives etcd restarts and compaction. Events: addresses derive from the etcd **key** via `addrFromEventKey` (strip the prefix), never the value (DELETE events carry empty values — `TestAddrFromEventKey`); `EventTypePut` for unknowns runs `NewClient` → `consHash.Add` + map insert (`NewClient` failure logged, address skipped); `EventTypeDelete` closes and deletes the client; empty and self addresses are skipped. `mu` guards `clients` and ring changes; `handleWatchEvents`/`fetchAllServices` hold the **write** lock across `NewClient`/`Close` (`grpc.NewClient` never blocks on connectivity, but heavy churn serializes against `PickPeer`). `Close` cancels the watch ctx, closes every client and the etcd client under the write lock, aggregating errors into `errors while closing: %v`; it does not await the goroutine's exit. `PrintPeers` logs discovered addresses — not `selfAddr` (in the ring, not in `clients`).

### Client

```go
func NewClient(addr string, svcName string, etcdCli *client_v3.Client) (*Client, error)
func (c *Client) Get(group, key string) ([]byte, error)
func (c *Client) Set(ctx context.Context, group, key string, value []byte) error
func (c *Client) Delete(group, key string) (bool, error)
func (c *Client) Close() error
```

`NewClient` = `grpc.NewClient(addr, insecure.NewCredentials(), grpc.WithDefaultCallOptions(grpc.WaitForReady(true)))` — lazy dial, no reachability check, so it essentially only fails on a malformed target (`failed to create grpc client: %v`). `svcName`/`etcdCli` are stored and never read; `nil` `etcdCli` is fine (tests do it). `Get`/`Delete` build their own 3 s `context.Background()` timeouts and discard the caller's; only `Set` honors its `ctx` (bounded by `syncToPeers`). Errors wrap with `%v`, not `%w` (`failed to get value from yukino_cache: %v` + set/delete variants) — `status.FromError` won't recover codes. `Get` returns `resp.GetValue()` (nil value → nil slice, not an error); `Delete` returns `resp.GetValue()` bool; `Close` closes only the conn, nil when `conn` is nil. No TLS, no retry, no circuit breaker; `WaitForReady(true)` makes a down peer block until the deadline instead of failing fast.

### Server

```go
type ServerOptions struct {
    EtcdEndpoints []string
    DialTimeout   time.Duration
    MaxMsgSize    int // becomes grpc.MaxRecvMsgSize only
}
var DefaultServerOptions = &ServerOptions{
    EtcdEndpoints: []string{"localhost:2379"}, DialTimeout: 5 * time.Second, MaxMsgSize: 4 << 20,
}
type ServerOption func(*ServerOptions)
func WithEtcdEndpoints(endpoints []string) ServerOption
func WithDialTimeout(timeout time.Duration) ServerOption

func NewServer(addr, svcName string, opts ...ServerOption) (*Server, error)
func (s *Server) Start() error
func (s *Server) Stop()
func (s *Server) Get(ctx context.Context, req *pb.Request) (*pb.ResponseForGet, error)
func (s *Server) Set(ctx context.Context, req *pb.Request) (*pb.ResponseForGet, error)
func (s *Server) Delete(ctx context.Context, req *pb.Request) (*pb.ResponseForDelete, error)
```

`Server` embeds `pb.UnimplementedYukinoCacheServer` by value. `NewServer` copies `*DefaultServerOptions` by value before applying options (mutating the global first changes every later server's baseline); etcd client failure → `failed to create etcd client: %v`; builds `grpc.NewServer(grpc.MaxRecvMsgSize(options.MaxMsgSize))` — the send limit stays at the gRPC default (4 MiB), capping `Get` responses; registers the pb service plus a `health.Server` (`svcName` = `SERVING`). No TLS fields exist. `Start` listens (`failed to listen: %v`), spawns `registerWithConfig` with the server's own endpoints/timeout (`WithEtcdEndpoints` honored), and blocks in `Serve`; registration failures are logged (`failed to register service: %v`), never returned — a node can serve while absent from etcd. `Stop` (`sync.Once`): health → `NOT_SERVING` (drain), close `stopCh` (lease revoked), `GracefulStop`, close the etcd client; `Serve` then returns nil. Handlers: all resolve `GetGroup(req.Group)`, else `fmt.Errorf("group %s not found", req.Group)` (the group must exist in the receiving process). `Get` bumps `server_requests`, calls `group.Get(withPeerRequest(ctx), req.Key)` (local getter, no re-forward — `TestPeerOriginatedGetDoesNotForward`), returns `&pb.ResponseForGet{Value: view.ByteSlice()}`. `Set` marks the context (`if !isPeerRequest(ctx)`) before `group.Set` (no re-broadcast) and echoes `req.Value`. `Delete` returns `(&pb.ResponseForDelete{Value: err == nil}, err)` — gRPC keeps only the status when `err != nil`. Handlers touch no `Server` state beyond the registry, so a zero-value `&Server{}` answers RPCs (used by `group_test.go` and `regression_test.go`).

### ConsistentHash

```go
type ConHashConfig struct {
    DefaultReplicas int // virtual nodes per physical node
    HashFunc        func(data []byte) uint32
    MinReplicas          int
    MaxReplicas          int
    LoadBalanceThreshold float64
}
var DefaultConHashConfig = &ConHashConfig{DefaultReplicas: 50, MinReplicas: 10,
    MaxReplicas: 200, HashFunc: crc32.ChecksumIEEE, LoadBalanceThreshold: 0.25}
type ConHashOption func(*ConsistentHashMap)
func WithConsistentHashConfig(config *ConHashConfig) ConHashOption // nil ignored
func NewConsistentHash(opts ...ConHashOption) *ConsistentHashMap
func (m *ConsistentHashMap) Add(nodes ...string) error
func (m *ConsistentHashMap) Remove(node string) error
func (m *ConsistentHashMap) Get(key string) string
func (m *ConsistentHashMap) GetStats() map[string]float64
```

Stable fixed-replica ring (groupcache-style ownership): no goroutine, no `Close` needed. Only `DefaultReplicas` and `HashFunc` affect behavior — no code path reads `MinReplicas`, `MaxReplicas`, `LoadBalanceThreshold`. `NewConsistentHash` stores the shared `DefaultConHashConfig` **pointer**, so mutating the global retroactively changes existing rings. Virtual nodes: `HashFunc(fmt.Appendf(nil, "%s-%d", node, i))`, `i` in `[0, DefaultReplicas)`. `Add`: `errors.New("no nodes provided")` with zero args; skips empty names; no-op for a present node (`TestMapConcurrentAccess`); skips taken virtual-node hashes (no ownership stealing); re-sorts `keys` once. `Remove`: `errors.New("invalid node")` (empty), `fmt.Errorf("node %s not found", node)` (unknown); deletes exactly the node's recorded hashes and filters `keys` in place. `Get`: `""` for empty key or ring; binary search for the first hash `>= HashFunc(key)`, wrap to index 0; read lock only; bumps the node's counter and `totalRequests` atomically. `GetStats`: `map[node]share` (counter/total), empty before the first `Get`; counters never reset and `Remove` deletes a counter without touching `totalRequests`, so shares do not sum to 1 after removals. Hazards: nil `HashFunc` panics on the first `Get`; `DefaultReplicas <= 0` places no virtual nodes, so `Get` always returns `""`; neither is validated. `mu` guards `config`, `keys`, `hashMap`, `nodeHashes`, `nodeCounts` (write for Add/Remove, read for Get/GetStats).

### etcd registration

```go
type RegisterConfig struct {
    Endpoints   []string
    DialTimeout time.Duration
}
var DefaultRegisterConfig = &RegisterConfig{
    Endpoints: []string{"localhost:2379"}, DialTimeout: 5 * time.Second}
func Register(svcName, addr string, stopCh <-chan error) error
```

`Register` delegates to internal `registerWithConfig` with `DefaultRegisterConfig`; `Server.Start` calls it with the server's own config, so manual `Register` is for custom setups only. It returns `errors.New("empty address")` on empty `addr`, creates its own etcd client (a node ends up with three: registration, `Server.etcdCli`, `ClientPicker.etcdCli`), rewrites `:port` to `<first non-loopback IPv4>:port`, grants a 10 s lease (5 s ctx), puts `/services/<svcName>/<addr>` (value = the address) under the lease, and keeps it alive on `context.Background()`. The owner goroutine (closes its etcd client on exit) selects: `stopCh` → revoke (3 s timeout) + return; keepalive channel closed → `reRegister` retries `registerLease` with backoff from 1 s, doubling while below 30 s, until success or `stopCh` closes — nodes rejoin after an etcd hiccup or lease expiry. `stopCh` is `<-chan error`; close (or send) triggers shutdown; `Server` uses an unbuffered `chan error` it only closes. The exported `Register` always reads the global; only the server path honors `WithEtcdEndpoints`.

## Dashboard

```go
func StartDashboard(addr string)
func DashboardHandler() func(ctx *yukino_http.Context, next func())
```

`StartDashboard`: package-level `sync.Once` — first call builds a `yukino_http` app, registers `GET /dashboard/ws` with `DashboardHandler()`, serves in a goroutine; later calls (any address) do nothing and log nothing; no shutdown, the listener lives for the process. `NewGroup` calls it automatically when `CacheOptions.DashboardAddr` is non-empty. `DashboardHandler` upgrades via `ctx.Upgrade(&yukino_http.UpgradeOptions{ReadBufferSize: 4096, WriteBufferSize: 4096})` (logs/returns on failure) and never calls `next()` — mountable on an existing app to share a port. Per connection: 30 s `ws.Heartbeat`; writer sends one snapshot immediately then every 2 s (exit on write error or `ws.Closed()` → stop heartbeat, close); reader loops `ws.ReadJSON(&cmd)`.

Snapshot JSON: `{"type":"snapshot","groups":[{"name":"scores","stats":{...},"entries":[{"key":"player:42","size":16,"expire_at":1234567890,"level":0}]}]}` — `buildSnapshot` walks `GetAllGroups()`, includes only `DashboardEnabled()` groups, embeds full `Stats()` + `Entries()` one-to-one. Command JSON: `{"action":"delete","group":"scores","key":"player:42"}` → `handleCommand` resolves via `GetGroup`, ignores missing/disabled, calls `g.Delete(context.Background(), cmd.Key)` (normal async peer sync applies); unknown actions logged and ignored. No authentication, no origin check.

## pb/ (generated gRPC surface)

`pb/yukino.proto`: `package pb` with `option go_package = "github.com/hangtiancheng/yukino.go/libs/yukino_cache/pb;pb"`; generated files declare `package pb` (`pb/yukino.pb.go:7`, `pb/yukino_grpc.pb.go:7`). Import style: `pb "github.com/hangtiancheng/yukino.go/libs/yukino_cache/pb"`.

```protobuf
message Request           { string group = 1; string key = 2; bytes value = 3; }
message ResponseForGet    { bytes value = 1; }
message ResponseForDelete { bool  value = 1; }
service YukinoCache {
  rpc Get(Request) returns (ResponseForGet);
  rpc Set(Request) returns (ResponseForGet);
  rpc Delete(Request) returns (ResponseForDelete);
}
```

Go surface: `Request{Group, Key, Value []byte}` + `GetGroup()/GetKey()/GetValue()`; `ResponseForGet{Value []byte}` + `GetValue()`; `ResponseForDelete{Value bool}` + `GetValue()`; each message also has `Reset/String/ProtoMessage/ProtoReflect`. `YukinoCacheClient` interface (`Get/Set/Delete(ctx, *Request, ...grpc.CallOption)`) + `NewYukinoCacheClient(grpc.ClientConnInterface)`; `YukinoCacheServer` interface (three methods + unexported `mustEmbedUnimplementedYukinoCacheServer()`); `UnimplementedYukinoCacheServer struct{}` must be embedded **by value** (nil pointer embedding panics inside `RegisterYukinoCacheServer` — `pb/yukino_grpc.pb.go:111`); `UnsafeYukinoCacheServer` omits the must-embed method and is not recommended; `RegisterYukinoCacheServer(grpc.ServiceRegistrar, YukinoCacheServer)`. Also `File_pb_yukino_proto protoreflect.FileDescriptor`, `YukinoCache_ServiceDesc` (ServiceName `pb.YukinoCache`, 3 unary methods, no streams), and `YukinoCache_{Get,Set,Delete}_FullMethodName = "/pb.YukinoCache/{Get,Set,Delete}"`. The client being an interface is what lets `client_test.go` inject `fakeYukinoCacheClient` via `&Client{grpcCli: fake}`. Generated by protoc-gen-go v1.36.4 / protoc-gen-go-grpc v1.5.1 — regenerate with matching versions. All three RPCs share `Request` (`value` matters only for `Set`, which reuses `ResponseForGet`): prefer dedicated messages for new RPCs; carry trace id/tenant/version via gRPC metadata, not the body.

## Internals that affect correctness

Lock scope: `groupsMu` covers only the registry map (destroy paths close outside it — no re-entrant deadlock). `peersMu` covers only `peers`. `Cache.mu` covers the store pointer, not contents (store shards internally). `lruStore.locks[i]` covers bucket `i`'s both levels and counters; no code holds two bucket locks. `ClientPicker.mu` covers `clients` + ring changes; the ring has its own `mu`, so `PickPeer` takes picker-then-ring read locks in a fixed order.

| Goroutine                          | Started by                   | Stopped by                              | Leaks when                                             |
| ---------------------------------- | ---------------------------- | --------------------------------------- | ------------------------------------------------------ |
| coarse clock updater               | package `init()` in `lru.go` | never                                   | always alive; unavoidable, one per process             |
| `lruStore.cleanupLoop`             | `NewStore`                   | `Store.Close` (`closeOnce` + `closeCh`) | you build a `Cache`/store and never `Close` it         |
| `Group.syncToPeers`                | each non-peer `Set`/`Delete` | returns after the RPC or its 3s timeout | never joined; `Group.Close` does not wait for it       |
| `ClientPicker.watchServiceChanges` | `NewClientPicker`            | `ClientPicker.Close` (context cancel)   | you drop the picker without `Close`                    |
| registration keepalive             | `registerWithConfig`         | `stopCh` close (revokes the lease)      | `stopCh` never closes; the etcd client is never closed |
| dashboard listener                 | first `StartDashboard`       | never                                   | always, once started                                   |
| dashboard writer + reader          | each WebSocket connection    | write/read error or `ws.Closed()`       | the peer holds the socket open forever                 |

Per-node release order: `Group.Close()` → `ClientPicker.Close()` → `Server.Stop()`; the dashboard listener and clock goroutine survive. Values clone on every boundary (in: `Set`/`loadData`/`getFromPeer`; out: `ByteSlice`/`Server.Get`) — callers may mutate any `[]byte` handed in or back; retired LRU slots nil their value so memory is collectable while the slot stays allocated. Timeouts: peer reads hard-code 3 s and ignore the caller ctx; peer writes use `syncToPeers`' 3 s ctx (honored by `Client.Set`); singleflight has none, so followers wait for the leader's worst-case 3 s peer RPC plus getter latency; `AddWithExpiration` computes remaining duration on the real clock (`time.Until`) while the store converts against the coarse clock — up to ~100 ms apart. Zero values: `SingleFlightGroup{}` usable; `&Server{}` answers RPCs but `Stop` would panic (nil `stopCh`); a `Cache{}` literal's lazy store gets `NewStore` defaults and no byte budget; `cacheBytes <= 0` disables byte eviction; a peer-less `Group` is a pure local read-through cache.

## Typical usage

Single node:

```go
g := cache.NewGroup("scores", 64<<20, cache.GetterFunc(
    func(ctx context.Context, key string) ([]byte, error) {
        return []byte("score-of-" + key), nil
    },
), cache.WithExpiration(5*time.Minute))
defer g.Close()
view, err := g.Get(ctx, "player:42") // view.String() == "score-of-player:42"
```

Distributed (etcd at a non-default endpoint — start server, then picker, then group; close group → picker → server; `NewClientPicker`/`Register` normalize the same `":port"` string as the server side):

```go
srv, err := cache.NewServer(":9001", "yukino_cache",
    cache.WithEtcdEndpoints([]string{"127.0.0.1:2379"}))
// srv.Start() in a goroutine; it logs (not returns) registration failures.
cache.DefaultRegisterConfig.Endpoints = []string{"127.0.0.1:2379"} // picker reads this global
picker, err := cache.NewClientPicker(":9001", cache.WithServiceName("yukino_cache"))
g := cache.NewGroup("scores", 64<<20, loader, cache.WithExpiration(time.Minute))
g.RegisterPeers(picker)
_ = g.Close(); _ = picker.Close(); srv.Stop()
```

Errors:

```go
switch {
case errors.Is(err, cache.ErrGroupClosed): return nil, err
case errors.Is(err, cache.ErrKeyRequired): return nil, fmt.Errorf("bad request: %w", err)
case err != nil: // "failed to get data: <getter error>"; peer failures never surface
    return nil, err
}
```

`Set` rejects empty values (`ErrValueRequired`); represent absence with `Delete`. Peer sync failures surface only as `[YukinoCache] failed to sync ...` logs or `peer_misses`. Dashboard: set `CacheOptions.DashboardAddr` (`NewGroup` starts the listener on `ws://<addr>/dashboard/ws`; the endpoint takes unauthenticated `delete` commands — bind to loopback or front with auth), or mount `cache.DashboardHandler()` on your own `yukino_http` app — still set `DashboardAddr` so `DashboardEnabled()` holds and the group appears in snapshots.

## Testing patterns

No etcd or live gRPC needed. Harnesses: `fakePeerPicker`/`fakePeer` (`group_test.go`) cover self-ownership, remote reads, peer-failure fallback, write propagation, the peer-request marker; `fakeYukinoCacheClient` (`client_test.go`, implements `pb.YukinoCacheClient`) injects via `&Client{grpcCli: fake}` (nil conn → `Close` returns nil); `testValue` (`lru_test.go`) is the minimal `Value`; `&Server{}` directly answers RPC handlers (`group_test.go`, `regression_test.go`). `regression_test.go` pins core invariants: byte budget, L2 invalidation on `Set`, exactly-once `OnEvicted` on expired reads, panic recovery, duplicate-registration panic, non-forwarding, self-pick local load, in-callback double-check, `addrFromEventKey` parsing, `Cache` close races, stats alignment. `single_flight_test.go` is external (`package yukino_cache_test`, exported API only); the rest is in-package (`ByteView{b:...}`, `group.mainCache`, `group.load`, bucket internals). Always `DestroyAllGroups()` first (global registry, duplicate panic). TTL tests: ≥150 ms TTL with 400 ms sleeps (coarse clock granularity). Run: `cd libs/yukino_cache && go test ./...`.

## Pitfalls

1. `NewGroup` panics on duplicate names (process-global registry, no replace fallback). `Stats`: `hit_rate`/`avg_load_time_ms` keys are omitted until their denominators are nonzero; `Group.Clear` never resets group counters.
2. `WithCacheOptions` replaces `mainCache` — the `cacheBytes` argument is overridden by your `opts.MaxBytes` (8 MiB from `DefaultCacheOptions()`, no budget from a bare `CacheOptions{}`).
3. TTLs under ~200 ms are unreliable (100 ms clock steps, ~1 s resync, real-vs-coarse mixing). `ValidPeerAddr` accepts `localhost:<anything>` or any dotted-quad with any port string; no port-range check, no IPv6; it is called by nothing in the package.
4. Peer writes are one-shot best effort (see Consistency); peer reads ignore caller cancellation and block up to 3 s on a down peer (`WaitForReady(true)`). `Group.Close` never waits for in-flight `syncToPeers` goroutines (unbounded: one per write, each ≤3 s).
5. Forgetting `Close` leaks the store `cleanupLoop` and the picker's etcd watch plus peer connections.
6. Transport is plaintext (insecure creds; no TLS option anywhere).
7. Effective memory ceiling can be far below `MaxBytes`: per-bucket budget, skewed keys, and promotion that bypasses the budget loop until the next write to that bucket.
8. `GetStats` shares don't sum to 1 after any `Remove` (cumulative counters; `totalRequests` not decremented). Custom `ConHashConfig`: nil `HashFunc` panics on first `Get`; `DefaultReplicas <= 0` yields an empty ring; copy `DefaultConHashConfig.HashFunc`.
9. `DefaultConHashConfig`/`DefaultRegisterConfig`/`DefaultServerOptions` are shared mutable pointers — rings keep the pointer (mutating changes existing rings); `NewServer` copies its options struct (only later servers see changes). Mutate once at startup before constructing.
10. `Register` and `NewClientPicker` read the global `DefaultRegisterConfig` only; `WithEtcdEndpoints` affects just `Server.Start`'s registration — with etcd off `localhost:2379` the picker can watch a different cluster than the server registers into.
11. A node creates three etcd clients (registration, server, picker); `NewClient`'s `etcdCli` param is stored but never used — `nil` is fine.
12. `StartDashboard` is a `sync.Once` with no error path and no stop: the first address wins; bind to loopback / add auth (no checks at all).
13. `getLocalIP` picks the first non-loopback IPv4 — may be unreachable on multi-NIC/container hosts; prefer explicit `host:port`.
14. `Server.Start` logs but never returns registration failures — watch for `failed to register service`. `Server.Delete` returns a message plus error; gRPC keeps only the status when the error is non-nil. `MaxMsgSize` only raises `MaxRecvMsgSize`; the send side stays capped at the gRPC default (4 MiB).
15. Empty values: reachable via the read path (getter returning nil/empty caches an empty `ByteView`), rejected on the write path (`Set`). `Group.Delete` discards `Cache.Delete`'s bool; call `Cache.Delete` directly to learn whether the key existed.
16. `ListGroups`/`GetAllGroups` order is map-random — sort before comparing. `Clear`/`Len`/sweeps are O(N) walks under bucket locks, and `Cache.Stats` calls `Len` — don't scrape stats hotly.
17. `Create` (`*cache`) and `NewStore` (`*lruStore`) return unexported types — declare `var s Store = NewStore(opts)`; never build on `Create`.

## File map (`libs/yukino_cache/`)

| File                      | Purpose                                                                                                    |
| ------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `byte_view.go`            | `ByteView`, internal `cloneBytes`                                                                          |
| `store.go`                | `Value`, `Store`, `StoreOptions`, `NewStoreOptions`                                                        |
| `lru.go`                  | `lruStore`/`NewStore`/`Bytes`/`Evictions`, `Entry`, `Now`, `HashBKRD`, `MaskOfNextPowOf2`, `Create`, sweep |
| `cache.go`                | `Cache`, `CacheOptions`, `DefaultCacheOptions`: lazy init, guarded store, stats, idempotent close          |
| `group.go`                | `Group`, `Getter`/`GetterFunc`, options, sentinels, read-through + double-check, async peer sync, registry |
| `single_flight.go`        | `SingleFlightGroup.Do` (`sync.Map`, panic recovery)                                                        |
| `consistent_hash.go`      | `ConsistentHashMap`, `NewConsistentHash`, `ConHashOption`, `WithConsistentHashConfig`                      |
| `config.go`               | `ConHashConfig`, `DefaultConHashConfig`                                                                    |
| `peers.go`                | `PeerPicker`/`Peer`, `ClientPicker`, resilient etcd watch                                                  |
| `client.go`               | `Client` (gRPC `Peer`, insecure, 3 s read/delete timeouts)                                                 |
| `server.go`               | `Server`, `ServerOptions`/options, gRPC + health, `Start`/`Stop`, peer-marker handlers                     |
| `register.go`             | `RegisterConfig`, `DefaultRegisterConfig`, `Register`, lease/keepalive/backoff, `getLocalIP`               |
| `dashboard.go`            | `StartDashboard`, `DashboardHandler`, snapshot/command protocol over `yukino_http`                         |
| `utils.go`                | `ValidPeerAddr`                                                                                            |
| `regression_test.go`      | Invariant pins: byte budget, L2 invalidation, exactly-once `OnEvicted`, panic recovery, stats alignment    |
| `group_test.go`           | Group/Cache/Server/utility tests; `fakePeerPicker`, `fakePeer`                                             |
| `client_test.go`          | `Client` tests via `fakeYukinoCacheClient`                                                                 |
| `lru_test.go`             | Store/LRU tests, `testValue`                                                                               |
| `consistent_hash_test.go` | Ring add/get/remove/stats, concurrent churn                                                                |
| `single_flight_test.go`   | External-package singleflight tests                                                                        |
| `pb/yukino.proto`         | Protocol definition (`package pb`, full-path `go_package`)                                                 |
| `pb/yukino.pb.go`         | Generated `Request`, `ResponseForGet`, `ResponseForDelete`, `File_pb_yukino_proto`                         |
| `pb/yukino_grpc.pb.go`    | Generated stubs, `YukinoCache_ServiceDesc`, method-name constants                                          |

## Dependencies

Direct requires in `libs/yukino_cache/go.mod` (`go 1.26.0`): `github.com/hangtiancheng/yukino.go/libs/yukino_http v0.0.1` (dashboard app/routing/WebSocket; `replace`d to `../yukino_http`; sibling `replace` directives point `yukino_orm`/`yukino_rpc` at local paths even though this module doesn't require them), `google.golang.org/grpc v1.83.1`, `google.golang.org/protobuf v1.36.11`, `go.etcd.io/etcd/client/v3 v3.7.0`. Everything else is `// indirect`: etcd `api/v3` + `client/pkg/v3`, `go.yaml.in/yaml/v3` (transitive — the cache imports no YAML library), `go.uber.org/zap`, OTel, `testify`, `golang.org/x/*`, `genproto`. Stdlib used by the package: `context`, `errors`, `fmt`, `hash/crc32`, `log`, `maps`, `math`, `net`, `sort`, `strings`, `sync`, `sync/atomic`, `time`.
