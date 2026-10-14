---
name: yukino-rpc
description: >
  TCP-based RPC framework for Go (module
  github.com/hangtiancheng/yukino.go/libs/yukino_rpc). Hand-rolled binary
  protocol, not gRPC. Use when writing Go that imports yukino_rpc:
  rpc.Dial/NewServer/NewRegistry,
  ClientConn.Invoke/InvokeAsync/NewStream/Close,
  Server.Register/Serve/GracefulStop/Stop, ServerStream.Send,
  ClientStream.Recv, Future, DialOption (WithTimeout, WithDialCodec,
  WithRegistry, WithLoadBalancer), WithCodec, CodecJSON/CodecProto,
  Registry, Instance, LoadBalancer, pkg/api samples. Wire format: Magic
  0x1234, big-endian HeaderLen/BodyLen, JSON header, mandatory gzip body,
  StreamFlag frames (None/Data/End/Error), per-header codec negotiation,
  RequestID multiplexing over one socket, reflection dispatch (grpc-go and
  net/rpc signatures), server-streaming only, three-state circuit breaker,
  token-bucket rate limiting, RR/random/weighted-RR load balancing,
  connection pooling, graceful shutdown, no heartbeats. Not for grpc-go
  itself, Connect-Go, Twirp, or HTTP/SSE/WebSocket (use yukino-http).
---

# yukino_rpc

`github.com/hangtiancheng/yukino.go/libs/yukino_rpc` is a hand-rolled TCP RPC framework. Current release: git tag `libs/yukino_rpc/v0.0.2`. It does not use gRPC, HTTP/2, protobuf service stubs, or any code generator: it defines its own length-delimited binary frame, its own JSON header, its own request multiplexing, and its own reflection-based server dispatch. The public API deliberately mimics grpc-go naming (`Dial`, `ClientConn`, `Invoke`, `NewStream`, `NewServer`, `Serve`, `GracefulStop`) so call sites read familiarly, but nothing underneath is shared with grpc-go. `google.golang.org/grpc` appears in `go.mod` only as an indirect dependency pulled in by the etcd client; the framework never uses it for transport.

Design: one socket per peer address, every concurrent request correlated by a monotonically increasing `RequestID`; a single reader goroutine per connection demultiplexes responses into futures and stream buffers. Resilience (circuit breaking, load balancing, discovery-driven rate limiting) is wired only into the registry-backed client path; the static single-address path is deliberately bare. There is no application-level heartbeat: idle connections carry nothing, and a dead peer is discovered only when a frame is written or read. Public surface: `pkg/rpc` (client, server, options, aliases) and `pkg/api` (sample arithmetic services). Everything else is under `internal/` and unimportable outside the module, yet its behaviour is user-visible: wire format, framing, codec negotiation, breaker thresholds, balancer selection, lease TTL semantics, stream buffering.

## When to load adjacent skills

- Load `yukino-orm` when RPC handlers persist to MongoDB.
- Load `yukino-cache-go` to memoize RPC replies or back a handler with a distributed cache; yukino_rpc has no cache of its own.
- Load `yukino-http` for HTTP, REST, SSE, or WebSocket; yukino_rpc speaks only its own binary protocol over raw TCP, with no gateway or transcoding. Skip all three for pure protocol, transport, or dispatch questions about yukino_rpc.

## Architecture overview

```
user code -> rpc.Dial(target, opts...) -> rpc.ClientConn (mode: static | registry)
  static:   transport.ConnectionPool(target,0,1) -> TCPClient -> TCPConnection
  registry: internal/client.Client (limiter 10000 -> reg.Discover+lb.Select
            -> breaker(10,0.6,5s) -> per-addr ConnectionPool) -> TCPClient,
            discovery via internal/registry.Registry over etcd v3
  TCPClient: one per socket (lazy 5s dial), atomic seq, pending/streams
  sync.Maps, one readLoop demuxing by StreamFlag. TCPConnection: bufio(4096)
  + PacketBuffer (magic resync), writeMu-serialized Write, SetLinger(0) Close.
  Frames via internal/protocol (Magic 0x1234 | HeaderLen | BodyLen | JSON
  header | body), bodies via internal/codec (JSON=1, PROTO=2, gzip=1).
=== TCP wire ===
internal/server.Server (accept loop; one goroutine per conn; per-server
  TokenBucket(10000); services map) -> Handler (per-header codec selection,
  reflect dispatch, safeCall recover, streams via streamWg.Go) -> serverStream
  (Send->StreamData, end->StreamEnd, sendError->StreamError)
```

Unary call flow:

```
Invoke(ctx, "Math", "Add", args, &reply)
  [registry] limiter.Allow -> Discover+Select -> breaker.Allow (rejections:
  rate limit exceeded / no instance available / load balancer returned empty
  address / circuit breaker open)
  pool.Acquire(ctx bounded by timeout) -> codec.Marshal(args)
  -> TCPClient.SendAsyncWithCodec (seq=nextSeq(), pending[seq]=Future, gzip
  frame, Write) -> Future.GetResultWithContext; readLoop demuxes the StreamNone
  reply into Future.Done; [registry] OnComplete -> breaker; codec.Unmarshal
```

Ownership: static `ClientConn` owns one `ConnectionPool` + one `Codec`. Registry `ClientConn` owns one `internal/client.Client`, which owns one `TokenBucket`, one `LoadBalancer`, one `ConnectionPool` per address (`sync.Map`), one `CircuitBreaker` per `service|addr` (`sync.Map`). Each pool holds at most one `TCPClient` (`maxActive=1` at every call site). Each `TCPClient` owns one `TCPConnection`, one `readLoop`, `pending`, `streams`. `Server` owns one `Handler`, one `TokenBucket`, a `conns` set, the accept goroutine (`serveWg`), one goroutine per connection (`wg`), one per in-flight stream (per-connection `streamWg` declared inside `Handle`). `Registry` owns one etcd client, one cancellable context, one keepalive-drain goroutine per `Register`, one watch goroutine per discovered service; `internal/stream` holds only the two stream interfaces, breaking the would-be import cycle between `internal/server` and `internal/transport` (`pkg/rpc` re-exports them as aliases).

## Core types

`pkg/rpc/rpc.go` aliases (not wrappers — every method of the aliased type is public). `CodecJSON`/`CodecProto` are `var`s typed `codec.Type`, the only codec values a user passes to `WithDialCodec`/`WithCodec`; `codec.Register` lives in `internal/codec`, so no third codec can be added from outside the module:

```go
type CodecType    = codec.Type              // byte; JSON = 1, PROTO = 2
type Registry     = registry.Registry
type Instance     = registry.Instance       // struct { Addr string }
type LoadBalancer = load_balance.LoadBalancer
type ServerStream = stream.ServerStream
type ClientStream = stream.ClientStream
type Future       = transport.Future

var (
    CodecJSON  = codec.JSON   // CodecType(1)
    CodecProto = codec.PROTO  // CodecType(2)
)
```

```go
func NewRegistry(endpoints []string) (*Registry, error)
func NewServer(opts ...ServerOption) *Server
func Dial(target string, opts ...DialOption) (*ClientConn, error)
func WithTimeout(d time.Duration) DialOption
func WithDialCodec(t CodecType) DialOption
func WithRegistry(reg *Registry) DialOption
func WithLoadBalancer(lb LoadBalancer) DialOption
func WithCodec(t CodecType) ServerOption
```

- `NewRegistry` builds an etcd v3 client (`DialTimeout: 5s`) with a hard-coded key prefix. The returned error is a client-configuration error, not a connectivity error: an unreachable etcd cluster still yields a usable `*Registry`, and failures surface later from `Discover`/`Register`.
- `NewServer` panics (never returns an error) on any option error: `"yukino_rpc: invalid server option: " + err.Error()`. The only failure is `WithCodec` with an unregistered type.
- `Dial` performs no network I/O. It validates the codec via `codec.New`, then branches: with `WithRegistry` it builds `internal/client.NewClient(reg, WithClientTimeout(o.timeout), WithClientCodec(o.codecType), [WithClientLoadBalancer])` and ignores `target`; otherwise it creates a lazy `transport.NewConnectionPool(target, 0, 1)` whose socket is established by the first `Invoke`/`InvokeAsync`/`NewStream`.

DialOption defaults:

| Option                 | Default when omitted                                                     | Notes                                                                                                                                                                          |
| ---------------------- | ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `WithTimeout(d)`       | `5 * time.Second`                                                        | Registry mode rejects `d <= 0` at `Dial` with `client timeout must be positive`. Static mode accepts `d <= 0` silently, then every call fails with `context.DeadlineExceeded`. |
| `WithDialCodec(t)`     | `CodecJSON`                                                              | Validated at `Dial`; unregistered type yields `codec: type <n> not registered`.                                                                                                |
| `WithRegistry(reg)`    | nil -> static mode                                                       | Presence alone switches to registry mode.                                                                                                                                      |
| `WithLoadBalancer(lb)` | `&load_balance.RoundRobin{}` (zero value) in `internal/client.NewClient` | Silently ignored in static mode (stored, never read). A nil passed explicitly is skipped by `Dial`, so the default survives.                                                   |

`ServerOption`: exactly one member, `WithCodec(t)`, default `CodecJSON`; it is the fallback codec used only when a request header carries `CodecType == 0`. Panics through `NewServer` if `t` is unregistered.

### ClientConn

```go
type ClientConn struct {
    // unexported: mode, pool, regClient, codec, codecType, timeout
}
```

The zero value nil-panics on any method; always construct with `Dial`.

- `Invoke(ctx, service, method string, args, reply any) error` — blocking unary; wraps `ctx` with `cc.timeout` for send and wait. Registry mode additionally forces `future.Done(nil, callCtx.Err())` when the call context expires so the breaker records the failure exactly once.
- `InvokeAsync(ctx, service, method string, args any) (*Future, error)` — both modes. Acquires under a `cc.timeout`-bounded context (cancelled right after acquire), then spawns one watchdog goroutine with `time.NewTimer(cc.timeout)`; on expiry the future resolves with `context.DeadlineExceeded`.
- `NewStream(ctx, service, method string, args any) (ClientStream, error)` — opens a server stream; pool acquisition bounded by `cc.timeout`, stream lifetime governed by the caller's `ctx` (the only cancel path). Registry mode returns an `observedStream` wrapper feeding terminal outcomes into the breaker.
- `Close() error` — always nil. Static: closes the pool (reaping the `readLoop`). Registry: stops the client limiter ticker and closes every per-address pool. The `*Registry` is not closed; call `reg.Close()` yourself. Pool `Close` is idempotent.

`*ClientConn` is safe for concurrent use (`Acquire` mutex; `sync.Map` + atomic seq + write mutex in `TCPClient`; mutexed `Future`). Concurrent `Close` during in-flight calls is safe: later `Acquire` returns `ErrPoolClosed`; pending futures resolve with `connection closed`.

### Server

`type Server struct { inner *internal_server.Server }` — facade over `internal/server`.

- `Register(name string, service any)` — mutex-guarded `map[string]any`; re-registration overwrites; no signature validation until a call fails with `unsupported method signature`; safe during `Serve`.
- `Serve(lis net.Listener) error` — records the listener; if shutdown already began, closes it and returns nil. Accept loop wraps each connection in a `TCPConnection`, tracks it in `conns`, and runs `Handle` in a `wg.Go` goroutine. Returns nil when `Accept` fails post-shutdown; on other accept errors it `continue`s. In practice `Serve` never returns non-nil.
- `GracefulStop()` — `beginShutdown` (close `closing` + listener, `sync.Once`), wait for accept loop (`serveWg`), sweep `SetReadDeadline(time.Now())` over tracked conns to unblock idle readers, wait for connection goroutines and their streams (`wg`), stop the limiter, log `server graceful stop complete`. In-flight work drains; idle pooled connections do not delay it (`TestGracefulStopWithIdleConnection`).
- `Stop()` — `beginShutdown`, wait accept loop, force-close every conn (`SetLinger(0)` then `Close`, peers see RST), stop limiter, log `server stop complete`. Does not wait for handlers; still effective after a prior `GracefulStop` (only the listener step is inside the `Once`).

### Future (alias of `transport.Future`)

Constructors `NewFuture`/`NewFutureWithCodec` are internal; users only receive futures from `InvokeAsync`.

- `Done(res []byte, err error)` — producer side, idempotent: first call latches, fires the `OnComplete` callback (outside the mutex, before `close(done)`), then closes the channel. Later calls are no-ops, which makes late server responses after a timeout harmless.
- `Wait() ([]byte, error)` — unbounded block; returns raw decompressed body + error; blocks forever if nothing resolves it. `WaitWithContext(ctx)` / `WaitWithTimeout(d)` are the bounded variants; on context completion they return `ctx.Err()`.
- `GetResult(reply any) error` / `GetResultWithContext(ctx, reply)` — decode the body with the codec captured at send time (nil codec falls back to JSON); latched error returned verbatim, `reply` untouched. `DoneChan() <-chan struct{}` exposes the completion channel; `IsDone()` is the non-blocking probe.
- `OnComplete(fn func(error))` — single callback slot; invokes `fn` synchronously if already complete; a second registration before completion overwrites the first (see Pitfalls: overwriting the breaker callback).

### Stream interfaces

```go
type ServerStream interface { Send(msg any) error; Context() context.Context }
type ClientStream interface { Recv(msg any) error; Context() context.Context }
```

`ServerStream.Send` marshals with the request codec and writes one `StreamData` frame under the connection write mutex (concurrent `Send` yields intact frames). Its `Context()` derives from `context.Background()` and is never cancelled — it cannot detect client disconnect. `ClientStream.Recv` returns the next decoded frame, then `io.EOF` after a `StreamEnd` once buffered frames drain, the server error on `StreamError`, or `ctx.Err()` when the stream context finishes. `Recv` is single-consumer. The concrete `*transport.ClientStreamConn` has a `Cancel()` method not present in the interface and unimportable — cancelling the `ctx` given to `NewStream` is the only client-side stop.

### Errors

Two sentinel values exist, both in `internal/transport` and unexported through `pkg/rpc`: `ErrPoolClosed = errors.New("connection pool closed")` (returned by `Acquire` after `Close`; match on the string) and `ErrStreamNotFound = errors.New("stream not found")` (declared, referenced nowhere — dead code). Everything else crosses the wire as a string in `Header.Error`, rebuilt client-side with `errors.New`, so `errors.Is`/`errors.As` never match typed server errors. Panics rather than errors: `codec.Register` with a nil factory (`codec: factory is nil`) or duplicate type (`codec: type <n> already registered`), and `rpc.NewServer` on any option error. Framework-generated messages:

| Message                                                                                          | Origin                                                                               |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `service not found: <s>` / `method not found: <s>.<m>` / `unsupported method signature: <s>.<m>` | `internal/server/handler.go` reflect dispatch                                        |
| `handler panic: <v>`                                                                             | `safeCall` recovered a panic                                                         |
| `rate limit exceeded`                                                                            | server `Handle` limiter and registry-client limiter                                  |
| `circuit breaker open`                                                                           | registry client, Open or probe-exhausted HalfOpen                                    |
| `no instance available` / `load balancer returned empty address`                                 | `internal/client/resolver.go`                                                        |
| `registry not configured`                                                                        | registry-mode client with nil `*Registry`                                            |
| `connection closed` / `connection pool closed`                                                   | `TCPClient.Close`, send-path closed re-check; `ErrPoolClosed`                        |
| `codec: type <n> not registered` / `codec must not be nil`                                       | `codec.New`; `NewHandler` without a codec option                                     |
| `client timeout must be positive` / `client load balancer must not be nil`                       | internal client options                                                              |
| `header is nil`                                                                                  | `protocol.Encode`                                                                    |
| `data too short` / `invalid magic number` / `packet too large` / `incomplete packet`             | `protocol.Decode`                                                                    |
| `compressor not found` / `proto codec: not proto.Message`                                        | `codec.Compress`/`Decompress` unregistered type; protobuf codec on a non-proto value |

### pkg/api

Sample services used by tests and examples — no framework logic: `Args{A, B int}`, `Args1{A, B, C int}`, `Reply{Result int}`, and stateless `Arith`/`Arith2` with grpc-style `Add`/`Mul` (`Result` = sum / product of the fields); register with `server.Register("Arith", &api.Arith{})`.

## Wire protocol (`internal/protocol`)

```
byte offset: 0        2              6              10          10+HeaderLen
             +--------+--------------+--------------+-----------+-----------+
             | Magic  |  HeaderLen   |    BodyLen   |  Header   |   Body    |
             | uint16 |    uint32    |     uint32   |  JSON     | codec bytes|
             | 0x1234 |     BE       |      BE      | HeaderLen |  BodyLen  |
             +--------+--------------+--------------+-----------+-----------+
const Magic uint16 = 0x1234   (message.go:11)
```

- Fixed 10-byte big-endian prefix; no version field, no flags byte, no checksum. `HeaderLen`/`BodyLen` are the encoded-header and post-compression-body lengths.
- The header is always JSON (`codec.New(codec.JSON)` fresh per `Encode`/`Decode`), regardless of body codec.
- Body is compressed iff `Header.Compression != CompressionNone`; every framework send site hard-codes `codec.CompressionGzip`, so bodies are always gzipped on the wire. An empty body still yields a ~23-byte gzip stream, so `BodyLen` is never 0 for framework frames.
- No application-level heartbeat frames exist.

```go
type Header struct {
    RequestID   uint64
    ServiceName string
    MethodName  string
    Error       string
    CodecType   CodecType                 // protocol-local byte type
    Compression codec.CompressionType
    StreamFlag  StreamFlag `json:",omitempty"`
}
type CodecType byte   // CodecTypeJSON = iota+1 (=1), CodecTypeProto (=2)
type StreamFlag byte   // StreamNone=0, StreamData=1, StreamEnd=2, StreamError=3
```

- Only `StreamFlag` is `omitempty`: `StreamNone` is absent from serialized JSON and a missing key decodes to 0 (unary). All other fields are always present.
- `protocol.CodecType` and `codec.Type` are distinct types sharing the numeric encoding; send paths convert explicitly (`CodecType: protocol.CodecType(cc.codecType)`).
- Responses carry only `RequestID` (+`Error`/`StreamFlag` when applicable): `ServiceName`, `MethodName`, `CodecType` are not echoed, and the client decodes replies with the codec captured at send time.

`Encode` rejects a nil header (`header is nil`), compresses, marshals the header, and writes one fresh `[]byte` of `10+headerLen+bodyLen`. `Decode` validates in order: `<10` bytes -> `data too short`; magic -> `invalid magic number`; `10+hl+bl > math.MaxInt` -> `packet too large` (unreachable on 64-bit; guards 32-bit builds); `len(data) < total` -> `incomplete packet`; then header unmarshal and body decompress. `DecodeHeaderLen`/`DecodeBodyLen` read a BE `uint32`, returning 0 for inputs under 4 bytes.

`PacketBuffer` (mutex-guarded, initial cap `4096*2`) resynchronizes by dropping one leading byte at a time while bytes 0-1 are not the magic, then requires 10 bytes, then the full frame; a complete frame is copied out and the buffer advanced by reslicing, so garbage ahead of a valid frame is recovered in the same `Read`. Stream multiplexing is `RequestID` + `StreamFlag` only — a stream request is wire-identical to a unary request (`StreamNone`), and the server decides by reflecting on the method's second parameter. The client must choose `Invoke` vs `NewStream` correctly; mismatches are not diagnosed (Pitfalls).

## Codecs and compression (`internal/codec`)

```go
type Codec interface { Marshal(v any) ([]byte, error); Unmarshal(data []byte, v any) error }
type Type byte; type Factory func() Codec
func Register(t Type, f Factory)   // panics: factory is nil / already registered
func New(t Type) (Codec, error)    // "codec: type <n> not registered"
const JSON  Type = 1               // encoding/json wrappers (json.go)
const PROTO Type = 2               // proto.Marshal/Unmarshal, requires proto.Message (protobuf.go)
```

Registration is via package `init()` guarded by `sync.RWMutex`; `New` returns a fresh instance per call (stateless structs). JSON codec inherits `encoding/json` semantics: only exported fields round-trip; `int64` above 2^53 loses precision when a peer decodes into `any`; `Unmarshal([]byte{}, ...)` fails with `unexpected end of JSON input`. Protobuf codec errors with `proto codec: not proto.Message`; there are no `.proto` files, no generated stubs, no descriptor-registry use in the module (`codec_test.go` uses `emptypb.Empty`).

Codec negotiation is per-request and one-directional: every client send path stamps `Header.CodecType`; `Handler.Process` honours any non-zero value (unknown -> `codec: type <n> not registered` error frame) and falls back to the server's configured codec (JSON unless `WithCodec`) only when it is 0. So `WithDialCodec(CodecProto)` interoperates with a default-JSON server as long as every request/reply type is a `proto.Message` on both sides.

```go
type CompressionType byte  // CompressionNone=0, CompressionGzip=1
func Compress(data []byte, t CompressionType) ([]byte, error)    // "compressor not found" if unset
func Decompress(data []byte, t CompressionType) ([]byte, error)
func RegisterCompressor(t CompressionType, c compressor)         // compressor iface is unexported
```

`GzipCompressor` is the only registered compressor (`init`); nothing is registered for `CompressionNone` (Encode/Decode skip compression in that case instead of calling it). Fresh `gzip.Writer`/`gzip.Reader` per call, default level, no `sync.Pool`. Because `compressor`'s methods are unexported and everything is in `internal/`, gzip is the only real option and no `pkg/rpc` knob disables it.

## Transport (`internal/transport`)

`TCPConnection`: 4096-byte `bufio.Reader` + `PacketBuffer`. `Read` loops buffer-attempt/fresh-scratch-read; any decode error propagates and tears the connection down. `Write` encodes, takes `writeMu`, loops until all bytes flush. `Close` sets `SetLinger(0)` on `*net.TCPConn` (RST, not FIN). `SetReadDeadline` exists for the `GracefulStop` sweep.

`TCPClient` (one per socket): `newTCPClient` dials with hard-coded `net.DialTimeout("tcp", addr, 5*time.Second)` (independent of `WithTimeout`) and starts one `readLoop`. `seq atomic.Uint64`, `nextSeq()` = `seq.Add(1)` (first RequestID = 1). `SendAsyncWithCodec`/`SendStream`: reject when closed (`connection closed`) -> allocate seq -> store `Future` in `pending` / `ClientStreamConn` in `streams` -> re-check closed (`LoadAndDelete` own entry and fail) -> write under `writeMu` -> on write error delete entry, `fail(err)`, return.

`readLoop` demultiplexes by `Header.StreamFlag`:

| Flag                   | Action                                                                                                                                         |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `StreamData`           | `streams.Load(seq)` -> `Push(body)`; entry retained                                                                                            |
| `StreamEnd`            | `streams.LoadAndDelete(seq)` -> `End()` (terminates with `io.EOF`)                                                                             |
| `StreamError`          | `streams.LoadAndDelete(seq)` -> `Error(errors.New(Header.Error))`                                                                              |
| `StreamNone` (default) | `pending.LoadAndDelete(seq)`; absent -> drop frame; else `Future.Done(nil, errors.New(Error))` if `Error != ""`, else `Future.Done(body, nil)` |

Unknown sequence numbers are silently dropped (late responses after a client timeout are harmless; but an error response to a stream request vanishes — Pitfalls). Teardown is unified in `shutdown(err)` (CAS-guarded on `closed`), entered by read/write failure (`fail`) and `Close` (with `connection closed`): close socket (reaping `readLoop`), resolve every `pending` future with `err`, terminate every `stream` with `err`. Callers blocked in `Wait()`/`GetResult()` always wake (`TestTCPClientCloseUnblocksPending`).

`ConnectionPool`: `NewConnectionPool(addr, maxIdle, maxActive)` — `maxIdle` accepted and never read; `maxActive=1` at every call site (`pkg/rpc/client.go`, `internal/client/resolver.go`), so one socket per address. `Acquire(ctx)` holds `p.mu` for its whole body including any dial: closed -> `ErrPoolClosed`; one non-blocking `ctx` check (this does not bound the dial); dial if under `maxActive`; else round-robin scan from `p.next` returning the first live client and splicing dead ones out; if all dead, dial fresh. `Close` is idempotent and closes every client.

`ClientStreamConn`: 64-slot buffered frame channel, derived cancellable context, out-of-band `termCh` closed once via `sync.Once`. `Push` blocks when the buffer is full and the stream is neither cancelled nor terminated — from `readLoop`, stalling everything else on that socket. `End`/`Error` never block (terminal state bypasses the data channel; `TestClientStreamTerminalNonBlocking`). `Recv` drains buffered frames before returning the terminal error. `Cancel()` cancels only the local context; the server handler runs to completion. No flow control beyond the 64-frame buffer and TCP's window.

## Registry (`internal/registry`, reachable via `rpc.Registry`)

```go
type Instance struct{ Addr string }
func NewRegistry(endpoints []string) (*Registry, error)   // etcd v3, DialTimeout 5s
func (r *Registry) Register(service string, ins Instance, ttl int64) error
func (r *Registry) Discover(service string) ([]Instance, error)
func (r *Registry) Close() error
```

Hard-coded namespace, no parameter: `prefix = "/github.com/hangtiancheng/yukino.go/libs/yukino_rpc/services/"`, `key = <prefix><service>/<addr>`, `value = <addr>`.

`Register`: `Grant(ctx, ttl)` (`ttl` in seconds, passed straight through to etcd unvalidated) -> `Put(key, addr, WithLease)` -> `KeepAlive` plus one goroutine that merely drains the channel. No recovery: when the channel closes (lease expiry, etcd outage) the goroutine exits, the lease ID is not retained, the instance disappears from every watcher, and nothing re-registers. Long-lived servers need external re-registration.

`Discover`: RLock fast path on the in-memory cache; else `initService` under write lock (`Get(prefix+service+"/", WithPrefix())`, populate `map[addr]Instance`, start one `watch(service)` goroutine); return `copyInstances`. The copy is defensive (callers cannot mutate the cache) but iterates a Go map, so order is randomized per call. Cache entries are never evicted; watch goroutines are bounded only by the number of discovered services. `watch` applies `EventTypePut` (insert) and `EventTypeDelete` (trim prefix, delete), and re-opens the watch one second after the channel closes. `Close` cancels the registry context and closes the etcd client; both nil-safe, so `&Registry{}` closes without panicking. `ClientConn.Close` never calls it.

Note: `Discover`'s cache-hit path takes `r.mu.RLock()` then calls `copyInstances`, which takes `RLock()` again. Recursive read-locking of `sync.RWMutex` is documented unsafe: a writer blocking between the two acquisitions deadlocks (see Pitfalls).

## Load balancing (`internal/load_balance`)

```go
type LoadBalancer interface { Select([]registry.Instance) registry.Instance }
```

All implementations return the zero `Instance{}` for an empty list, which the client turns into `load balancer returned empty address`. Concrete types are internal; `pkg/rpc` exports only the interface, so custom balancers implement `Select` — the built-ins are reachable only as defaults or inside the module's own tests.

- `RoundRobin{idx atomic.Uint64}` / `NewRR()`: `list[(i-1)%len]`, first pick index 0; the zero value is usable, which is why `internal/client.NewClient` defaults to `&RoundRobin{}`. The counter is not rebased when the list size changes.
- `Random{r *rand.Rand, m sync.Mutex}` / `NewRandom()`: `math/rand` seeded from `time.Now().UnixNano()`; zero value nil-panics; mutex serializes selection.
- `WeightedRR` / `NewWeightedRR(weights []int)`: Nginx-style smooth weighted RR; negative weights clamp to 0; returns zero `Instance` when `len(list) != len(weights)` or `totalWeight <= 0`. `{1,2,3}` over six picks yields exactly 1/2/3 (`load_balance_test.go`). Endpoint refresh: no resolver goroutine, no cached address — `internal/client.getAddr` calls `Discover` + `Select` on every `Invoke`/`InvokeAsync`/`InvokeStream`. After the first call `Discover` is an in-memory read kept fresh by the watch goroutine. Pools per address are created lazily in a `sync.Map` and never evicted: a long-running client accumulates one idle socket (and one breaker) per address it ever contacted until `Close`.

## Circuit breaker (`internal/breaker`)

Three states `Closed`/`Open`/`HalfOpen`, mutex-guarded, used only by the registry-mode client with hard-coded parameters from `internal/client/resolver.go`: `breaker.NewCircuitBreaker(10, 0.6, 5*time.Second)`, keyed `service|addr`, lazily created via `sync.Map.LoadOrStore`, never removed.

- `Allow()`: Closed -> true. Open -> false until `time.Since(lastStateChange) > openTimeout`, then the first call transitions to HalfOpen, sets `halfOpenProbe = true`, and returns true. HalfOpen -> false while a probe is outstanding, else admit one. So exactly one probe per half-open episode.
- `RecordSuccess()`: Closed -> count++, and once `failure+success >= windowSize` evaluate the rate (a success completing the window can trip the breaker at `rate >= threshold`, else counters reset). HalfOpen -> `Closed`. Open -> ignored.
- `RecordFailure()`: Closed -> count++, window evaluation as above. HalfOpen -> straight back to `Open` (restarts `openTimeout`). Open -> ignored.

The window is a fixed-count tumbling window (no time decay, no minimum-request guard beyond `windowSize`). `breaker_test.go` pins the transitions. Accounting coverage: unary registry calls record pool/send failures directly and completion outcomes via `Future.OnComplete`; `Invoke`'s forced `Done(callCtx.Err())` and `InvokeAsync`'s watchdog timeout each fire once (`Done` is idempotent). Streams: `observedStream` records at most once via `sync.Once` — success on `io.EOF`, failure on any other terminal error, nothing on `context.Canceled` (caller cancel is not a service failure); abandoned streams record nothing. Static mode has no breaker.

## Rate limiter (`internal/limiter`)

`NewTokenBucket(rate)` clamps negative to 0, starts full (`tokens = rate`), and runs one goroutine with a `time.Second` ticker that resets `tokens = rate` per tick — a fixed-window quota, not a smooth drip: the whole budget is available at the top of each second and a burst can exhaust it instantly; rate 0 rejects everything. `Allow` decrements under mutex; `Stop` closes via `sync.Once`. Both instances use hard-coded 10000 with no option: server (`internal/server.NewServer`) shared across all connections/services — `Handle` checks after reading each frame and, on rejection, writes a response with the original `RequestID`, `Error: "rate limit exceeded"`, gzip, `StreamFlag` 0, then continues. Registry client (`internal/client.NewClient`) checks at the top of `invokeAsync`/`InvokeStream`; `Client.Close` stops it. An un-stopped server leaks its ticker goroutine.

## Server internals: dispatch, streaming, locking

Reflection dispatch (`internal/server/handler.go`) accepts exactly three shapes, tried in order; lookup is `reflect.Value.MethodByName` on the registered value (exported methods only; a pointer-receiver method requires registering a pointer):

| Signature                                         | Checked via                                                                                             | Reply handling                                                           |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `Method(ctx context.Context, req *T) (*R, error)` | NumIn 2, NumOut 2, In(0) implements `context.Context`, In(1)/Out(0) pointers, Out(1) implements `error` | non-nil `*R` dereferenced; nil `*R` -> nil result -> empty response body |
| `Method(req *T, stream ServerStream) error`       | NumIn 2, NumOut 1 error, In(0) pointer, In(1) implements `stream.ServerStream`                          | streaming (below)                                                        |
| `Method(req *T, reply *R) error` (net/rpc style)  | NumIn 2, NumOut 1 error, both inputs pointers, In(1) plain pointer                                      | framework allocates `*R`, returns `reply.Elem().Interface()`             |

Anything else -> `unsupported method signature`. Request body unmarshal is skipped when the body is empty. Every reflective call goes through `safeCall`, recovering panics into `handler panic: <value>` — a panicking handler cannot crash the process or poison the connection (`TestPanicAndBadSignatureDoNotCrashServer`). `NewHandler(s any, ...)` ignores its first parameter (the service arrives per request through `Process`) and errors `codec must not be nil` without a codec option, which is why `NewServer` always passes `WithHandlerCodec(codec.JSON)`.

Streaming: only server-streaming exists (no client-originated `StreamFlag` codepoints; `ClientStream` has no `Send`; no half-close, no client- or bidi-streaming at the protocol level). A streaming method runs via `streamWg.Go` (per-connection WaitGroup declared in `Handle`), so it never blocks other requests on the socket (`TestStreamDoesNotBlockUnary`). The wrapper writes exactly one terminal frame: `StreamError` (with `err.Error()`) on panic or non-nil error, `StreamEnd` otherwise. `serverStream.Send` writes one `StreamData` frame per call and returns write errors to the handler; ignoring them sends into a dead connection. `streamWg.Wait()` precedes the deferred `conn.Close()`, so connections outlive their streams and `GracefulStop` drains them.

Locking: `Server.mu` guards `services`+`listener`+`conns` (held briefly per request for the service lookup, never during handlers); `TCPClient.writeMu` plus `TCPConnection.writeMu` (redundant double lock, harmless); `sync.Map` for `pending`/`streams`/pools/breakers; atomic `closed`/`seq`; `PacketBuffer` and `Future` mutexes; `CircuitBreaker.mu`, `TokenBucket.mu`, `Registry.mu` (RWMutex). `ConnectionPool.mu` is held across `net.DialTimeout`. Goroutines and reaping: `readLoop` (first `Acquire` per address; reaped by `Close`/peer close); `InvokeAsync` watchdog (bounded by `timeout`); accept loop (`serveWg`); per-connection `Handle` (`wg`); per-stream handler (`streamWg.Go` — a never-returning handler blocks `GracefulStop` forever); server/client limiter tickers (`Stop`); registry keepalive drain (channel close or `Registry.Close`); registry watch per service (`Registry.Close`). `Close` of a `ClientConn` does not reap a registry's goroutines; `Registry.Close` does not un-dial etcd clients created elsewhere.

Timeouts stack, none transmitted to the server: (1) hard-coded 5s dial timeout; (2) `cc.timeout` (default 5s) bounding acquisition, unary calls, and the async watchdog; (3) the caller's `ctx` — unary effective deadline is `min(ctx deadline, now+timeout)`; for `NewStream`, `ctx` governs stream lifetime with no framework timeout. `Handler.Process` invokes with `context.Background()`: handlers keep running after the client gives up and see no cancellation. Hot-path allocation: fresh 4096 scratch per buffered read, fresh frame copy in `PacketBuffer.Read`, fresh output slice in `Encode`, fresh JSON codec per `Encode`/`Decode`, fresh gzip writer/reader per body; `PacketBuffer` advances by reslicing (backing array reclaimed only when `append` reallocates); no `sync.Pool` anywhere.

## Usage

### Server (grpc-style handler)

```go
type Args struct{ A, B int }
type Reply struct{ Result int }
type MathService struct{}

func (s *MathService) Add(ctx context.Context, args *Args) (*Reply, error) {
    return &Reply{Result: args.A + args.B}, nil
}

server := rpc.NewServer() // panics on invalid options; default CodecJSON
server.Register("Math", &MathService{})
lis, _ := net.Listen("tcp", "127.0.0.1:8080")
log.Fatal(server.Serve(lis)) // returns nil once Stop/GracefulStop closes the listener
```

### Client (static mode)

```go
conn, err := rpc.Dial("127.0.0.1:8080", rpc.WithTimeout(5*time.Second)) // err: codec validation only
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

var reply Reply
err = conn.Invoke(context.Background(), "Math", "Add", &Args{1, 2}, &reply) // first call dials
```

Protobuf body: `rpc.Dial(target, rpc.WithDialCodec(rpc.CodecProto))` with `proto.Message` request/reply types; the server needs no matching option (per-request `CodecType` negotiation). Async: `future, err := conn.InvokeAsync(...)` then `<-future.DoneChan()` / `future.GetResult(&reply)`; the watchdog resolves with `context.DeadlineExceeded` after `cc.timeout`, so `GetResult`/`Wait` cannot hang forever (unbounded `Wait` on a raw future can).

### Server-streaming

```go
// server — recognized as streaming by the second parameter
func (s *FeedService) Subscribe(req *SubArgs, stream rpc.ServerStream) error {
    for i := 0; i < req.Count; i++ {
        if err := stream.Send(&Event{Index: i}); err != nil {
            return err // client gone; framework writes the terminal frame
        }
    }
    return nil // framework writes StreamEnd
}

// client
ctx, cancel := context.WithCancel(context.Background())
defer cancel() // cancelling ctx is the only way to abandon a stream
stream, err := conn.NewStream(ctx, "Feed", "Subscribe", &SubArgs{Count: 10})
if err != nil {
    log.Fatal(err)
}
for {
    var ev Event
    if err := stream.Recv(&ev); err != nil {
        break // io.EOF on StreamEnd; error on StreamError/ctx/transport
    }
    process(ev)
}
```

Consume promptly: the client buffers only 64 frames; a full buffer blocks the shared `readLoop`.

### Registry mode

```go
// server side: publish this instance with a 10-second lease
reg, err := rpc.NewRegistry([]string{"localhost:2379"}) // err: client config, not connectivity
if err != nil {
    log.Fatal(err)
}
defer reg.Close()
lis, _ := net.Listen("tcp", "0.0.0.0:8080")
if err := reg.Register("Math", rpc.Instance{Addr: "10.0.0.5:8080"}, 10); err != nil { // alias-reachable; no pkg/rpc wrapper
    log.Fatal(err)
}
server := rpc.NewServer()
server.Register("Math", &MathService{})
go server.Serve(lis)

// client side: target is ignored; omit WithLoadBalancer for default RoundRobin
conn, err := rpc.Dial("", rpc.WithRegistry(reg), rpc.WithTimeout(3*time.Second))
if err != nil {
    log.Fatal(err)
}
defer conn.Close() // stops the client limiter, closes every per-address pool
```

Registry mode is the only way to get rate limiting (client side), circuit breaking, load balancing, and discovery. Do not combine `WeightedRR` with the registry: instance order is randomized per `Discover`.

### Error handling and shutdown

`errors.Is` works only for locally produced `context.DeadlineExceeded` (client timeout — the handler may still be running) and `context.Canceled`; every other error arrives as an `errors.New` string and needs `strings.Contains`: `"circuit breaker open"` (registry mode; retry after ~5s), `"rate limit exceeded"` (10000/s, client-side or server-side), `"no instance available"`, `"method not found"` / `"unsupported method signature"` (programming error), `"handler panic"` (server recovered; still healthy), `"connection closed"` / `"connection pool closed"` (torn down). Anything else is the service method's own error string.

For shutdown, `GracefulStop` has no internal deadline: run it in a goroutine with your own timeout and fall back to `Stop` (force: RST every connection) — `Stop` after `GracefulStop` is valid.

## Testing patterns

- Loopback on ephemeral port (`pkg/rpc/stream_test.go` `startTestServer`): `net.Listen("tcp","127.0.0.1:0")`, `go server.Serve(lis)`, `t.Cleanup(server.Stop)`, address from `lis.Addr()` (`pkg/rpc` does not expose it). `t.Cleanup(server.Stop)` rather than `GracefulStop`, so a hung handler fails by timeout.
- In-process, no sockets (`server_test.go`, `transport_test.go`): `net.Pipe()` + `transport.NewTCPConnection` on each side; `SetLinger(0)` is skipped (not a `*net.TCPConn`).
- Direct dispatch (`server_test.go`): `NewHandler(nil, WithHandlerCodec(codec.JSON))` then `h.invoke(...)` with a nil conn/streamWg for the two non-streaming signatures; a nil `streamWg` runs streaming handlers synchronously.
- `codec_test.go`: `assertPanics` helper for registry panics; `failingCompressor` for compression errors; `emptypb.Empty` for the proto round-trip. `registry_test.go` builds `&Registry{services: ...}` literals (and `&Registry{}` for nil-safe `Close`) to test discovery offline without etcd.
- `fixes_test.go` pins: `GracefulStop` completing with an idle pooled connection (`TestGracefulStopWithIdleConnection`), a slow stream not blocking unary calls (`TestStreamDoesNotBlockUnary`), handler panics and non-pointer returns not crashing the server (`TestPanicAndBadSignatureDoNotCrashServer`), `InvokeAsync` reachable from the public API (`TestInvokeAsyncStatic`).

## Pitfalls and known limitations

1. Gzip is mandatory: every send site hard-codes `CompressionGzip`; no `pkg/rpc` option changes it. For small payloads the ~23-byte gzip framing exceeds the data; batch items into one call if overhead matters.
2. Limiter rate (10000) and breaker parameters (`10, 0.6, 5s`) are hard-coded, not options. The limiter is a fixed-window reset, so 10000 requests can land in the first millisecond of a second and the rest of the second rejects everything. Do upstream admission control if you need real shaping.
3. A grpc-style handler returning `(nil, nil)` yields an empty response body; the client then runs `json.Unmarshal` on it and fails with `unexpected end of JSON input`. Always return a non-nil `*R`.
4. One socket and one `readLoop` per address (`maxActive=1`): a stream whose consumer lets its 64-frame buffer fill blocks `Push` in `readLoop`, stalling every future and stream on that connection. Terminal frames never block (separate channel); data frames do.
5. `ConnectionPool.Acquire` holds the pool mutex across the 5-second `net.DialTimeout`; the entry `ctx` check does not bound the dial, so every concurrent acquirer for that address queues behind an unreachable-peer dial — even ones whose contexts already expired.
6. An error response to a stream request is silently dropped: `writeError` and rate-limit rejections always write `StreamFlag` 0, `readLoop` looks in `pending` (the seq is registered in `streams`), finds nothing, discards the frame. `NewStream` against a nonexistent service/method, a mismatched signature, an undecodable body, or a server-side rejection returns a stream whose `Recv` blocks until cancellation. Give stream contexts deadlines and verify method names in integration tests.
7. Unary vs stream is indistinguishable on the wire, so a client/server mismatch is not diagnosed: `Invoke` on a streaming method waits for a unary response that never comes; `NewStream` on a unary method drops the arriving `StreamNone` frame and blocks. Keep the calling convention in sync with the handler shape manually.
8. Static mode has zero resilience features (no limiting/breaker/balancer/discovery/health-checking); `WithLoadBalancer` in static mode is stored and never read.
9. `WithTimeout` behaves inconsistently for non-positive values: registry `Dial` rejects (`client timeout must be positive`); static accepts and then every call fails with `context.DeadlineExceeded`. Always pass a positive duration.
10. Static-mode `Invoke` leaks its `pending` entry on timeout (no forced `future.Done`, unlike registry mode); the entry clears only when a late response arrives or the connection shuts down.
11. `WeightedRR` over the etcd registry misroutes: `Select` matches weights positionally but `copyInstances` returns randomized map order per call, and `getAddr` calls `Discover` per request. `Select` also returns empty when `len(weights) != len(instances)` — guaranteed on fleet scaling — surfacing as `load balancer returned empty address`. Use it only with a stable, self-ordered list; use `RoundRobin`/`Random` with the registry. Randomized order also degrades `RoundRobin` toward random without misrouting.
12. Lease expiry is undetected and unrepaired: `Register`'s keepalive goroutine only drains; on channel close the instance is gone from discovery with no re-registration. Supervise and re-register periodically.
13. `Registry.Discover`'s cache-hit path nests `RLock` inside `RLock` (`copyInstances`). Recursive read-locking is documented unsafe for `sync.RWMutex`: a `watch` writer blocked between the acquisitions deadlocks `Discover` and the watch. Small window, widens with churn.
14. The etcd prefix is hard-coded to `/github.com/hangtiancheng/yukino.go/libs/yukino_rpc/services/` with no namespace parameter: environments sharing one etcd cluster discover each other's instances. Use separate clusters or key-range ACLs.
15. `rpc.NewServer` panics rather than returning errors; a configuration-driven codec value can crash the process. Validate against `rpc.CodecJSON`/`rpc.CodecProto` first, or wrap in `recover`.
16. `TCPConnection.Close` sets `SetLinger(0)` — peers see RST, not clean EOF, and kernel send-buffer data is discarded. `GracefulStop` avoids this for in-flight work but its connection close is still RST.
17. Only server-streaming exists at the protocol level (no client-originated frames, no `ClientStream.Send`, no half-close). Model bidi flows as two server-streams or repeated unary calls.
18. Signatures are validated at call time, not registration: typos, unexported methods, value-registered pointer receivers, or wrong shapes succeed silently and fail per call with `method not found` / `unsupported method signature`. Smoke-test every method.
19. Deadlines/cancellation never propagate to the server (handlers run under `context.Background()`), so handlers keep burning CPU/transactions/frames after the client gave up. Enforce server-side deadlines inside handlers.
20. Unary requests on one connection are serialized: `Handle` reads a frame, dispatches synchronously via `Process`, then reads the next — only streaming handlers get goroutines. One slow unary handler delays every subsequent unary call on that socket (i.e., all of that client's calls to that address). Keep unary handlers fast or move slow work behind streams.
21. Error types are erased across the wire (plain `Header.Error` strings, `errors.New` on the client side); no codes/status/details. Encode structured failures in the reply type.
22. One malformed frame kills the connection: any `Decode` error from `TCPConnection.Read` tears down the socket (client: fails every pending future/stream; server: closes the connection). `PacketBuffer` resyncs on bad magic, but a header failing JSON unmarshal or a body failing gzip is fatal. Never share the port with another protocol.
23. `Serve` spins on persistent accept errors: post-shutdown check fails, then `continue` with no backoff — e.g. fd exhaustion becomes a tight CPU loop — and since `Serve` never returns non-nil, supervisors cannot distinguish this from a clean shutdown. Monitor accept health externally.
24. `Future.OnComplete` is a single slot: registering your own on a registry-mode async future overwrites the breaker-recording callback, so that call stops feeding the breaker. Use `DoneChan()` for your own notification.
25. Streams cannot be cancelled through the `ClientStream` interface (`Cancel` is on the unimportable concrete type); cancel the `NewStream` ctx — client-side only; the server handler still runs to completion.
26. Per-address pools and per-`service|addr` breakers are never evicted: a churning fleet grows one idle socket + one breaker per historical address until `ClientConn.Close`. Resource cleanup is otherwise manual: an unstopped `Server` leaks its limiter ticker; an unclosed registry-mode `ClientConn` leaks its limiter plus every pool `readLoop`; an unclosed `Registry` leaks one watch goroutine per service plus keepalive drains. `ClientConn.Close` does not close the `*Registry`. Defer all three.
27. `transport.ErrStreamNotFound` is declared and never referenced — dead code; and two parameters are vestigial: `NewConnectionPool`'s `maxIdle` and `NewHandler`'s first parameter are accepted and ignored.

## File map (all 44 `.go` files under `libs/yukino_rpc/`)

| Path                                                                                                                               | Contents                                                                                                                                                                 |
| ---------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `pkg/rpc/rpc.go`                                                                                                                   | aliases `CodecType`/`Registry`/`Instance`/`LoadBalancer`/`Future`; `CodecJSON`/`CodecProto` vars; `NewRegistry`                                                          |
| `pkg/rpc/client.go`                                                                                                                | `DialOption`s + defaults, `connMode`, `Dial`, `ClientConn` methods, static send paths, async watchdog                                                                    |
| `pkg/rpc/server.go`                                                                                                                | `ServerOption`, `WithCodec`, `NewServer` (panics on option error), `Server` facade                                                                                       |
| `pkg/rpc/stream.go`                                                                                                                | `ServerStream`/`ClientStream` aliases                                                                                                                                    |
| `internal/stream/stream.go`                                                                                                        | the two stream interfaces; breaks the server<->transport import cycle                                                                                                    |
| `pkg/rpc/stream_test.go`, `pkg/rpc/fixes_test.go`                                                                                  | end-to-end streaming tests + `startTestServer` harness; regression tests for graceful-stop idle conns, stream-vs-unary isolation, panic resilience, public `InvokeAsync` |
| `pkg/api/arith.go`, `pkg/api/arith_test.go`                                                                                        | `Arith`/`Arith2`/`Args`/`Args1`/`Reply` samples; direct unit tests                                                                                                       |
| `internal/protocol/header.go`, `message.go` (+`message_test.go`)                                                                   | `Header`, `CodecType`, `StreamFlag`; `Magic`, `Encode`/`Decode`/`DecodeHeaderLen`/`DecodeBodyLen`; wire-format tests                                                     |
| `internal/codec/codec.go`, `json.go`, `protobuf.go`, `compress.go` (+`codec_test.go`)                                              | `Codec`, registry, `JSON`=1/`PROTO`=2; `CompressionType`, `GzipCompressor`, `Compress`/`Decompress`                                                                      |
| `internal/transport/tcp_connection.go`, `tcp_client.go`, `tcp_connection_pool.go`, `future.go`, `stream.go` (+`transport_test.go`) | `PacketBuffer`, `TCPConnection`; `TCPClient`/`readLoop`; `ConnectionPool`/`ErrPoolClosed`; `Future`; `ClientStreamConn`/`ErrStreamNotFound`                              |
| `internal/server/server.go`, `handler.go`, `options.go`, `stream.go` (+`server_test.go`)                                           | accept loop/limiter/graceful shutdown; per-header codec dispatch + reflect + `safeCall`; `WithHandlerCodec`/`WithServerCodec`; `serverStream`                            |
| `internal/client/client.go`, `invoke.go`, `option.go`, `resolver.go` (+`client_test.go`)                                           | registry-mode `Client`; `Invoke`/`InvokeAsync`/`InvokeStream`/`observedStream`; client options; pool/addr/breaker resolution                                             |
| `internal/registry/`                                                                                                               | `registry.go`: `Instance`, `Registry`, `Register`/`Discover`/`watch`/`Close` over etcd v3 (+`registry_test.go`)                                                          |
| `internal/load_balance/`, `internal/breaker/`, `internal/limiter/`                                                                 | `balancer.go`/`round_robin.go`/`random.go`/`weighted_rr.go` (+test); three-state `breaker.go` (+test); fixed-window `token_bucket.go` (+test)                            |

## Dependencies

`go.mod` declares `module github.com/hangtiancheng/yukino.go/libs/yukino_rpc` with `go 1.26.0`; the enclosing `go.work` pins the toolchain at `go 1.26.4`. Code uses recent-toolchain APIs (`reflect.TypeFor`, `sync.WaitGroup.Go`, range-over-int).

Direct requirements, exactly two:

- `go.etcd.io/etcd/client/v3 v3.7.0` — used only by `internal/registry` (`New`, `Grant`, `Put`+`WithLease`, `KeepAlive`, `Get`+`WithPrefix`, `Watch`).
- `google.golang.org/protobuf v1.36.11` — used by `internal/codec/protobuf.go` (`proto.Marshal`/`Unmarshal`) and `codec_test.go` (`emptypb.Empty`). No `.proto` files, no generated code, no descriptor registry. All other `require` entries are `// indirect`, pulled in by etcd: notably `google.golang.org/grpc v1.83.1` (etcd's transitive dependency — yukino_rpc uses gRPC for nothing; do not infer gRPC semantics from its presence), `go.uber.org/zap v1.28.0` (the framework itself logs only `server graceful stop complete` / `server stop complete` via stdlib `log`; no logger injection point), `github.com/stretchr/testify v1.12.1` and `go.yaml.in/yaml/v3 v3.0.5` (transitive entries imported by neither this module's code nor its tests), plus etcd/protobuf/otel/x-architecture transitive entries (`coreos/go-semver`, `coreos/go-systemd/v22`, `golang/protobuf`, `grpc-gateway/v2`, `etcd/api/v3`, `etcd/client/pkg/v3`, `otel` + `otel/sdk/metric`, `multierr`, `x/net`, `x/sys`, `x/text`, `genproto/googleapis/{api,rpc}`). `go.mod` also carries `replace` directives mapping `yukino_cache`, `yukino_http`, and `yukino_orm` to sibling directories; none of the three is imported by yukino_rpc. No YAML library is imported by any `.go` file in this module — service-discovery configuration is constructor arguments (`NewRegistry(endpoints)`), not files.

## Cross-references

- `yukino-orm` — RPC handlers are the natural home for MongoDB queries; keep them fast (unary serialization, Pitfall 20) and self-deadlined (Pitfall 19).
- `yukino-cache-go` — yukino_rpc has no response cache, idempotency keys, or dedup; memoize at the call site or inside handlers.
- `yukino-http` — the edge for HTTP/WebSocket; it must call yukino_rpc as a client, since there is no transcoding or reflection service.
