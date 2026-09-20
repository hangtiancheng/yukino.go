# Yukino HTTP

Go HTTP framework, inspired by Koa.js. Features include deferred response, Trie-based routing, middleware composition, WebSocket (RFC 6455), and Server-Sent Events. Zero external dependencies.

## Quick Start

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

## Install

```bash
go get github.com/hangtiancheng/yukino.go/yukino_http
```

Requires Go 1.21+.

## Architecture

```
Request
  -> http.Server.ServeHTTP
    -> Router.handle(ctx, middlewares)
      -> compose(middlewares, handler)   // Koa-style onion model
        -> Logger -> Recovery -> ... -> Handler
      -> ctx.respond()                  // deferred response: writes Status + Body + Headers
```

The response is not written during middleware execution. Middleware sets `ctx.Status`, `ctx.Body`, and headers via `ctx.Set()`. After the entire chain completes, `ctx.respond()` serializes and writes the HTTP response. This is the "deferred response" pattern from Koa.js -- downstream middleware can inspect or modify the response after `next()` returns.

For WebSocket and SSE, `ctx.flushed` is set to `true` during upgrade/initialization. `ctx.respond()` checks this flag and skips writing, since the connection has already been hijacked or headers have already been sent.

## Application

```go
app := yukino.New()       // bare Application
app := yukino.Default()   // with Logger + Recovery

go func() {
    if err := app.Listen(":8000"); err != nil && err != http.ErrServerClosed {
        log.Fatal(err)
    }
}()

// graceful shutdown
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
app.Shutdown(ctx)
```

## Routing

Trie-based router with dynamic segments (`:param`) and wildcard (`*path`). Exact routes take priority over wildcards.

```go
app.Get("/users/:id", handler)         // /users/123 -> ctx.Param("id") == "123"
app.Post("/api/*filepath", handler)    // /api/a/b/c -> ctx.Param("filepath") == "a/b/c"
app.All("/any", handler)               // all HTTP methods
app.Static("/static", "./public")      // serve static files
```

Route groups with prefix and shared middleware:

```go
api := app.Router("/api")
api.Use(authMiddleware)

v1 := api.Router("/v1")
v1.Get("/users/:id", getUser)    // GET /api/v1/users/:id
v1.Post("/users", createUser)    // POST /api/v1/users

v2 := api.Router("/v2")
v2.Use(rateLimit)
v2.Get("/users/:id", getUserV2)  // GET /api/v2/users/:id
```

Each Router holds its own middleware stack. Parent middleware runs before child middleware. Path boundary matching is exact -- `/v1` middleware won't fire for `/v10`.

## Middleware

Signature: `func(ctx *yukino.Context, next func())`

```go
func Timer() yukino.Middleware {
    return func(ctx *yukino.Context, next func()) {
        start := time.Now()
        next()
        log.Printf("%s %s -> %d (%v)", ctx.Method, ctx.Path, ctx.Status, time.Since(start))
    }
}

app.Use(Timer())
```

Not calling `next()` short-circuits the chain. Code after `next()` runs after downstream middleware completes (onion model). Because the response is deferred, post-`next()` code can still modify `ctx.Status` / `ctx.Body`.

Built-in:

- `Logger()` -- logs `[status] uri in duration`
- `Recovery()` -- catches panics, prints stack trace, returns 500

## Context

```go
// Request
ctx.Request            // *http.Request
ctx.Path               // request path
ctx.Method             // HTTP method
ctx.Query("key")       // query parameter
ctx.Param("key")       // route parameter (:param or *wildcard)
ctx.PostForm("key")    // form value
ctx.Get("Header")      // request header
ctx.BindJSON(&v)       // decode JSON body
ctx.FormFile("key")    // uploaded file

// Response (deferred -- written after middleware chain completes)
ctx.Status = 200
ctx.Set("X-Custom", "value")
ctx.JSON(obj)                    // application/json
ctx.String("hello %s", name)    // text/plain
ctx.Data([]byte{...})           // raw bytes
ctx.HTML("template", data)      // html/template
ctx.Redirect("/other")          // 302
ctx.Throw(400, "bad request")   // set status + JSON error body

// State sharing between middleware
ctx.State["user"] = currentUser
```

When `ctx.Body` is set but `ctx.Status` is not explicitly set, the framework auto-corrects the default 404 to 200, matching Koa behavior.

## WebSocket

RFC 6455 implementation from scratch. No external dependencies.

Two API styles: event-driven (Node.js ws style) and imperative (Go style).

### Event-driven

```go
app.Get("/ws", func(ctx *yukino.Context, next func()) {
    ws, err := ctx.Upgrade(&yukino.UpgradeOptions{
        CheckOrigin: func(r *http.Request) bool { return true },
    })
    if err != nil {
        return
    }

    ws.OnMessage(func(mt int, data []byte) {
        ws.Send("echo:" + string(data))
    })
    ws.OnClose(func(code int, reason string) {
        log.Printf("closed: %d", code)
    })
    ws.OnError(func(err error) {
        log.Printf("error: %v", err)
    })
    go ws.Listen()
})
```

### Imperative

```go
app.Get("/ws", func(ctx *yukino.Context, next func()) {
    ws, err := ctx.Upgrade(nil)
    if err != nil {
        return
    }
    go func() {
        defer ws.Close()
        for {
            mt, data, err := ws.ReadMessage()
            if err != nil {
                break
            }
            ws.WriteMessage(mt, data)
        }
    }()
})
```

### With heartbeat

```go
ws, _ := ctx.Upgrade(nil)
stopHeartbeat := ws.Heartbeat(30 * time.Second)
defer stopHeartbeat()
```

### WSConn API

Upgrade:

```
ctx.Upgrade(opts)         hijack connection, perform WebSocket handshake, return *WSConn
```

Event handlers (register before calling Listen):

```
ws.OnMessage(fn)          func(messageType int, data []byte)
ws.OnClose(fn)            func(code int, reason string)
ws.OnError(fn)            func(err error)
ws.OnPing(fn)             func(data []byte)
ws.OnPong(fn)             func(data []byte)
ws.Listen()               blocking read loop, dispatches to handlers above
```

Read/Write:

```
ws.ReadMessage()          (messageType int, data []byte, err error)
ws.ReadJSON(v)            read + JSON unmarshal
ws.WriteMessage(mt, data) write a frame
ws.WriteJSON(v)           JSON marshal + write
ws.Send(text)             write text message
ws.WriteText(text)        write text message
ws.WriteBinary(data)      write binary message
```

Control:

```
ws.Ping()                 send ping frame
ws.Heartbeat(interval)    periodic ping, returns stop function
ws.Close()                send close frame + close connection
ws.CloseWithMessage(c, t) close with specific code and reason
ws.Closed()               <-chan struct{}, signals connection end
ws.SetReadDeadline(t)     deadline on underlying connection
ws.SetWriteDeadline(t)    deadline on underlying connection
ws.NetConn()              access underlying net.Conn
```

## Server-Sent Events

```go
app.Get("/events", func(ctx *yukino.Context, next func()) {
    sse := ctx.SSE()
    sse.Event("greeting", "hello")   // named event
    sse.Data("plain data")           // unnamed event
    sse.JSON("update", obj)          // JSON event
    sse.Done()                       // send [DONE] marker
})
```

Long-lived streaming with heartbeat:

```go
app.Post("/chat", func(ctx *yukino.Context, next func()) {
    sse := ctx.SSE()
    stop := sse.Heartbeat(15 * time.Second)
    defer stop()

    ch := make(chan string)
    go produceMessages(ch)
    sse.Stream(ch)  // blocks until channel closes or client disconnects
})
```

### SSEWriter API

```
ctx.SSE()                 set event-stream headers, return *SSEWriter
sse.Event(name, data)     send named event
sse.Data(data)            send data event (multi-line safe)
sse.JSON(name, obj)       send JSON event
sse.ID(id)                set event ID
sse.Retry(ms)             set reconnect interval
sse.Comment(text)         comment line
sse.Heartbeat(interval)   periodic keepalive, returns stop function
sse.Stream(ch)            consume channel until closed or client disconnects
sse.Closed()              client disconnect signal
sse.Flush()               manual flush
sse.Done()                send [DONE] marker
```

## File Structure

```
yukino_http/
  yukino.go        Application, compose(), ServeHTTP
  context.go     Context, request accessors, state
  response.go    deferred response rendering
  group.go       Router groups, Static, path boundary matching
  router.go      internal router, dispatch
  trie.go        Trie node for route matching
  websocket.go   WebSocket (RFC 6455)
  sse.go         Server-Sent Events
  logger.go      Logger middleware
  recovery.go    Recovery middleware
```

## WebSocket Implementation Notes

This section documents the technical decisions and a pitfall encountered during implementation.

### Upgrade Process

1. Validate WebSocket request headers (Connection: upgrade, Upgrade: websocket, Sec-WebSocket-Key)
2. Call `http.NewResponseController(w).Hijack()` to take over the TCP connection from `http.Server`
3. Clear all deadlines via `conn.SetDeadline(time.Time{})` -- the HTTP server may have set read/write deadlines that would expire on the long-lived WebSocket connection
4. Reuse the hijacked `bufio.Reader` if its buffer is large enough; otherwise allocate a new one
5. Compute `Sec-WebSocket-Accept` as `base64(sha1(key + GUID))` per RFC 6455 Section 4.2.2
6. Write the 101 Switching Protocols response directly via `conn.Write()`, bypassing bufio.Writer
7. Return `*WSConn` wrapping the raw `net.Conn`

### Frame I/O

Reads go through `bufio.Reader` for efficiency -- small frame headers benefit from buffered reads. Writes go directly to `conn.Write()` following the Gorilla WebSocket pattern. Each write sends header + payload in a single `conn.Write()` call to avoid TCP fragmentation of partial frames.

Server frames are unmasked (RFC 6455 Section 5.1). Client frames are masked and the server unmasks them on read.

### The GUID Pitfall

RFC 6455 defines a magic GUID used in the `Sec-WebSocket-Accept` calculation:

```
258EAFA5-E914-47DA-95CA-C5AB0DC85B11
```

This is a fixed constant from the spec, not a value you generate. During initial development, a wrong GUID was used (`...5BBF24F4B947` instead of `...C5AB0DC85B11`). The symptom:

- Go test clients and curl connected successfully
- Browsers (Chrome, Safari) rejected the connection with close code 1006

The failure was silent on the server side: `conn.Write()` returned no error, the 101 response was byte-for-byte valid HTTP, and the frame encoding was correct. The only thing wrong was the Sec-WebSocket-Accept hash value.

Root cause: Go WebSocket client libraries and curl do not validate `Sec-WebSocket-Accept`. Browsers do, per RFC 6455 Section 4.1 step 5. This meant the server appeared to work in every test except the actual browser.

The debugging process went through increasingly lower abstraction levels -- custom framework, standard `http.Server`, raw TCP, manual HTTP parsing, `syscall.Write` on a blocking file descriptor -- all showing the same failure with browsers while succeeding with Go clients. A Python websockets server on the same machine worked with the same browsers, seemingly proving Go itself was broken. In the end, comparing the `computeAcceptKey` implementation against Gorilla WebSocket's source revealed the single wrong constant.

Lessons:

- Always verify cryptographic/protocol constants against the RFC, character by character
- Test WebSocket implementations with an actual browser from the start, not just programmatic clients
- When browsers reject WebSocket with code 1006 and no useful error, check the Sec-WebSocket-Accept computation first -- it's the most common cause
- "Works with curl but not browsers" is a strong signal that the handshake response is subtly wrong, not that the networking stack is broken
