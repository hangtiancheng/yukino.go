<div align="center">

# Yukino.go

**A family of elegant, production-oriented Go infrastructure libraries.**

Four independent modules — HTTP framework, MongoDB ORM, distributed cache, and RPC framework — each inspired by a best-in-class design, each usable on its own.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

</div>

---

## Modules

| Module                           | What it is        | Inspired by                                        | Key traits                                                                                                                   |
| -------------------------------- | ----------------- | -------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| [`yukino_http`](./yukino_http)   | HTTP framework    | [Koa.js](https://koajs.com)                        | Deferred response, Trie routing, middleware onion, WebSocket (RFC 6455), SSE — zero external dependencies                    |
| [`yukino_orm`](./yukino_orm)     | MongoDB ORM       | [Knex.js](https://knexjs.org)                      | Chainable query builder, transactions, grouped aggregation, streaming, auto-increment sequences                              |
| [`yukino_cache`](./yukino_cache) | Distributed cache | [groupcache](https://github.com/golang/groupcache) | Read-through + singleflight, write propagation, dual-level LRU, consistent hashing, etcd discovery, live dashboard           |
| [`yukino_rpc`](./yukino_rpc)     | RPC framework     | [grpc-go](https://github.com/grpc/grpc-go)         | grpc-go-style API over a custom TCP binary protocol, multiplexing, streaming, circuit breaker, rate limiting, load balancing |

The modules are independently versioned and importable. The only cross-module dependency is optional: `yukino_cache`'s real-time dashboard is served through `yukino_http`.

---

## yukino_http

Koa-style HTTP framework with a **deferred response** model: middleware sets `ctx.Status` / `ctx.Body`, and the response is written only after the whole onion chain completes — downstream middleware can still inspect and modify it after `next()` returns.

```bash
go get github.com/hangtiancheng/yukino.go/yukino_http
```

```go
package main

import (
  "net/http"

  yukino "github.com/hangtiancheng/yukino.go/yukino_http"
)

func main() {
  app := yukino.Default() // Logger + Recovery
  app.Get("/", func(ctx *yukino.Context, next func()) {
    ctx.Status = http.StatusOK
    ctx.String("Hello World")
  })
  app.Listen(":8000")
}
```

Highlights:

- Trie-based routing with `:param`, `*wildcard`, route groups, and static file serving
- Middleware composition with built-in `Logger` and `Recovery`
- WebSocket implemented from scratch (RFC 6455) — event-driven and imperative APIs, heartbeat support
- Server-Sent Events with heartbeat and channel streaming
- Graceful shutdown via `app.Shutdown(ctx)`

Full documentation: [`yukino_http/README.md`](./yukino_http/README.md)

---

## yukino_orm

A lightweight MongoDB ORM built around a **Knex-style chainable query builder** on top of the official `mongo-driver`.

```bash
go get github.com/hangtiancheng/yukino.go/yukino_orm
```

```go
engine, err := yukino_orm.NewEngine(ctx, "mongodb://localhost:27017", "demo")
if err != nil {
  log.Fatal(err)
}
defer engine.Close(ctx)

var adults []User
err = engine.Model(&User{}).            // collection name derived: "users"
  Where("age", ">=", 18).
  WhereNotNull("email").
  OrderBy("created_at", "desc").
  Limit(10).
  Find(ctx, &adults)
```

Highlights:

- Rich predicates: `Where` / `WhereIn` / `WhereBetween` / `WhereNull` / `WhereLike` and all `Or*` variants; same-field conditions always AND-combine, never silently overwrite
- Automatic `$set` wrapping for plain documents and structs
- Aggregation: `Count` / `Sum` / `Avg` / `Min` / `Max` / `Distinct` / `Pluck`, plus `GroupBy` + `Having` grouped aggregation
- Transactions with automatic session binding, auto-increment sequences, index management
- Streaming large result sets via `Cursor` / `Each`

Full documentation: [`yukino_orm/README.md`](./yukino_orm/README.md)

---

## yukino_cache

A distributed caching framework that keeps groupcache's `Group + Getter` mental model and extends it for microservice scenarios: **write propagation**, **etcd service discovery**, and a **real-time WebSocket dashboard**.

```bash
go get github.com/hangtiancheng/yukino.go/yukino_cache
```

```go
scores := cache.NewGroup("scores", 64<<20, cache.GetterFunc(
  func(ctx context.Context, key string) ([]byte, error) {
    return loadFromDB(ctx, key) // invoked on cache miss
  },
), cache.WithExpiration(5*time.Minute))
defer scores.Close()

view, err := scores.Get(ctx, "player:42") // read-through, singleflight-deduplicated
_ = scores.Set(ctx, "player:42", []byte("9999"))
_ = scores.Delete(ctx, "player:42")
```

Highlights:

- Read-through with singleflight to prevent cache stampede; `Set` / `Delete` asynchronously sync to the owning peer
- Bucket-sharded dual-level LRU (L1 → L2 promotion) with TTL and background cleanup
- Consistent hashing with virtual nodes (groupcache semantics)
- Distributed mode: gRPC transport + etcd-based peer discovery with lease keepalive and watch
- Optional WebSocket dashboard streaming live group statistics and cache entries

Full documentation: [`yukino_cache/README.md`](./yukino_cache/README.md)

---

## yukino_rpc

A lightweight RPC framework with a **grpc-go-aligned public API** (`NewServer` / `Register` / `Serve` / `Dial` / `Invoke`) running over a custom TCP binary protocol with full request multiplexing.

```bash
go get github.com/hangtiancheng/yukino.go/yukino_rpc
```

```go
// Server
server := rpc.NewServer()
server.Register("Arith", &api.Arith{})
lis, _ := net.Listen("tcp", ":8080")
_ = server.Serve(lis)

// Client
conn, _ := rpc.Dial("127.0.0.1:8080", rpc.WithTimeout(3*time.Second))
defer conn.Close()

var reply api.Reply
_ = conn.Invoke(ctx, "Arith", "Add", &api.Args{A: 1, B: 2}, &reply)
```

Highlights:

- Three method signatures via reflection: grpc-go unary, net/rpc unary, and server-side streaming (`Send` / `Recv`)
- Pluggable codecs (JSON default, Protobuf) with Gzip compression; registry-based extension
- Resilience built in: circuit breaker (Closed / Open / HalfOpen), token-bucket rate limiting, connection pooling
- Load balancing: RoundRobin, Random, smooth Weighted Round-Robin — or your own `LoadBalancer`
- etcd v3 service registration and discovery with real-time watch

Full documentation: [`yukino_rpc/README.md`](./yukino_rpc/README.md)

---

## Repository Layout

```
yukino.go/
├── go.work              # Go workspace spanning all modules
├── yukino_http/         # HTTP framework        github.com/hangtiancheng/yukino.go/yukino_http
├── yukino_orm/          # MongoDB ORM           github.com/hangtiancheng/yukino.go/yukino_orm
├── yukino_cache/        # Distributed cache     github.com/hangtiancheng/yukino.go/yukino_cache
├── yukino_rpc/          # RPC framework         github.com/hangtiancheng/yukino.go/yukino_rpc
├── scripts/             # Maintenance tooling (license headers, renaming)
└── tag.js               # Tag every module at once for release
```

Each module is a standalone Go module with its own `go.mod`, tests, and README. The workspace (`go.work`) wires them together for local development via `replace` directives.

## Development

Prerequisites and recommended tooling:

```bash
git config --global core.fileMode false
go env -w GOPROXY=https://goproxy.cn,direct
go install github.com/air-verse/air@latest

# golangci-lint
brew install golangci-lint          # macOS
choco install golangci-lint         # Windows (Chocolatey)
scoop install main/golangci-lint    # Windows (Scoop)
```

Workspace scripts (pnpm):

```bash
pnpm lint          # run golangci-lint across all modules
pnpm lint:fix      # lint with auto-fix
pnpm tag           # tag all modules with the package.json version
```

Run tests per module:

```bash
cd yukino_http && go test ./...
cd yukino_orm && go test ./...    # uses mongodb://localhost:27017 (MONGO_URI to override)
cd yukino_cache && go test ./...
cd yukino_rpc && go test ./...
```

## Versioning

Modules are released independently with per-module git tags (`yukino_http/v0.0.1`, `yukino_rpc/v0.0.1`, ...). `node tag.js` reads the version from `package.json` and tags every module in one shot; individual modules can be pinned with `node tag.js --cache=v0.1.0 --rpc=v0.2.0`.

## License

[MIT](./LICENSE) © hangtiancheng
