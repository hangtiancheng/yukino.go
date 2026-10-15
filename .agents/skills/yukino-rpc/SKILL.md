---
name: yukino-rpc
description: >
  Build, modify, or debug the Go TCP RPC module
  github.com/hangtiancheng/yukino.go/libs/yukino_rpc.
  Use for its pkg/rpc client/server APIs, reflection handlers, server streams,
  binary framing, codecs, etcd registry, discovery, balancing, and resilience.
  This skill targets yukino_rpc rather than grpc-go.
---

# yukino_rpc

Source root: `libs/yukino_rpc/`. The public framework import is
`github.com/hangtiancheng/yukino.go/libs/yukino_rpc/pkg/rpc`.
`pkg/api` contains sample services; the implementation is under `internal/`.
Transport is a custom TCP protocol. Read the relevant implementation before
changing behavior; dependencies and toolchain requirements come from
`go.mod` and the repository's `go.work`.

## Source guide

| Work                                                       | Read                                                                                                                                                                                                                                                        |
| ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Public aliases, Dial/options, Invoke/InvokeAsync/NewStream | [pkg/rpc/rpc.go](../../../libs/yukino_rpc/pkg/rpc/rpc.go), [pkg/rpc/client.go](../../../libs/yukino_rpc/pkg/rpc/client.go), [pkg/rpc/stream.go](../../../libs/yukino_rpc/pkg/rpc/stream.go)                                                                 |
| Public server, reflected method dispatch, shutdown         | [pkg/rpc/server.go](../../../libs/yukino_rpc/pkg/rpc/server.go), [internal/server/handler.go](../../../libs/yukino_rpc/internal/server/handler.go), [internal/server/server.go](../../../libs/yukino_rpc/internal/server/server.go)                         |
| Wire encoding and body codecs                              | [internal/protocol](../../../libs/yukino_rpc/internal/protocol), [internal/codec](../../../libs/yukino_rpc/internal/codec)                                                                                                                                  |
| Futures, streams, framing, connection reuse                | [internal/transport](../../../libs/yukino_rpc/internal/transport)                                                                                                                                                                                           |
| Registry-mode call paths and discovery                     | [internal/client](../../../libs/yukino_rpc/internal/client), [internal/registry/registry.go](../../../libs/yukino_rpc/internal/registry/registry.go)                                                                                                        |
| Balancers, circuit breaker, limiter                        | [internal/load_balance](../../../libs/yukino_rpc/internal/load_balance), [internal/breaker/breaker.go](../../../libs/yukino_rpc/internal/breaker/breaker.go), [internal/limiter/token_bucket.go](../../../libs/yukino_rpc/internal/limiter/token_bucket.go) |

## Public API and mode selection

`Dial(target, opts...) (*ClientConn, error)` validates the codec and builds
a lazy connection pool; it does not establish the TCP connection.
Options are `WithTimeout`, `WithDialCodec`, `WithRegistry`, and
`WithLoadBalancer`. Defaults are JSON and five seconds.

| Mode                               | Behavior                                                                                                                                                                   |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Static, without a non-nil registry | Uses target directly and one pool. No client limiter, circuit breaker, discovery, or load-balancer selection.                                                              |
| Registry, with `WithRegistry(reg)` | Ignores target, discovers by service name on each call, uses the default round-robin or supplied LoadBalancer, and creates address pools and per-service/address breakers. |

`WithTimeout(d <= 0)` is rejected in registry mode; static mode accepts it
and its context-bounded operations expire immediately. Pass a positive value.
Built-in balancer types live under `internal/`; external consumers can
implement the public `LoadBalancer` alias using
`Select([]rpc.Instance) rpc.Instance`.

- `Invoke(ctx, service, method, args, reply) error` is unary and adds the
  configured timeout to the caller context.
- `InvokeAsync(ctx, service, method, args) (*Future, error)` returns after
  sending and starts a configured-timeout watchdog. Once sent, later caller
  cancellation is not monitored by that watchdog.
  Use `GetResultWithContext` or `WaitWithContext` for a bounded local wait.
- `Future.GetResult` decodes; `Wait` returns raw decompressed bytes.
  `DoneChan` signals completion. `OnComplete` is one callback slot:
  replacing it on a registry future overwrites breaker accounting.
- `NewStream(ctx, service, method, args) (ClientStream, error)` opens a
  server stream. Its lifetime is governed by the caller context, not
  `WithTimeout`. The public interface has `Recv(any) error` and
  `Context()`; cancel the supplied context to abandon it.
- `ClientConn.Close()` closes pools and stops its client limiter, but
  does not close a supplied Registry. Close the registry through its owner.

## Server methods and codec behavior

`NewServer(opts...) *Server` defaults to JSON and panics on invalid codec
options. `WithCodec` sets the fallback for a request whose CodecType is zero;
a non-zero request CodecType selects the codec per request.

`Register(name, service)` stores the supplied receiver and overwrites a
previous registration under the same name. It does not validate methods.
Register a pointer when handlers have pointer receivers. Calls resolve
exported methods by name and accept these shapes:

```text
Method(ctx context.Context, req *Request) (*Reply, error)
Method(req *Request, reply *Reply) error
Method(req *Request, stream rpc.ServerStream) error
```

The stream interface exposes `Send(any) error` and `Context()`.
Only server streaming is implemented. Handler panics during the reflective
call become error responses. Unary handlers run synchronously in the
connection read loop; streaming handlers run in separate goroutines.
A slow unary call delays later requests on that connection.

- Server handler contexts originate from `context.Background()`.
  Client cancellation/deadlines are not transmitted and do not cancel server
  work; a server stream's Context does not signal client disconnection.
- Both unary handler forms dereference the reply before marshaling.
  A nil reply from the context-style handler becomes an empty body, which
  cannot be decoded as JSON.
- `CodecProto` requires `proto.Message` values. The unary dereference
  strips the pointer required by ordinary generated protobuf messages,
  causing `proto codec: not proto.Message` on response encoding.
  Do not infer working protobuf unary calls from codec registration alone.
  Streaming Send marshals the supplied value directly.
- Errors crossing the wire are plain Header.Error strings reconstructed
  with `errors.New`; server error types and identities are not preserved.

## Framing, timeouts, and stream behavior

Frames contain a ten-byte prefix:
big-endian `uint16 Magic = 0x1234`, `uint32 HeaderLen`,
`uint32 BodyLen`, then a JSON header and encoded body.
Header fields include RequestID, ServiceName, MethodName, Error, CodecType,
Compression, and optional StreamFlag. Framework send paths use gzip bodies;
no public compression option is exposed.

- Responses are demultiplexed by RequestID and StreamFlag:
  None=0 for unary, Data=1, End=2, Error=3 for streams.
  A stream request has no distinguishing request flag; the reflected server
  signature decides the response convention. Match Invoke/NewStream to that
  signature.
- Dispatch errors before a stream handler starts, including unknown
  service/method and server rate rejection, are sent as unary error frames.
  The client stream map does not receive them, so Recv can wait until its
  context ends. Give stream calls deadlines.
- Each address pool currently allows one TCP connection. Stream data uses
  a 64-frame buffer; a full buffer blocks the shared client read loop,
  delaying other responses on the socket. Consume promptly.
- New TCP dials use a fixed five-second `net.DialTimeout` while holding
  the pool mutex. Acquire checks context only before that dial, and socket
  writes have no per-call write deadline. `WithTimeout` does not bound
  every phase of a call.
- Unary timeouts do not remove the corresponding pending-map entry;
  the late response or connection shutdown removes it.
- There is no application heartbeat. TCP closure fails pending futures and
  streams. Socket Close sets `SetLinger(0)` for TCP connections.

## Registry and resilience

`NewRegistry(endpoints)` creates an etcd client.
`Register(service, rpc.Instance{Addr: addr}, ttl)` writes a leased address
under `/github.com/hangtiancheng/yukino.go/libs/yukino_rpc/services/`;
TTL is in seconds. Keepalive closure ends its drain goroutine without
re-registering. `Registry.Close()` cancels its work and closes the etcd client.

`Discover` initially loads a service snapshot and starts a watch, then
returns copied map values. The list order is unspecified. The watch restarts
after closure without a full snapshot reload or snapshot-revision continuation.
Its cache-hit path takes an RLock and calls a helper that takes RLock again;
a queued writer between the acquisitions can deadlock that path.
When changing discovery, inspect these lock and resynchronization paths.

- Default round-robin selects by list position, so unspecified discovery
  order prevents a stable endpoint rotation. Internal WeightedRR also binds
  weights to positions and rejects a different list length.
- Pools and per-service/address breakers are retained until the client is
  closed; discovery removal does not evict them.
- The server and registry client each have a quota of 10000 requests,
  replenished in full on a one-second ticker. Despite the TokenBucket name,
  the implementation is a periodic full reset, not continuous refill.
- Registry client breakers use ten recorded outcomes, failure ratio 0.6,
  and five seconds open, then admit one half-open probe.
  Unary outcomes are recorded via futures; stream outcomes are recorded
  when Recv observes EOF or an error, excluding `context.Canceled`.

`Serve(listener)` accepts TCP connections. `GracefulStop()` closes the
listener, interrupts idle reads, and waits for active handlers/streams,
without an internal deadline. `Stop()` closes sockets without waiting for
handlers; it can be used as a timed fallback. Stop the server to release
its limiter. Persistent non-shutdown accept errors are retried without
backoff and are not returned from Serve.

## Example and verification

```go
package rpcexample

import (
    "context"

    rpc "github.com/hangtiancheng/yukino.go/libs/yukino_rpc/pkg/rpc"
)

type Args struct{ A, B int }
type Reply struct{ Result int }
type Math struct{}

func (*Math) Add(ctx context.Context, args *Args) (*Reply, error) {
    return &Reply{Result: args.A + args.B}, nil
}

func NewMathServer() *rpc.Server {
    server := rpc.NewServer()
    server.Register("Math", &Math{})
    return server
}

func Add(ctx context.Context, conn *rpc.ClientConn, a, b int) (int, error) {
    var reply Reply
    err := conn.Invoke(ctx, "Math", "Add", &Args{A: a, B: b}, &reply)
    return reply.Result, err
}
```

The server's owner supplies a `net.Listener` to Serve and shuts it down.
The client is obtained with `rpc.Dial(addr)` and closed by its owner.
From `libs/yukino_rpc`, run `go test ./...`.
Existing tests use ephemeral loopback listeners, `net.Pipe`, and offline
registry maps; they do not require a running etcd cluster.
Use deadlines in streaming tests and verify both handler shape and call mode.
