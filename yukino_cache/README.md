# Yukino Cache

A distributed caching framework written in Go, with an API style aligned to [groupcache](https://github.com/golang/groupcache). yukino_cache retains the elegant namespace model and read-through semantics of groupcache while extending it with write propagation, etcd-based service discovery, consistent hashing with virtual nodes, bucket-sharded dual-level LRU local storage, and a real-time WebSocket dashboard.

Module path: `github.com/hangtiancheng/yukino.go/yukino_cache`

Source directory: `yukino_cache/`

---

## Features

- Namespace-based Group: organizes caches using the `Group + Getter` pattern, consistent with the groupcache developer experience.
- Read-through with singleflight: automatically loads data on local miss while deduplicating concurrent loads for the same key to prevent cache stampede.
- Write propagation: extends the read-only groupcache model with `Set` / `Delete` operations that asynchronously sync to the owning peer via consistent hashing.
- Bucket-sharded dual-level LRU: local storage is partitioned into buckets to reduce lock contention; frequently accessed keys are promoted from L1 to L2 for improved reuse.
- TTL with background cleanup: supports per-key expiration with a background goroutine that periodically scans and evicts expired entries.
- Consistent hashing: virtual nodes with a stable key-to-node mapping (groupcache semantics).
- etcd-based service discovery: the server registers with a lease and keepalive; clients watch etcd to dynamically maintain the peer list.
- gRPC transport: cross-node RPC uses gRPC with health checking.
- Real-time WebSocket dashboard: an optional monitoring endpoint streams live snapshots of all group statistics and cache entries to connected clients.
- Comprehensive statistics: `Group.Stats()` exposes hit rate, average load latency, per-level hits, and more for integration with monitoring systems.
- Explicit lifecycle management: `Group`, `Cache`, `Server`, and `ClientPicker` all provide `Close` / `Stop` methods for safe resource cleanup in tests and graceful shutdown scenarios.

---

## Installation

```bash
go get github.com/hangtiancheng/yukino.go/yukino_cache
```

Dependencies:

- Go 1.21+ (recommended: the version declared in `go.mod`, currently `go 1.26.0`)
- gRPC: `google.golang.org/grpc`
- etcd client: `go.etcd.io/etcd/client/v3` (required for distributed mode; standalone mode does not require etcd)
- yukino_http: `github.com/hangtiancheng/yukino.go/yukino_http` (required only when using the dashboard feature)

---

## Quick Start

### Standalone Mode: Local Cache Only

```go
package main

import (
    "context"
    "fmt"
    "time"

    cache "github.com/hangtiancheng/yukino.go/yukino_cache"
)

func main() {
    ctx := context.Background()

    // 1. Create a named cache group.
    //    - The second parameter is the cache capacity.
    //    - The third parameter is a Getter invoked on local miss.
    scores := cache.NewGroup("scores", 64<<20, cache.GetterFunc(
        func(ctx context.Context, key string) ([]byte, error) {
            // Simulate loading from a database.
            return []byte("score-of-" + key), nil
        },
    ), cache.WithExpiration(5*time.Minute))
    defer scores.Close()

    // 2. Read (first call goes through Getter; subsequent calls hit local cache).
    view, err := scores.Get(ctx, "player:42")
    if err != nil {
        panic(err)
    }
    fmt.Println(view.String())

    // 3. Explicit write.
    _ = scores.Set(ctx, "player:42", []byte("9999"))

    // 4. Explicit delete.
    _ = scores.Delete(ctx, "player:42")

    // 5. Inspect statistics.
    fmt.Printf("stats: %+v\n", scores.Stats())
}
```

### Distributed Mode: Multi-Node with Service Discovery

Each node runs both a gRPC `Server` (serving cache requests) and a `ClientPicker` (discovering other nodes):

```go
package main

import (
    "context"
    "log"
    "time"

    cache "github.com/hangtiancheng/yukino.go/yukino_cache"
)

func main() {
    ctx := context.Background()

    // 1. Start the gRPC cache server on this node.
    srv, err := cache.NewServer(
        ":9001",        // Listen address for this node.
        "yukino_cache",   // etcd service name (must be consistent across all nodes).
        cache.WithEtcdEndpoints([]string{"127.0.0.1:2379"}),
        cache.WithDialTimeout(5*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }
    go func() {
        if err := srv.Start(); err != nil {
            log.Printf("server stopped: %v", err)
        }
    }()
    defer srv.Stop()

    // 2. Create a PeerPicker that discovers other nodes via etcd.
    picker, err := cache.NewClientPicker(
        ":9001",
        cache.WithServiceName("yukino_cache"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer picker.Close()

    // 3. Create a Group with distributed peers.
    g := cache.NewGroup("scores", 64<<20, cache.GetterFunc(
        func(ctx context.Context, key string) ([]byte, error) {
            return []byte("loaded-" + key), nil
        },
    ), cache.WithPeers(picker), cache.WithExpiration(time.Minute))
    defer g.Close()

    // 4. Cross-node operations:
    //    - Get: if the key belongs to another node's partition, it is fetched via gRPC.
    //    - Set/Delete: writes locally first, then asynchronously syncs to the owning peer.
    view, _ := g.Get(ctx, "player:42")
    log.Printf("value=%s", view.String())
}
```

### Dashboard Mode: Real-Time Monitoring

Enable the WebSocket dashboard by configuring `DashboardAddr` in cache options:

```go
cacheOpts := cache.DefaultCacheOptions()
cacheOpts.DashboardAddr = ":8080"

g := cache.NewGroup("scores", 64<<20, getter,
    cache.WithCacheOptions(cacheOpts),
)
```

The dashboard serves a WebSocket endpoint at `/dashboard/ws` that pushes a JSON snapshot of all enabled groups every 2 seconds. It also accepts commands (e.g., deleting a key) from connected clients.

Alternatively, mount the handler on an existing `yukino_http` application:

```go
app.Get("/dashboard/ws", cache.DashboardHandler())
```

---

## Core Concepts

### Group

`Group` is an independent cache namespace holding a `Getter`, a local `Cache`, an optional `PeerPicker`, and a `SingleFlightGroup`. Groups with the same name are globally unique within a process.

### Getter

The data loading callback:

```go
type Getter interface {
    Get(ctx context.Context, key string) ([]byte, error)
}

type GetterFunc func(ctx context.Context, key string) ([]byte, error)
```

Invoked when both local cache and peer caches miss. The returned bytes are cached and exposed to callers as a `ByteView`.

### ByteView

An immutable byte view that protects cached data from external mutation:

```go
view.Len()        // Number of bytes.
view.ByteSlice()  // Defensive copy to prevent caller from corrupting the cache.
view.String()     // Converts to string.
```

### PeerPicker / Peer

`PeerPicker` uses consistent hashing to select the owner node for a given key. `Peer` is the RPC client abstraction for that node. yukino_cache ships with `ClientPicker` (etcd-based service discovery) and `Client` (gRPC implementation).

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

### SingleFlightGroup

Deduplicates concurrent loads for the same key: the first caller executes `fn`, while all other callers block and share the result, preventing cache stampede and redundant origin calls.

### Cache / Store / lruStore

`Cache` is a thin wrapper over `Store` providing lazy initialization, hit/miss statistics, and idempotent close. The default `Store` implementation (`lruStore`) uses bucket-sharded dual-level LRU: entries are placed in L1 on write and promoted to L2 on subsequent access, with each bucket independently locked. A background goroutine periodically scans and evicts expired entries.

### ConHashMap

A consistent hash ring with a fixed number of virtual nodes per physical node (`DefaultReplicas`). The key-to-node mapping only changes when nodes are added or removed, keeping cache ownership stable (groupcache semantics).

---

## API Reference

### Group Registration and Management

```go
func NewGroup(name string, cacheBytes int64, getter Getter, opts ...GroupOption) *Group

func WithExpiration(d time.Duration) GroupOption     // Default TTL; zero means no expiration.
func WithPeers(peers PeerPicker) GroupOption         // Inject distributed peers.
func WithCacheOptions(opts CacheOptions) GroupOption // Override default local cache settings.

func GetGroup(name string) *Group        // Retrieve a registered Group by name.
func GetAllGroups() map[string]*Group    // Snapshot of all registered Groups.
func ListGroups() []string               // List all registered Group names.
func DestroyGroup(name string) bool      // Close and remove a single Group.
func DestroyAllGroups()                  // Close and remove all Groups.
```

If `getter` is nil, `NewGroup` panics. If a Group with the same name already exists, it is replaced with a warning.

### Group Methods

```go
func (g *Group) Get(ctx context.Context, key string) (ByteView, error)
func (g *Group) Set(ctx context.Context, key string, value []byte) error
func (g *Group) Delete(ctx context.Context, key string) error
func (g *Group) Clear()
func (g *Group) Close() error
func (g *Group) RegisterPeers(peers PeerPicker)   // May only be called once.
func (g *Group) Stats() map[string]any    // Hit rate, load latency, per-level hits.
func (g *Group) Entries() []Entry                 // All live entries in local cache.
func (g *Group) DashboardEnabled() bool           // Whether the dashboard is active.
```

Sentinel errors: `ErrKeyRequired`, `ErrValueRequired`, `ErrGroupClosed`.

### Local Cache

```go
type CacheOptions struct {
    MaxBytes      int64                         // Capacity budget.
    BucketCount   uint16                        // Number of shards (rounded up to power of 2).
    CapPerBucket  uint16                        // L1 capacity per shard.
    Level2Cap     uint16                        // L2 capacity per shard.
    CleanupTime   time.Duration                 // Background cleanup interval.
    OnEvicted     func(key string, value Value) // Eviction callback.
    DashboardAddr string                        // If set, starts the WebSocket dashboard.
}

func DefaultCacheOptions() CacheOptions
func NewCache(opts CacheOptions) *Cache
```

### Consistent Hashing

```go
type ConHashConfig struct {
    DefaultReplicas      int                          // Virtual nodes per node (default: 50).
    HashFunc             func(data []byte) uint32     // Hash function (default: crc32.ChecksumIEEE).

    // Deprecated: the auto-rebalancer was removed; these fields are ignored.
    MinReplicas          int
    MaxReplicas          int
    LoadBalanceThreshold float64
}

func NewConHash(opts ...ConHashOption) *ConHashMap
func WithConHashConfig(config *ConHashConfig) ConHashOption

func (m *ConHashMap) Add(nodes ...string) error
func (m *ConHashMap) Remove(node string) error
func (m *ConHashMap) Get(key string) string
func (m *ConHashMap) GetStats() map[string]float64
```

### Server and Client

```go
// Server
func NewServer(addr, svcName string, opts ...ServerOption) (*Server, error)
func WithEtcdEndpoints(endpoints []string) ServerOption
func WithDialTimeout(timeout time.Duration) ServerOption

func (s *Server) Start() error
func (s *Server) Stop()

// Client / Service Discovery
func NewClientPicker(addr string, opts ...PickerOption) (*ClientPicker, error)
func WithServiceName(name string) PickerOption

func (p *ClientPicker) PickPeer(key string) (Peer, bool, bool)
func (p *ClientPicker) PrintPeers()
func (p *ClientPicker) Close() error

// Service Registration (called internally by Server.Start; rarely used directly)
func Register(svcName, addr string, stopCh <-chan error) error
```

`PickPeer` returns a triple `(peer, ok, self)`:

- `ok == false`: no available node on the ring.
- `ok == true && self == true`: the key belongs to this node; the caller should use the local path.
- `ok == true && self == false`: the returned `Peer` is a remote gRPC client.

### SingleFlight

```go
type SingleFlightGroup struct{ /* ... */ }
func (g *SingleFlightGroup) Do(key string, fn func() (any, error)) (any, error)
```

### Dashboard

```go
func StartDashboard(addr string)
func DashboardHandler() func(ctx *yukino_http.Context, next func())
```

`StartDashboard` launches a standalone HTTP server at `addr` with a `/dashboard/ws` endpoint. It is safe to call multiple times; only the first invocation takes effect. `DashboardHandler` returns a middleware-compatible handler for integration with an existing `yukino_http` application.

The WebSocket connection pushes a JSON snapshot every 2 seconds containing group statistics and all cache entries (key, size, expiration, level). Clients can send JSON commands such as `{"action": "delete", "group": "scores", "key": "player:42"}`.

### Utility Functions

```go
func ValidPeerAddr(addr string) bool       // Basic address validation.
func HashBKRD(s string) int32              // BKDR hash for bucket selection.
func MaskOfNextPowOf2(cap uint16) uint16   // Next power-of-two mask.
func Now() int64                           // Coarse-grained internal clock (nanoseconds).
```

---

## Configuration and Defaults

| Configuration                               | Default              | Description                                                   |
| ------------------------------------------- | -------------------- | ------------------------------------------------------------- |
| `CacheOptions.MaxBytes`                     | 8 MiB                | Capacity budget for the local cache.                          |
| `CacheOptions.BucketCount`                  | 16                   | Number of shards (rounded up to the next power of 2).         |
| `CacheOptions.CapPerBucket`                 | 512                  | L1 capacity per shard (number of entries).                    |
| `CacheOptions.Level2Cap`                    | 256                  | L2 capacity per shard (number of entries).                    |
| `CacheOptions.CleanupTime`                  | 1 minute             | Background expired-entry cleanup interval.                    |
| `CacheOptions.DashboardAddr`                | "" (disabled)        | Address to start the WebSocket dashboard server.              |
| `WithExpiration`                            | 0 (no expiration)    | Group-level default TTL.                                      |
| `ConHashConfig.DefaultReplicas`             | 50                   | Initial virtual nodes per physical node.                      |
| `ConHashConfig.MinReplicas` / `MaxReplicas` | -                    | Deprecated: ignored (rebalancer removed).                     |
| `ConHashConfig.LoadBalanceThreshold`        | -                    | Deprecated: ignored (rebalancer removed).                     |
| `ServerOptions.EtcdEndpoints`               | `["localhost:2379"]` | etcd endpoints (must be overridden for production).           |
| `ServerOptions.DialTimeout`                 | 5 seconds            | etcd connection timeout.                                      |
| `ServerOptions.MaxMsgSize`                  | 4 MiB                | gRPC `MaxRecvMsgSize`; responses exceeding this are rejected. |
| `RegisterConfig.Endpoints`                  | `["localhost:2379"]` | etcd endpoints used by the service registration routine.      |
| Service registration lease TTL              | 10 seconds           | Renewed via keepalive.                                        |

---

## Architecture and Request Flow

### Read Path

```
Group.Get(ctx, key)
  -> Local mainCache hit? Yes -> Return ByteView
                           No  -> SingleFlightGroup.Do(key)
                                    -> Peers available? Yes -> PickPeer
                                                                -> Self? Yes -> getter.Get
                                                                        No  -> peer.Get (gRPC)
                                                                                -> Success -> Write local -> Return
                                                                                -> Failure -> Fallback to getter.Get
                                                      No  -> getter.Get
                                    -> Store in local cache (with TTL)
```

### Write Path

```
Group.Set(ctx, key, value)
  -> Write to local cache (with TTL)
  -> No peerRequest marker in ctx? -> Async syncToPeers
                                         -> PickPeer
                                         -> Hits self -> Skip
                                         -> Hits remote -> peer.Set with peerRequest marker to prevent loops
```

`Group.Delete` follows the same structure. The `peerRequest` marker is propagated through a private context key, ensuring the owner node only writes locally and does not forward again.

### Service Discovery

```
Server.Start
  -> net.Listen
  -> goroutine: Register(svcName, addr, stopCh)
       -> etcd Grant 10s lease
       -> Put /services/<svc>/<addr> with lease
       -> KeepAlive for continuous renewal
  -> grpcServer.Serve

ClientPicker
  -> On startup: Get /services/<svc>/ (prefix) to fetch existing nodes -> NewClient + add to ring
  -> Background Watch /services/<svc>/
       -> PUT  -> NewClient + add to ring
       -> DEL  -> client.Close + remove from ring
```

### Dashboard Data Flow

```
StartDashboard(addr) or DashboardHandler()
  -> WebSocket connection established
  -> Every 2 seconds: build snapshot from GetAllGroups()
       -> For each group with DashboardEnabled():
            -> Collect Stats() + Entries()
            -> Serialize as JSON -> Push to client
  -> Incoming commands (e.g., "delete") -> handleCommand -> group.Delete(ctx, key)
```

---

## Comparison with groupcache

| Dimension            | groupcache                   | yukino_cache                                    |
| -------------------- | ---------------------------- | ----------------------------------------------- |
| Namespace model      | `Group + Getter`             | `Group + Getter` (with `context.Context`)       |
| Global registry      | Yes                          | Yes, plus `ListGroups` / `DestroyGroup`         |
| Immutable value type | `ByteView`                   | `ByteView`                                      |
| Stampede prevention  | `singleflight`               | `SingleFlightGroup`                             |
| Consistent hashing   | `consistenthash`             | `ConHashMap` with fixed virtual nodes           |
| Write propagation    | None (read-only)             | `Set` / `Delete` with async peer sync           |
| Peer abstraction     | `PeerPicker` + `ProtoGetter` | `PeerPicker` + `Peer` (Get/Set/Delete)          |
| Transport            | HTTP / Protobuf              | gRPC                                            |
| Service discovery    | Static injection by caller   | Built-in etcd watch                             |
| Local storage        | Single-level LRU             | Bucket-sharded dual-level LRU (L1 + L2)         |
| Monitoring           | None                         | WebSocket dashboard with live stats and entries |

yukino_cache preserves the core API philosophy and mental model of groupcache (`Group + Getter + PeerPicker + singleflight`) while targeting microservice scenarios that require write propagation, automatic service discovery, and operational visibility.

---

## Testing

The repository includes comprehensive unit tests:

```bash
cd yukino_cache
go test ./...
```

Key scenarios covered:

- `group_test.go`: local hit, parameter validation, close semantics, expiration, Set/Delete sync to peer, peer failure fallback to local, `RegisterPeers` double-call panic, singleflight deduplication, `DestroyGroup` / `DestroyAllGroups` deadlock safety.
- `lru_test.go`: basic read/write, capacity eviction, expiration with background cleanup, L1-to-L2 promotion, concurrent read/write safety.
- `con_hash_test.go`: node addition/removal, statistics, `checkAndRebalance` deadlock safety.
- `single_flight_test.go`: deduplication, error propagation, sequential call counting.
- `client_test.go`: success and error paths for Get/Set/Delete using a mock `pb.YukinoCacheClient`.

---

## Directory Structure

```
yukino_cache/
  byte_view.go        # ByteView: immutable byte view with defensive copy
  store.go            # Value / Store interfaces, StoreOptions
  lru.go              # Bucket-sharded dual-level LRU, coarse-grained clock, BKDR hash
  cache.go            # Cache wrapper: lazy init, hit/miss stats, idempotent close
  group.go            # Group, Getter, global registry, read-through / write propagation / stats
  single_flight.go    # Concurrent same-key load deduplication
  con_hash.go         # Consistent hash ring with fixed virtual nodes
  config.go           # ConHashConfig defaults
  peers.go            # PeerPicker / Peer interfaces, ClientPicker (etcd-based discovery)
  server.go           # gRPC server with health check
  client.go           # gRPC Peer client implementation
  register.go         # etcd service registration with lease keepalive
  dashboard.go        # WebSocket dashboard: live stats and entry inspection
  utils.go            # ValidPeerAddr and other utilities
  pb/                 # Protobuf definitions and generated code
  *_test.go           # Unit tests for each module
```

---

## Known Limitations

- `CacheOptions.MaxBytes` is enforced as a byte budget divided evenly across buckets (`MaxBytes / BucketCount` per bucket); per-bucket entry caps (`CapPerBucket`, `Level2Cap`) still apply on top of it.
- `Client` never creates its own etcd client; pass a shared `*clientv3.Client` (as `ClientPicker` does) when constructing clients manually.
- `SingleFlightGroup` recovers panics inside `fn` and returns them as errors to every caller.
- gRPC transport is plaintext: TLS is not supported. Run nodes on a trusted network or add transport security externally.
- `ServerOptions.EtcdEndpoints` defaults to `localhost:2379` and must be overridden for multi-environment deployments.

Contributions via issues and pull requests are welcome.

---

## License

See the `LICENSE` file in the repository root.
