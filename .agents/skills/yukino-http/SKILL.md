---
name: yukino-http
description: >
  Build, modify, or debug the Go HTTP framework
  github.com/hangtiancheng/yukino.go/libs/yukino_http.
  Use for its onion middleware, trie routes and groups, deferred responses,
  response takeover, static files, SSE, and WebSocket APIs.
---

# yukino_http

Source root and import:
`libs/yukino_http/`,
`github.com/hangtiancheng/yukino.go/libs/yukino_http`.
The implementation uses the Go standard library.
Read the relevant source for signatures and behavior; dependency and toolchain
requirements come from `go.mod` and the repository's `go.work`.

## Source guide

| Work                                             | Read                                                                                                   |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| Application, HTTP server, middleware composition | [yukino.go](../../../libs/yukino_http/yukino.go)                                                       |
| Context, request access, headers, takeover       | [context.go](../../../libs/yukino_http/context.go)                                                     |
| Response setters and rendering                   | [response.go](../../../libs/yukino_http/response.go)                                                   |
| Router groups, prefix matching, static serving   | [group.go](../../../libs/yukino_http/group.go)                                                         |
| Registration and trie lookup                     | [router.go](../../../libs/yukino_http/router.go), [trie.go](../../../libs/yukino_http/trie.go)         |
| SSE and WebSocket lifecycle/framing              | [sse.go](../../../libs/yukino_http/sse.go), [websocket.go](../../../libs/yukino_http/websocket.go)     |
| Logging and panic recovery                       | [logger.go](../../../libs/yukino_http/logger.go), [recovery.go](../../../libs/yukino_http/recovery.go) |

## Request and response lifecycle

`New()` constructs an `Application` implementing `http.Handler`.
`Default()` also installs `Logger()` and `Recovery()`.
Handlers and middleware have type `func(ctx *Context, next func())`.

- Middleware runs as an onion. Omitting `next()` short-circuits the chain;
  calling it twice panics. `Throw(status, message)` only stages a response:
  return from the handler to stop processing.
- Rendering runs after the chain unwinds. `JSON`, `String`, `Data`,
  and `HTML` stage a body and immediately promote the implicit 404 to 200.
  Use `SetStatus` or `Throw` for an explicit status, especially 404 with
  a body; direct assignment to `Status` does not mark it explicit.
- `ctx.Set` buffers response headers; `ctx.Get` reads request headers.
  For multiple header values, use `ctx.Writer.Header().Add`.
  Buffered Content-Type takes precedence over the renderer's default.
- `Body == nil` writes only the status line; `ctx.JSON(nil)` therefore
  does not serialize a JSON null. Exact `[]byte`/`string` types and values
  implementing `io.Reader` have dedicated render paths; other ordinary
  values are JSON-marshaled.
- JSON marshal failure writes a 500 response before committing the staged
  status. Missing templates also write 500. These paths do not update
  `ctx.Status`; Logger runs before deferred rendering and is not an
  authoritative record of the final wire status.
- Statuses 204, 205, and 304 clear the staged body and buffered
  Content-Type/Content-Length/Transfer-Encoding.
- `BindJSON(out)` decodes once and closes the request body. It has no
  built-in size limit. Use `Query`, `Param`, `PostForm`, and `FormFile`
  for the corresponding request data; the caller closes files returned by
  `FormFile`.
- `Recovery` converts panics from downstream middleware/handlers into a
  staged 500 and re-panics `http.ErrAbortHandler`. It cannot replace a
  response after takeover or catch panics in separately started goroutines
  or the later deferred renderer.

Call `ctx.TakeOver()` before writing directly to `Writer` or invoking an
external `http.Handler`. It copies buffered headers and disables deferred
rendering, without writing a status/body or wrapping the writer. Later
`ctx.Set`, `Body`, and status changes are not rendered.
`SetStatus` can still record the status for logging.
The ordinary HTTP writer must only be used during its handler's lifetime.

`Listen(addr) error` uses the application's internal `http.Server`;
`Shutdown(ctx)` shuts down that server. To configure TLS or server timeouts,
serve the application with a caller-owned `http.Server{Handler: app}` and
shut down that server directly.

## Routing and static files

- Application and Router expose `Get/Post/Put/Delete/Patch/Head/Options`
  and `All`; `All` registers those seven methods.
- `Router(prefix)` creates groups, including nested groups. Prefixes gain
  a leading slash and lose trailing slashes; `"/"` becomes the empty
  prefix. Route patterns are concatenated with the group prefix, so supply
  a leading slash on each route pattern.
- Middleware is collected per request from every path-prefix-matching
  router in router creation order. Prefix matching observes the slash
  boundary: `/v1` does not match `/v10`.
- `:name` captures one segment; `*name` captures the remaining segments.
  Pattern parsing stops at the first `*` segment. Empty slash
  segments are removed; duplicate canonical patterns overwrite handlers.
- Exact children are searched before wildcard children. Wildcard children
  are searched in insertion order: there is no separate priority rule
  between `:param` and `*rest`. Lookup backtracks when a branch fails.
- A method mismatch takes the normal 404 path, including prefix middleware.
  GET does not automatically register HEAD. Configure routes and middleware
  before concurrent serving because registration is unsynchronized.
- `Static(relativePath, root)` mounts a GET wildcard route, probes and
  closes the file, and delegates through `TakeOver` to `http.FileServer`.
  Missing files and directories without `index.html` return 404.
  The wildcard requires a segment, so the bare mount path does not match.
  The static response recorder mirrors the served status into `ctx.Status`.

## SSE

`ctx.SSE()` records and writes 200, sets stream headers, and takes over the
response. Set custom buffered headers before calling it; those headers are
copied during takeover and can override stream defaults.

- `Data(text)` emits one `data:` line per input line and a blank line.
  `Event(name, text)` also emits an event field; `JSON(name, value)`
  omits that field for an empty name and silently returns on marshal failure.
- `ID(id)` emits a field line without a blank line; call it before its
  event. `Retry(ms)` and `Comment(text)` end with a blank line.
  `Done()` sends `[DONE]` as data.
- Write methods serialize with the writer mutex and flush when the HTTP
  writer implements `http.Flusher`. Public `Flush()` itself does not
  acquire that mutex.
- `Closed()` is the request context's done channel.
  `Stream(ch)` exits on channel closure or request cancellation; it does
  not cancel or drain the producer.
- `Heartbeat(interval)` starts keepalive comments and returns an idempotent
  stop function that waits for the goroutine. Stop it before the handler
  returns, and tie producers to the request context.

## WebSocket

`ctx.Upgrade(opts)` performs the server handshake through HTTP hijacking.
It checks GET, upgrade headers, version 13, and a non-empty key.
Pre-hijack failures stage an HTTP error; return when an error is reported.

- A nil `UpgradeOptions` is valid. `CheckOrigin == nil` accepts every
  origin. `Subprotocols` are negotiated in server-list order.
- Successful upgrade writes its own 101 handshake, marks the context
  flushed, and clears connection deadlines. Buffered `ctx.Set` headers
  are not copied into that handshake.
- `ReadBufferSize` influences reader selection; `WriteBufferSize` is
  declared but unused. Positive `MaxMessageSize` overrides the default
  65536-byte read limit, applied to frames and reassembled messages.
- Choose the callback loop (`OnMessage/OnClose/OnError/OnPing/OnPong`,
  then blocking `Listen()`) or synchronous `ReadMessage/ReadJSON`.
  Register callbacks before reading, and do not mix the two read loops.
- Public `WriteMessage`, `WriteJSON`, `Send`, `WriteText`,
  `WriteBinary`, and `Ping` use the write mutex. Automatic pong replies
  from the read loops bypass it, so concurrent read/write operation does
  not have complete serialization of all outgoing frames.
- Fragmented data is reassembled within one `readMessage` call. A control
  frame between fragments returns immediately and loses that partial
  message. Text-message payloads are not UTF-8 validated.
- Read errors and peer-close handling signal `Closed()` but do not close
  the underlying connection. Its owner must call `Close()` or
  `CloseWithMessage(code, text)`. A hijacked connection can outlive the
  HTTP handler and must be managed separately from `Application.Shutdown`.
- `Heartbeat(interval)` sends pings and returns a blocking, idempotent
  stop function. A ping failure stops that goroutine without itself
  closing the socket.

## Example and verification

```go
package httpexample

import (
    "net/http"

    yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

func NewApp() *yukino.Application {
    app := yukino.Default()
    api := app.Router("/api")
    api.Get("/hello/:name", func(ctx *yukino.Context, next func()) {
        ctx.JSON(yukino.H{"name": ctx.Param("name")})
    })
    api.Post("/echo", func(ctx *yukino.Context, next func()) {
        var input struct {
            Text string `json:"text"`
        }
        if err := ctx.BindJSON(&input); err != nil {
            ctx.Throw(http.StatusBadRequest, "invalid JSON")
            return
        }
        ctx.SetStatus(http.StatusAccepted)
        ctx.JSON(input)
    })
    return app
}
```

From `libs/yukino_http`, run `go test ./...`.
Use `httptest.NewRecorder` for deferred responses, takeover, and SSE.
WebSocket upgrade tests need a hijack-capable server such as
`httptest.NewServer(app)`; existing framing helpers are in
`websocket_test.go`. Set read deadlines in socket tests.
