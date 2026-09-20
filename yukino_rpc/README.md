# Yukino RPC

A lightweight RPC framework written in Go. Its public API is modeled after [grpc-go](https://github.com/grpc/grpc-go), while the transport layer uses a custom TCP binary protocol. The framework provides request multiplexing, server-side streaming, connection pooling, circuit breaking, rate limiting, load balancing, and etcd-based service discovery out of the box.

Module path: `github.com/hangtiancheng/yukino.go/yukino_rpc`

## Features

- grpc-go-aligned API surface: `NewServer` / `Register` / `Serve` / `GracefulStop` on the server side, `Dial` / `Invoke` / `NewStream` / `Close` on the client side, keeping migration cost low.
- Two connectivity modes: static direct-address dial, and registry-based discovery through etcd.
- Full request multiplexing over a single TCP connection, enabling any number of concurrent RPCs with minimal connection overhead.
- Pluggable codec system with built-in JSON (default) and Protobuf implementations; message bodies are compressed with Gzip by default.
- Server-side streaming with `Send` / `Recv` semantics for pushing an arbitrary number of frames.
- Built-in circuit breaker with three states (Closed / Open / HalfOpen), isolated per `service|addr` dimension.
- Built-in token-bucket rate limiter on both the server and the client.
- Built-in load balancers: RoundRobin, Random, and smooth Weighted Round-Robin; custom balancers can be provided via the `LoadBalancer` interface.
- Service registration and discovery powered by etcd v3, with automatic Watch for real-time instance changes.
- Reflection-based method dispatch that supports both grpc-go-style and net/rpc-style method signatures.

## Installation

```bash
go get github.com/hangtiancheng/yukino.go/yukino_rpc
```

Requires Go 1.21 or later. Registry mode additionally requires an accessible etcd v3 cluster.

## Quick Start

### Defining a Service

Server methods support two unary signatures. The framework identifies them automatically via reflection:

```go
package api

import "context"

type Args struct {
    A int
    B int
}

type Reply struct {
    Result int
}

type Arith struct{}

// grpc-go style (recommended)
func (a *Arith) Add(_ context.Context, args *Args) (*Reply, error) {
    return &Reply{Result: args.A + args.B}, nil
}

// net/rpc compatible style
func (a *Arith) Mul(args *Args, reply *Reply) error {
    reply.Result = args.A * args.B
    return nil
}
```

### Starting the Server

```go
package main

import (
    "log"
    "net"

    "github.com/hangtiancheng/yukino.go/yukino_rpc/pkg/api"
    "github.com/hangtiancheng/yukino.go/yukino_rpc/pkg/rpc"
)

func main() {
    server := rpc.NewServer()
    server.Register("Arith", &api.Arith{})

    lis, err := net.Listen("tcp", ":8080")
    if err != nil {
        log.Fatal(err)
    }

    log.Println("yukino_rpc server listening on :8080")
    if err := server.Serve(lis); err != nil {
        log.Fatal(err)
    }
}
```

Graceful shutdown:

```go
server.GracefulStop() // waits for in-flight requests to complete
// or
server.Stop()         // closes all connections immediately
```

### Client Invocation (Static Direct-Address)

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/hangtiancheng/yukino.go/yukino_rpc/pkg/api"
    "github.com/hangtiancheng/yukino.go/yukino_rpc/pkg/rpc"
)

func main() {
    conn, err := rpc.Dial("127.0.0.1:8080", rpc.WithTimeout(3*time.Second))
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    var reply api.Reply
    if err := conn.Invoke(context.Background(), "Arith", "Add", &api.Args{A: 1, B: 2}, &reply); err != nil {
        log.Fatal(err)
    }
    log.Println("1 + 2 =", reply.Result)
}
```

## Server-Side Streaming

A server method with the signature `Method(req *T, stream rpc.ServerStream) error` is treated as a streaming endpoint. The method calls `Send` for each data frame and returns normally when the stream is done; the framework automatically sends the end-of-stream marker. If the method returns an error, the framework sends an error frame instead.

Server:

```go
type Chunk struct {
    Index   int
    Content string
}

type FeedRequest struct {
    Count int
}

type FeedService struct{}

func (s *FeedService) Generate(req *FeedRequest, stream rpc.ServerStream) error {
    for i := 0; i < req.Count; i++ {
        if err := stream.Send(&Chunk{Index: i, Content: fmt.Sprintf("chunk-%d", i)}); err != nil {
            return err
        }
    }
    return nil
}
```

Client:

```go
stream, err := conn.NewStream(ctx, "Feed", "Generate", &FeedRequest{Count: 5})
if err != nil {
    log.Fatal(err)
}

for {
    var c Chunk
    if err := stream.Recv(&c); err != nil {
        if err == io.EOF {
            break
        }
        log.Fatal(err)
    }
    log.Println("recv:", c)
}
```

Streaming shares the same TCP connection as unary calls. Each stream is assigned a unique RequestID and multiplexed independently, so streaming and unary RPCs never block each other.

## Registry Mode (etcd)

Register the server with etcd:

```go
reg, err := rpc.NewRegistry([]string{"127.0.0.1:2379"})
if err != nil {
    log.Fatal(err)
}
// Register the service instance with a TTL-based lease:
// reg.Register("Arith", registry.Instance{Addr: "127.0.0.1:8080"}, 10)
```

Client discovery and invocation through the registry:

```go
reg, _ := rpc.NewRegistry([]string{"127.0.0.1:2379"})

conn, err := rpc.Dial("",
    rpc.WithRegistry(reg),
    rpc.WithTimeout(3*time.Second),
    // Optional: custom load balancer; defaults to RoundRobin
    // rpc.WithLoadBalancer(load_balance.NewRandom()),
)
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

var reply api.Reply
err = conn.Invoke(context.Background(), "Arith", "Add", &api.Args{A: 3, B: 4}, &reply)
```

In registry mode the `target` parameter passed to `Dial` is ignored. The address is selected in real time by the `LoadBalancer` from the set of instances returned by `Discover`.

## Codec and Compression

JSON is used by default. To switch to Protobuf, declare it explicitly on both sides:

```go
server := rpc.NewServer(rpc.WithCodec(rpc.CodecProto))

conn, _ := rpc.Dial(addr, rpc.WithDialCodec(rpc.CodecProto))
```

In Protobuf mode every args and reply type must implement `proto.Message`.

The codec layer is built on a registry pattern. Each codec type is registered via `codec.Register(type, factory)` during `init()`, so adding a custom codec only requires writing a struct that satisfies the `Codec` interface and registering it at startup.

Message bodies are Gzip-compressed by default. The `CompressionType` field in the protocol header controls this per message, and the compression subsystem is also registry-based (currently supporting `None` and `Gzip`).

## Configuration Options

Server options:

| Option                                           | Description                 |
| ------------------------------------------------ | --------------------------- |
| `rpc.WithCodec(rpc.CodecJSON \| rpc.CodecProto)` | Set the serialization codec |

Client dial options:

| Option                                      | Description                               |
| ------------------------------------------- | ----------------------------------------- |
| `rpc.WithTimeout(d time.Duration)`          | Per-RPC timeout (default 5 s)             |
| `rpc.WithDialCodec(t rpc.CodecType)`        | Serialization codec                       |
| `rpc.WithRegistry(reg *rpc.Registry)`       | Enable registry-based discovery           |
| `rpc.WithLoadBalancer(lb rpc.LoadBalancer)` | Load balancing strategy for registry mode |

## Architecture

```
+---------------------------------------------------------+
|                    pkg/rpc (Public API)                  |
|   Server   ClientConn   ServerStream   ClientStream     |
+--------+---------------------------------------+--------+
         |                                       |
+--------v----------+                  +---------v--------+
| internal/server   |                  | internal/client  |
| Reflection-based  |                  | Circuit Breaker  |
| dispatch + stream |                  | Rate Limiter     |
+--------+----------+                  | Load Balancer    |
         |                             | Connection Pool  |
         |                             +---------+--------+
         |                                       |
         +------------------+--------------------+
                            |
                   +--------v--------+
                   | internal/       |
                   |  transport      |
                   | TCPClient       |
                   | ConnectionPool  |
                   | Future / Stream |
                   +--------+--------+
                            |
                   +--------v--------+
                   | internal/       |
                   |  protocol       |
                   | Header + Message|
                   +--------+--------+
                            |
                   +--------v--------+
                   | internal/codec  |
                   | JSON / Protobuf |
                   | Gzip Compress   |
                   +-----------------+
```

### Wire Protocol

Every frame on the wire has the following layout:

```
+--------+-----------+---------+----------------+--------------+
| Magic  | HeaderLen | BodyLen |  Header (JSON) | Body (bytes) |
| 2 byte |  4 byte   |  4 byte |    N bytes     |   M bytes    |
+--------+-----------+---------+----------------+--------------+
```

- Magic (`0x1234`): a two-byte constant used to identify valid frames and recover from byte-stream misalignment.
- HeaderLen / BodyLen: big-endian unsigned 32-bit integers giving the byte length of the Header and Body sections respectively.
- Header: always JSON-encoded regardless of the chosen body codec. Carries control metadata including `RequestID`, `ServiceName`, `MethodName`, `CodecType`, `Compression`, `StreamFlag`, and `Error`.
- Body: serialized with the configured codec (JSON or Protobuf) and optionally Gzip-compressed.

`RequestID` is a per-connection monotonically increasing uint64 that enables multiplexing of requests and responses on a single TCP connection. `StreamFlag` distinguishes between regular unary responses (`None`), streaming data frames (`Data`), end-of-stream markers (`End`), and error frames (`Error`).

### Transport Layer

The transport layer sits directly above raw TCP and provides several abstractions:

- `TCPConnection`: wraps a `net.Conn` with a buffered reader and a `PacketBuffer` that accumulates partial reads, scans for the magic number, and extracts complete frames. Write operations are serialized with a mutex to prevent interleaving.
- `TCPClient`: manages a single TCP connection and exposes asynchronous request/response semantics. Each outgoing message is assigned a unique sequence number. A background `readLoop` goroutine continuously reads frames from the connection and dispatches them to the correct `Future` (for unary calls) or `ClientStreamConn` (for streaming calls) based on the `RequestID`.
- `Future`: a promise-style primitive used for asynchronous unary calls. Supports blocking wait, context-aware wait with cancellation, timeout-based wait, completion callbacks via `OnComplete`, and direct deserialization into a reply struct via `GetResult` / `GetResultWithContext`.
- `ClientStreamConn`: a context-aware channel-based abstraction for client-side stream consumption. Data frames are pushed into a buffered channel (capacity 64) and consumed via `Recv`. End-of-stream and error signals are delivered through the same channel, guarded by `sync.Once` to prevent duplicate termination. Supports context cancellation for early stream teardown.
- `ConnectionPool`: maintains a bounded set of `TCPClient` instances for a given address. Connections are lazily created up to `maxActive`. Closed connections are detected and evicted on `Acquire`, with a round-robin index for selection among live connections.

### Server Internals

- `Server`: accepts TCP connections on a `net.Listener`, wraps each in a `TCPConnection`, and dispatches incoming messages to the `Handler`. Each connection is handled in its own goroutine, tracked via a `sync.WaitGroup` for graceful shutdown. A server-side token-bucket rate limiter (default 10,000 tokens/s, refilled every second) rejects excess requests before dispatch.
- `Handler`: performs reflection-based method dispatch. It inspects the registered service struct's exported methods and matches them against three supported signatures (grpc-go unary, net/rpc unary, and server streaming). For streaming methods, the handler creates a `serverStream` that sends data, end, and error frames back to the client on the same connection, tagged with the originating `RequestID`.
- `GracefulStop` closes the listener, waits for all connection-handling goroutines to finish (draining in-flight requests), then stops the rate limiter. `Stop` closes the listener and forcibly closes all tracked connections.

### Client Internals (Registry Mode)

When `WithRegistry` is passed to `Dial`, the `ClientConn` delegates to `internal/client.Client`, which layers several resilience mechanisms on top of the transport:

- Service Discovery: on each call, `getAddr` queries the `Registry.Discover` method (backed by etcd) to get the current set of instances for the target service, then passes them to the configured `LoadBalancer.Select`.
- Circuit Breaker: created lazily per `service|addr` pair. Uses a sliding-window model with configurable window size (default 10), failure-rate threshold (default 60%), and open-state timeout (default 5 s). State transitions: Closed -> Open when the failure rate within a window exceeds the threshold; Open -> HalfOpen after the timeout expires, allowing one probe request; HalfOpen -> Closed on probe success, or back to Open on probe failure.
- Rate Limiter: a client-side token-bucket limiter (default 10,000 tokens/s) that rejects calls before they reach the network.
- Connection Pooling: a per-address `ConnectionPool` is created lazily via `sync.Map.LoadOrStore`, so multiple goroutines share the same pool for a given backend.
- Async Invocation: `invokeAsync` sends the request and returns a `Future` immediately. A completion callback feeds the result into the circuit breaker (`RecordSuccess` / `RecordFailure`). The synchronous `Invoke` wraps `invokeAsync` with `context.WithTimeout`.

### Service Registry (etcd)

The `Registry` connects to an etcd v3 cluster and stores service instances under a key prefix (`/github.com/hangtiancheng/yukino.go/yukino_rpc/services/<service>/<addr>`).

- `Register`: creates a lease with the specified TTL, puts the instance key, and starts a background goroutine that drains the `KeepAlive` channel to maintain the lease.
- `Discover`: returns the cached instance list if available; otherwise performs a one-time `Get` with prefix, populates the local cache, and starts a `Watch` goroutine for that service.
- `Watch`: listens to etcd events for the service prefix. `PUT` events add or update instances; `DELETE` events remove them. If the watch channel closes unexpectedly, it reconnects after a one-second backoff.

### Load Balancing

All balancers implement the `LoadBalancer` interface:

```go
type LoadBalancer interface {
    Select([]registry.Instance) registry.Instance
}
```

- `RoundRobin` (default): lock-free atomic counter, distributes calls evenly across instances.
- `Random`: mutex-protected `math/rand` source for uniform random selection.
- `WeightedRR`: smooth weighted round-robin. Accepts a weight slice at construction time and adjusts current weights on each selection so that higher-weighted instances receive proportionally more traffic while maintaining even distribution over time.

### Rate Limiting

The `TokenBucket` rate limiter maintains a token count that is reset to the configured rate every second via a background goroutine and ticker. Each `Allow()` call atomically decrements the count; when tokens are exhausted the call is rejected. Both the server and the registry-mode client instantiate their own limiter (default 10,000 req/s).

## Method Signatures

After calling `Register(name, service)`, the framework reflects over the exported methods of `service` and recognizes the following three signatures:

```go
// 1) grpc-go style unary (recommended)
func (s *Svc) Method(ctx context.Context, req *T) (*R, error)

// 2) net/rpc style unary (backward compatible)
func (s *Svc) Method(req *T, reply *R) error

// 3) Server-side streaming
func (s *Svc) Method(req *T, stream rpc.ServerStream) error
```

Methods that do not match any of these signatures are silently ignored. Invoking an unmatched method returns an `unsupported method signature` error.

## Error Handling

Errors returned by service methods are serialized into the `Header.Error` field and surfaced on the client side as `errors.New(headerError)` from `Invoke`. Framework-level errors include:

| Error                                              | Description                                          |
| -------------------------------------------------- | ---------------------------------------------------- |
| `service not found: <Service>`                     | The requested service name was not registered        |
| `method not found: <Service>.<Method>`             | The method does not exist on the service             |
| `unsupported method signature: <Service>.<Method>` | The method signature is not recognized               |
| `rate limit exceeded`                              | Rejected by the token-bucket rate limiter            |
| `circuit breaker open`                             | Rejected by the circuit breaker                      |
| `no instance available`                            | The registry returned zero instances for the service |
| `connection closed`                                | The underlying TCP connection was already closed     |

## Project Structure

```
yukino_rpc/
├── pkg/
│   ├── rpc/                 Public API (grpc-go-style facade)
│   │   ├── rpc.go           Type aliases, NewRegistry, codec constants
│   │   ├── server.go        Server, NewServer, Serve, GracefulStop, Stop
│   │   ├── client.go        ClientConn, Dial, Invoke, NewStream, Close
│   │   └── stream.go        ServerStream / ClientStream type aliases
│   └── api/                 Example service (Arith) and message types
└── internal/
    ├── server/              TCP server, reflection-based dispatch, stream support
    ├── client/              Registry-mode client with breaker, limiter, LB, pool
    ├── transport/           TCPConnection, ConnectionPool, TCPClient, Future, Stream
    ├── protocol/            Header, Message, binary encode/decode
    ├── codec/               Codec interface, JSON / Protobuf / Gzip implementations
    ├── breaker/             Three-state circuit breaker
    ├── limiter/             Token-bucket rate limiter
    ├── load_balance/        RoundRobin / Random / WeightedRR balancers
    ├── registry/            etcd v3 service registration and discovery
    └── stream/              ServerStream / ClientStream interface definitions
```

## Running Tests

```bash
cd yukino_rpc
go test ./...
```

End-to-end integration tests for server streaming are in `pkg/rpc/stream_test.go`, covering normal streaming, mid-stream errors, mixed stream-and-unary calls, and context cancellation.

## Dependencies

- `go.etcd.io/etcd/client/v3` -- service registration and discovery
- `google.golang.org/protobuf` -- Protobuf codec

## Roadmap

- Interceptors (Unary / Stream), unifying rate limiting, circuit breaking, logging, metrics, and tracing as pluggable middleware
- Client-side streaming and bidirectional streaming
- Expose circuit breaker and rate limiter parameters as configurable options
- OpenTelemetry and Prometheus integration
- Proto-based code generator for strongly typed client/server stubs
- Registry abstraction layer to support Nacos, Consul, Kubernetes Service, and others

## License

See the LICENSE file in the repository root.
