---
name: yukino-http
description: >
  Lightweight Koa-inspired HTTP framework for Go, module
  github.com/hangtiancheng/yukino.go/libs/yukino_http, stdlib-only, zero deps. Exports
  Application (New, Default, Listen, Shutdown, Use, Router, ServeHTTP, LoadHTMLGlob,
  Static, Get/Post/Put/Delete/Patch/Head/Options/All), Context (JSON, String, Data,
  HTML, Redirect, Throw, SetStatus, TakeOver, BindJSON, Query, Param, PostForm,
  FormFile, Get, Set, SSE, Upgrade; Status/Body/Type/State/Params), Router groups,
  SSEWriter (Event, Data, JSON, ID, Retry, Comment, Heartbeat, Stream, Closed, Flush,
  Done), WSConn (Listen, On* handlers, Read/Write msg/JSON, Send, Ping, Close, Closed,
  Heartbeat, NetConn), UpgradeOptions, Logger, Recovery, H, Middleware, opcodes,
  ErrWSClosed/ErrWSInvalidFrame. For yukino_http: trie routing, Koa onion middleware,
  deferred responses with 404-to-200 promotion, ctx.TakeOver, SSE, RFC 6455 WebSocket,
  router groups, koa-static file serving. Not for plain net/http,
  gin/echo/fiber/chi/gorilla, Node Koa, reverse proxies, or gRPC (yukino-rpc).
---

# yukino_http

Koa-inspired HTTP framework: trie routing, onion middleware
(`func(ctx *Context, next func())`), deferred response rendering with 404-to-200
promotion, explicit response takeover (`ctx.TakeOver`), SSE, a hand-rolled RFC 6455
WebSocket (event-driven + synchronous APIs, fragmentation reassembly), prefix-normalized
router groups, koa-static-style file serving without directory listings. Standard
library only; zero external module dependencies.

- Module: `github.com/hangtiancheng/yukino.go/libs/yukino_http`
- Source root: `libs/yukino_http/` (library files at the module root, demo in `examples/hello/`)
- Go directive: `go 1.26.0`
- Current release tag: `libs/yukino_http/v0.0.2`

## Architecture overview

```
Application (implements http.Handler): root *Router, routers []*Router (every group in
creation order), internal *router (per-method trie roots + handlers["METHOD-/pattern"]),
htmlTemplates/funcMap, server *http.Server built in New() (Shutdown never races Listen)
Router (group): normalized prefix, middlewares, parent (never read), app back-pointer;
node (trie): pattern (leaves only), part (literal / ":name" / "*name"), children, isWild
Context: Request/Writer, Path/Method copied at construction, deferred Status/Body/Type,
State/Params, unexported headers (buffered ctx.Set), app, flushed, statusSet

Request lifecycle: ServeHTTP concatenates middleware of every prefix-matching Router
(matchRouterPath; collected at REQUEST time, so later Use() applies) -> newContext ->
getRoute (miss: synthesized explicit-404 notFound with text body) ->
compose(middlewares, handler) (panics on double next()) -> ctx.respond(), skipped while
ctx.flushed (set by SSE(), Upgrade(), TakeOver(), Static).
```

Key invariants:

- The only HTTP write happens in `respond()` after the chain unwinds: handlers mutate
  `Status`/`Body`/`Type`/`ctx.Set` instead of writing the writer, so code after `next()`
  sees and can change the response (writing to `ctx.Writer` yourself without
  `TakeOver()` commits the status early and triggers a superfluous-`WriteHeader` warning).
- Body setters (`JSON`/`String`/`Data`/`HTML`) call `promoteStatus`: an unrecorded
  default 404 becomes 200 immediately, so middleware after `next()` sees the final
  status (`TestPromotedStatusVisibleToMiddlewareAfterNext`).
- `next()` twice in one middleware panics `"yukino_http: next() called multiple times"`
  (a 500 under `Default()`); `ctx.flushed` discards all later deferred writes.

## Core types

### Application

```go
type Application struct{ /* unexported */ }

func New() *Application
func Default() *Application   // New() + Use(Logger(), Recovery())
```

| Method                                                                                             | Notes                                                                                                                                       |
| -------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `Listen(addr string) error`                                                                        | sets `server.Addr`, calls `ListenAndServe`; `http.ErrServerClosed` after `Shutdown`, bind errors propagate (`TestListenReturnsListenError`) |
| `Shutdown(ctx context.Context) error`                                                              | `http.Server.Shutdown`; `nil` if the internal server is nil                                                                                 |
| `SetFuncMap(funcMap template.FuncMap)` + `LoadHTMLGlob(pattern string)`                            | func map must be set before parse; `template.Must(...ParseGlob)` panics on parse errors                                                     |
| `ServeHTTP(w http.ResponseWriter, req *http.Request)`                                              | `http.Handler` implementation                                                                                                               |
| `Use(...)` / `Router(prefix) *Router` / `Static(relativePath, root)`                               | delegate to the root Router                                                                                                                 |
| `Get`/`Post`/`Put`/`Delete`/`Patch`/`Head`/`Options(pattern, handler)` and `All(pattern, handler)` | `All` registers GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS in that order; all delegate to root                                            |

### Context

```go
type H map[string]any
type Middleware func(ctx *Context, next func())

type Context struct {
    Request *http.Request
    Writer  http.ResponseWriter
    Path    string            // req.URL.Path at construction
    Method  string            // req.Method at construction
    Status  int               // default http.StatusNotFound
    Body    any               // nil => status line only
    Type    string            // content hint set by JSON/String/HTML
    State   map[string]any    // empty map per request; no locking
    Params  map[string]string // nil until a route hit; stays nil on 404; Param() reads nil safely
    // unexported: headers (buffered Set), app, flushed, statusSet
}
```

- `SetStatus(status int)` records an explicit status (never promoted). `statusSet` is
  also marked by `Throw`, `Redirect`, the notFound handler, `SSE`, `Upgrade`, Static
  misses, and `Recovery`; direct field assignment is not (`ctx.Status = 404` + a body
  setter yields 200 — use `SetStatus`/`Throw`).
- `Throw(status int, msg string)` stages the response only (`H{"message": msg,
"data": nil}` keeps the unified `{message, data}` envelope); return immediately and
  skip `next()` — it never aborts the chain.
- `TakeOver()` disables deferred rendering: sets `ctx.flushed`, copies `ctx.Set`-buffered
  headers to `Writer.Header()`, writes NO status or body, never wraps the writer
  (`http.Flusher` survives). Call it before writing directly to `Writer` or handing
  `Writer` to an external `http.Handler`; repeated calls are no-ops, so handler-set
  headers stick. Afterwards `ctx.Set`/`ctx.Body` never reach the wire (use `ctx.Writer`);
  `SetStatus` still records status for Logger. `SSE()` and Static call it; `Upgrade()`
  sets `flushed` directly. Covered by the three `TestTakeOver*` tests.

Request accessors: `Query(key)` (URL query), `Param(key)` (`:name`/`*name` value),
`PostForm(key)` (`FormValue`), `Get(header)` (request header), `FormFile(key)`
(`Request.FormFile`), `BindJSON(out any) error` (decodes, then `defer`s closing the body
— destructive, one call per request, no size limit, raw `encoding/json` errors).

Response setters (deferred):

| Method                                 | Effect                                                                                                                                                                                     |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `JSON(obj any)`                        | `Type="application/json"`, `Body=obj`, promote                                                                                                                                             |
| `String(format string, values ...any)` | `Type="text/plain"`, `Sprintf` body, promote; use `String("%s", s)` for untrusted strings containing `%`                                                                                   |
| `Data(data []byte)`                    | body bytes, promote; `Type` untouched, so empty `Type` leaves net/http sniffing                                                                                                            |
| `HTML(name string, data any)`          | `Type="text/html"`, internal `htmlPayload`, promote; requires `LoadHTMLGlob`                                                                                                               |
| `Redirect(url string)`                 | keeps 300/301/302/303/305/307/308 already in `ctx.Status` (checked field, not `statusSet`), else forces 302 explicit; buffers `Location`; nil `Body` becomes text `"Redirecting to <url>"` |
| `Set(header, value string)`            | buffers one response header under `http.CanonicalHeaderKey`; flushed by `respond()`, `TakeOver()`, and the Static handler                                                                  |

`respond()` order: return if `flushed`; fallback promotion (`Body != nil` + unrecorded
404 -> 200); empty statuses 204/205/304 clear `Body` and delete buffered
Content-Type/Content-Length/Transfer-Encoding (`TestEmptyStatusStripsBody`); flush
buffered headers; nil `Body` -> status line only; else exact-type switch: `htmlPayload`
(templates not loaded -> 500 `{"message":"HTML templates are not loaded"}`; execution
errors after the status line only log, truncating), `[]byte`, `string`, `io.Reader`,
anything else as JSON (marshalled BEFORE committing; marshal failure cleanly emits 500
`{"message":"Internal Server Error"}`; success defaults `Type` to `application/json`;
a named `type Raw []byte` arrives here as base64 — use `ctx.Data([]byte(raw))` for
bytes). `setContentType` never overwrites a Content-Type already on the writer, so a
buffered `ctx.Set("content-type", ...)` always wins (`TestUserContentTypeHeaderWins`).

### Router (groups and static)

```go
type Router struct{ /* unexported */ }

func (app *Application) Router(prefix string) *Router
func (r *Router) Router(prefix string) *Router   // nested child
func (r *Router) Use(middlewares ...Middleware)
func (r *Router) Get/Post/Put/Delete/Patch/Head/Options(pattern string, handler Middleware) // r.prefix + pattern
func (r *Router) All(pattern string, handler Middleware)
func (r *Router) Static(relativePath, root string)
```

- `normalizePrefix` forces a leading `/`, strips trailing `/`, maps `"/"` to `""`:
  `app.Router("v1")` and `app.Router("/v1/")` both yield `/v1` (`TestRouterPrefixNormalized`,
  `TestNestedRouter`).
- Middleware runs per request from all prefix-matching routers in Router CREATION order
  (not nesting depth); `Use` on a parent after creating children still applies
  (`TestMiddlewareOrder`, `TestRouterMiddlewareUsesPathBoundary`, `TestMatchRouterPath`).
- `matchRouterPath`: empty/`/` prefix matches everything; otherwise the request path
  must equal the prefix or continue with `/` (`/v1` never fires on `/v10`).
- Each registration logs `Route <METHOD> - <pattern>` via the standard `log` package.
- `Static(relativePath, root)` mounts a GET route at `<prefix><relativePath>/*filepath`
  over `http.StripPrefix(path.Join(r.prefix, relativePath), http.FileServer(http.Dir(root)))`:
  probes `ctx.Param("filepath")` (handle closed immediately, `TestStaticProbeClosesFileHandles`);
  missing -> explicit empty 404 via the deferred path; directories serve only when they
  contain `index.html` (no listings, `TestStaticDirectoryListingDisabled`); on serve,
  `ctx.TakeOver()` flushes buffered headers and `http.FileServer` writes through a
  `statusRecorder` mirroring the real status into `ctx.Status` (200 on first `Write`).

## Routing: middleware onion and trie

`compose` (unexported) builds the koa-compose dispatcher: `dispatch(i)` panics when
`i <= index` (double `next()`); not calling `next()` short-circuits the chain (the
idiomatic reject: `Throw` then `return`); the final handler receives a no-op `next`;
`compose` runs per request. `respond()` always runs after the chain, so even a
short-circuiting middleware's staged response is rendered.

One trie per HTTP method plus `handlers["METHOD-/canonical/pattern"]`; lookup is
O(segments) with sibling backtracking; wrong method = no match = 404.

- `:name` consumes one segment; `*name` consumes the remainder joined with `/` and must
  be last (`parsePattern` stops after the first `*` segment: `"/p/*name/*"` ->
  `["p", "*name"]`); a bare `*` matches but captures nothing (only `len(part) > 1` is
  recorded). `TestParsePattern` pins `"/"` -> `[]`, `"//p//:name//"` -> `["p", ":name"]`.
- Patterns are canonicalized on registration (`/users`, `/users/`, `//users` share one
  slot; last registration wins silently).
- `matchChildren` tries exact parts before wild ones: literal beats `:name` beats `*name`
  (`TestStaticRouteTakesPriorityOverWildcard`); `search` backtracks; empty-`pattern`
  nodes never match; a `*` node terminates the search. `TestGetRoute`: `/hello/yukino` ->
  `name=yukino`, `/assets/css/test.css` -> `filepath=css/test.css`.
- A miss runs `compose(middlewares, notFound)` with explicit 404 +
  `"404 NOT FOUND: <path>\n"`, so Logger/Recovery see 404s. A wildcard route does not
  match its own bare prefix: `Static("/assets", dir)` 404s `/assets` and `/assets/`.

## SSEWriter

```go
type SSEWriter struct{ /* unexported: ctx, flusher, mu */ }
func (ctx *Context) SSE() *SSEWriter
```

`SSE()`: asserts `Writer` to `http.Flusher` (nil-tolerant; the recorder flushes) ->
explicit `Status = 200` -> sets `text/event-stream`/`no-cache`/`keep-alive` on
`Writer.Header()` -> `TakeOver()` (buffered `ctx.Set` headers flush ON TOP and can
override `text/event-stream`; `TestSSEWithCustomHeaders`) -> writes the 200 status line.
Call `ctx.Set` BEFORE `ctx.SSE()`, at most once per request (a second call triggers the
superfluous-`WriteHeader` warning), then no deferred setters.

| Method                        | Wire output                                                                                                                                               |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Event(event, data string)`   | `event: <event>\n` + one `data:` line per input line + blank line; an empty name still writes `event: \n` — use `Data`/`JSON("", obj)` for unnamed events |
| `Data(data string)`           | one `data:` line per input line + blank line                                                                                                              |
| `JSON(event string, obj any)` | optional `event:` line, single `data: <json>\n\n`; marshal errors return silently                                                                         |
| `ID(id string)`               | bare `id: <id>\n` field line — call immediately before the `Event`/`Data`/`JSON` it belongs to                                                            |
| `Retry(ms int)`               | `retry: <ms>\n\n` — the blank line ends any partial event; emit between events                                                                            |
| `Comment(text string)`        | `: <line>\n` per line + blank line                                                                                                                        |
| `Heartbeat(interval) func()`  | goroutine writing `: keepalive\n\n`; returned stop is idempotent and blocks until exit; goroutine also exits on request-context cancel                    |
| `Stream(ch <-chan string)`    | forwards via `Data` until `ch` closes or request context done; does NOT drain/cancel the producer                                                         |
| `Closed() <-chan struct{}`    | `ctx.Request.Context().Done()` (disconnect detection)                                                                                                     |
| `Flush()`                     | flushes without the mutex; prefer write methods (they flush)                                                                                              |
| `Done()`                      | `Data("[DONE]")` (OpenAI-compatible sentinel)                                                                                                             |

All write methods (and the heartbeat write) take the internal mutex (`TestSSEHeartbeatConcurrency`
under `-race`); wire bytes are pinned by `TestSSEWriter`/`TestSSEMultilineData`/`TestSSEJSON`/
`TestSSEComment`/`TestSSEStream`/`TestSSEClosed`. Never use the writer after the handler
returns (`net/http` invalidates it).

## WebSocket

Hand-rolled RFC 6455, server role only (no permessage-deflate, no client dialer).
Sentinels are returned bare (`==`/`errors.Is` both work); `Upgrade` failures return
fresh unwrapped `errors.New` values — classify those by the staged `ctx.Status`. A nil
`*UpgradeOptions` or zero value is fully valid; subprotocol matching is case-sensitive
on trimmed values (`TestNegotiateSubprotocol`).

```go
const ( TextMessage = 1; BinaryMessage = 2; CloseMessage = 8; PingMessage = 9; PongMessage = 10 )
var ( ErrWSClosed = errors.New("websocket: connection closed"); ErrWSInvalidFrame = errors.New("websocket: invalid frame") )

func (ctx *Context) Upgrade(opts *UpgradeOptions) (*WSConn, error)

type UpgradeOptions struct {
    ReadBufferSize  int                        // 0 -> 4096 minimum bufio.Reader size
    WriteBufferSize int                        // declared but NEVER read; writes are unbuffered
    MaxMessageSize  int                        // 0 -> 65536 read cap per frame and per reassembled message
    CheckOrigin     func(r *http.Request) bool // nil -> every Origin accepted (no same-origin default!)
    Subprotocols    []string                   // nil -> no response header; server order wins
}
```

Validation order (each failure stages `Throw` + returns an error): CheckOrigin
(403 "origin not allowed") -> method GET (405) -> `Connection` contains `upgrade`
(400; `headerContains` splits commas case-insensitively, so `keep-alive, Upgrade`
passes, `TestHeaderContains`) -> `Upgrade` contains `websocket` (400) ->
`Sec-WebSocket-Version` contains `13` (400 + advisory `Sec-WebSocket-Version: 13`
buffered via `ctx.Set`, `TestWebSocketUpgradeRequiresVersion13`) -> `Sec-WebSocket-Key`
present (400). Then optional subprotocol negotiation, then
`http.NewResponseController(ctx.Writer).Hijack()` (failure -> 500, e.g. under
`httptest.ResponseRecorder`). Success: `ctx.flushed` set directly (buffered headers are
NOT flushed past this point), explicit `Status = 101` for Logger,
`SetDeadline(time.Time{})` clears conn deadlines (server timeouts stop applying), the
hijacked `bufio.Reader` is reused when it holds buffered bytes (they are already-read
frames) or is big enough, and the 101 handshake with `Sec-WebSocket-Accept =
base64(sha1(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))` (`TestComputeAcceptKey`)
goes straight to the `net.Conn` (a write failure closes it, no staged response). On an
`Upgrade` error just `return`; `respond()` renders the staged error.

```go
type WSConn struct{ /* unexported: conn, br, writeMu, readMu, closed, once, maxMessageSize, on* handlers */ }

// Event-driven; register BEFORE Listen (setters are plain assignments — mutating them
// while Listen runs is a data race):
func (ws *WSConn) OnMessage(fn func(messageType int, data []byte)); func (ws *WSConn) OnClose(fn func(code int, text string))
func (ws *WSConn) OnError(fn func(err error)); func (ws *WSConn) OnPing(fn func(data []byte)); func (ws *WSConn) OnPong(fn func(data []byte))
func (ws *WSConn) Listen() // blocking loop: ping -> OnPing + auto pong; pong -> OnPong; close -> answer + close Closed() + OnClose + return; read error -> handleError (OnError unless already closed) + close Closed(); does NOT close the net.Conn

// Synchronous:
func (ws *WSConn) ReadMessage() (messageType int, data []byte, err error) // holds readMu; auto-answers pings, skips pongs; close frame -> answers, closes Closed(), fires OnClose, returns ErrWSClosed; other errors -> handleError, raw error
func (ws *WSConn) ReadJSON(v any) error            // ReadMessage + json.Unmarshal; frame type ignored (binary JSON accepted)
func (ws *WSConn) WriteMessage(messageType int, data []byte) error
func (ws *WSConn) WriteJSON(v any) error           // TextMessage
func (ws *WSConn) Send(text string) error          // identical to WriteText
func (ws *WSConn) WriteText(text string) error     // TextMessage
func (ws *WSConn) WriteBinary(data []byte) error   // BinaryMessage
func (ws *WSConn) Ping() error                     // nil-payload ping under writeMu
func (ws *WSConn) Close() error                    // 1000 close frame + net.Conn close
func (ws *WSConn) CloseWithMessage(code int, text string) error

// Utility:
func (ws *WSConn) Closed() <-chan struct{}         // closes once: local Close, peer close, or read error
func (ws *WSConn) SetReadDeadline(t time.Time) error
func (ws *WSConn) SetWriteDeadline(t time.Time) error
func (ws *WSConn) Heartbeat(interval time.Duration) func() // ping goroutine; idempotent blocking stop; exits on ping failure or Closed()
func (ws *WSConn) NetConn() net.Conn               // addr info only; raw use corrupts framing
```

Concurrency and lifetime: public writes serialize on `writeMu`; `ReadMessage`/`ReadJSON`
on `readMu`; `Listen` takes NO read lock (never mix the two read loops) and the
automatic pongs in `Listen`/`ReadMessage` bypass `writeMu` (reply via `OnPing` +
`WriteMessage(PongMessage, …)` if you write elsewhere). `sync.Once` guards `Closed()`;
double `Close` returns the `net.Conn` double-close error but does not panic. Nothing
closes the hijacked conn — always `defer ws.Close()` (omitting leaks one fd per connection).

Frames: fragmented messages reassemble (`TestWebSocketFragmentedMessage`: "hello ",
"fragmented ", "world" -> "hello fragmented world"); a continuation without an
in-progress message, a new data frame mid-fragment, or an over-limit total yields
`ErrWSInvalidFrame`; a control frame between fragments is returned immediately and
DISCARDS the reassembly buffer (interleaved fragmentation is not supported). The size
cap checks declared length before allocating; writes are uncapped. Unmasked client
frames, non-zero RSV bits, control frames violating FIN/≤125-bytes, and unknown opcodes
are rejected (`TestWebSocketRejectsUnmaskedClientFrame`). `parseClosePayload`: `<2`
bytes -> `(1000, "")`; else big-endian code + text, blanked when invalid UTF-8
(`TestParseClosePayload`). Text payloads are not UTF-8 validated (RFC 6455 wants a 1007
close; raw bytes are delivered). Server frames are unmasked (correct for the server role).

## Built-in middleware

```go
func Logger() Middleware   // after next(): log "[<status>] <RequestURI> in <duration>" via std log
func Recovery() Middleware
```

Logger's status always matches what the client received because setters promote eagerly
and `SSE`/`Upgrade`/Static record 200/101/real (via `statusRecorder`). Plain text, no
ids. `Recovery` re-raises `http.ErrAbortHandler` (keeps `httputil.ReverseProxy` disconnect
semantics); any other panic is logged with `%v` + a `runtime.Callers` traceback and
rewritten to explicit 500, `Type` cleared, buffered header map replaced, `Body =
H{"message":"Internal Server Error"}` (`TestDefaultRecovery`). Fixed body — swap in your
own middleware to customize. It covers only the request goroutine, and after
`TakeOver`/`SSE`/`Upgrade` the staged 500 is discarded because `flushed` is set.

## Typical usage

```go
import yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"

app := yukino.Default()
api := app.Router("/api")
api.Use(func(ctx *yukino.Context, next func()) { // short-circuit by not calling next()
    if ctx.Get("Authorization") == "" { ctx.Throw(http.StatusUnauthorized, "missing token"); return }
    ctx.State["user"] = "resolved-user"; next()
})
api.Get("/users/:id", func(ctx *yukino.Context, next func()) {
    user, err := findUser(ctx.Param("id"))
    if err != nil { ctx.Throw(http.StatusNotFound, "user not found"); return } // Throw stages, never aborts
    ctx.JSON(yukino.H{"message": "ok", "data": user})                          // auto-promotes to 200
})
api.Post("/users", func(ctx *yukino.Context, next func()) {
    var in struct{ Name string `json:"name"` }
    if err := ctx.BindJSON(&in); err != nil { ctx.Throw(http.StatusBadRequest, "invalid JSON"); return }
    ctx.SetStatus(http.StatusCreated)
    ctx.JSON(yukino.H{"message": "created", "data": in.Name})
})

go func() { // graceful shutdown idiom
    if err := app.Listen(":8080"); err != nil && err != http.ErrServerClosed { log.Fatal(err) }
}()
sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop(); <-sigCtx.Done()
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel(); _ = app.Shutdown(shutdownCtx)
```

Templates, static files, takeover for an external handler:

```go
app.SetFuncMap(template.FuncMap{"upper": strings.ToUpper}) // must precede LoadHTMLGlob
app.LoadHTMLGlob("templates/*.tmpl")                        // panics on parse errors
app.Get("/", func(ctx *yukino.Context, next func()) { ctx.HTML("index.tmpl", yukino.H{"Name": "yukino"}) })
app.Static("/assets", "./public")                        // /assets/*filepath; bare /assets 404s
app.Router("/docs").Static("/files", "./docs-files")     // /docs/files/*filepath
app.Get("/mirror", func(ctx *yukino.Context, next func()) {
    ctx.TakeOver() // buffered headers flushed; nothing committed; Writer stays unwrapped
    externalHandler.ServeHTTP(ctx.Writer, ctx.Request)
})
```

SSE with heartbeat and disconnect handling:

```go
app.Post("/chat", func(ctx *yukino.Context, next func()) {
    sse := ctx.SSE()
    defer sse.Done()
    stop := sse.Heartbeat(15 * time.Second)
    defer stop()
    upstream := generateResponse(ctx.Request.Context()) // <-chan string
    for {
        select {
        case <-sse.Closed(): return // producer sees the canceled request context
        case chunk, ok := <-upstream:
            if !ok { return }
            sse.JSON("delta", yukino.H{"content": chunk})
        }
    }
})
```

WebSocket (event-driven; synchronous alternative: `ReadMessage`/`WriteMessage` or
`ReadJSON`/`WriteJSON` loop, `CloseWithMessage(1003, ...)` on bad input):

```go
app.Get("/ws", func(ctx *yukino.Context, next func()) {
    ws, err := ctx.Upgrade(&yukino.UpgradeOptions{
        ReadBufferSize: 8192, MaxMessageSize: 1 << 20,
        CheckOrigin:  func(r *http.Request) bool { return r.Header.Get("Origin") == "https://example.com" },
        Subprotocols: []string{"chat"},
    })
    if err != nil { return } // the staged error response is rendered by respond()
    defer ws.Close()
    defer ws.Heartbeat(30 * time.Second)() // stop func deferred; idempotent and blocking
    ws.OnMessage(func(messageType int, data []byte) {
        if messageType == yukino.TextMessage { _ = ws.Send("echo: " + string(data)) }
    })
    ws.OnClose(func(code int, text string) { log.Printf("closed: %d %s", code, text) })
    ws.Listen() // blocks until close or error
})
```

## Testing patterns

`net/http/httptest` only; all test files are in-package (`package yukino_http`) except
`takeover_test.go` (`package yukino_http_test`, public API only). Handler tests use
`app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(...))` — no port bound;
renderer tests use the unexported `newContext(rec, req)` + field sets + `ctx.respond()`
(in-package). SSE assertions match exact bytes against `rec.Body.String()` (the recorder
implements `http.Flusher`). WebSocket tests need `httptest.NewServer(app)` because
`Upgrade` hijacks (a recorder hijack fails -> 500): `websocket_test.go` hand-writes the
handshake over `net.Dial` and offers reusable helpers `dialWS`, `writeClientFrame`,
`writeClientFrameRaw` (explicit FIN/mask), `readServerFrame`; `trackedFS`/`trackedFile`
in `yukino_test.go` count fds for the static probe. Set client read deadlines in tests
expecting no reply.

## Pitfalls / known limitations

1. Unusable zero values: zero `Application` panics on `Listen`/`Use`/`Get`/`ServeHTTP`
   (nil server/router/root); zero `Context` panics on `Set`, nil `State`, `Status` 0;
   zero `Router` panics; `SSEWriter`/`WSConn` come only from `SSE()`/`Upgrade()`.
   Zero/nil `UpgradeOptions` is valid.
2. Explicit 404-with-body needs `SetStatus`/`Throw`; a direct `ctx.Status = 404` + body
   setter yields 200.
3. `next()` twice panics; skipping it short-circuits; the handler's `next` is a no-op.
   `Throw` doesn't abort — return immediately.
4. `BindJSON` consumes+closes the body; one call per request; no size cap.
5. Registration (routes, `Use`, `app.routers`) is unsynchronized: do it all before
   `Listen`. Duplicate canonical patterns overwrite handlers silently.
6. `ctx.Set` = one value per canonical key, no removal; multiple `Set-Cookie` need
   `Writer.Header().Add`; Recovery drops buffered headers but not direct ones.
7. `SetFuncMap` after `LoadHTMLGlob` has no effect; `LoadHTMLGlob` panics on parse
   errors; template execution errors truncate the response with only a log line.
8. No 405/`Allow`; wrong method gets the 404 body. `Static("/assets", dir)` 404s the
   bare `/assets` and `/assets/`.
9. `ctx.JSON(nil)` writes no body (nil `Body` = status line only); use `ctx.Data([]byte("null"))` for the literal.
10. After `TakeOver`/`SSE`/`Upgrade`, deferred setters are dead; a second `SSE()` call
    triggers the superfluous-`WriteHeader` warning; SSE/WS writers must not outlive the handler.
11. `SSE` flush no-ops on non-flushers; `Stream` never drains/cancels producers; `ID`
    must precede its event block; `Retry` closes any partial event.
12. WebSocket: origin check opt-in (`CheckOrigin == nil` accepts any Origin); read cap
    65536 unless `MaxMessageSize` raises it (writes uncapped); `Listen` and `ReadMessage`
    mutually exclusive; automatic pongs bypass `writeMu`; control frames between
    fragments break reassembly; `WriteBufferSize` ignored; hijack required (recorder ->
    500); upgrade clears conn deadlines; hijacked conns invisible to `Shutdown` (close
    via `CloseWithMessage(1001, ...)`); upgrade errors unwrapped (classify by
    `ctx.Status`); text frames not UTF-8 validated; nothing auto-closes the conn.
13. The internal `http.Server` is unexposed: timeouts/TLS need your own
    `http.Server{Handler: app}` (then `app.Shutdown` no-ops on it).
14. Route registration logs `Route <METHOD> - <pattern>`; silence via the standard
    `log` package. `RequestURI` in Logger output is empty on client-built requests.

## File map (all paths relative to `libs/yukino_http/`)

Flat module root: `yukino.go` (`Application`, `New`/`Default`, `ServeHTTP`, `compose`),
`context.go` (`Context`, `H`, `Middleware`, `TakeOver`, accessors), `response.go`
(setters + `respond`), `group.go` (`Router`, `Static`, `matchRouterPath`),
`router.go`/`trie.go` (internal trie), `sse.go` (`SSEWriter`), `websocket.go`
(`Upgrade`, `WSConn`, framing), `logger.go`/`recovery.go`, `examples/hello/main.go`
(demo: `/`, `/panic`, `/events` on `:9999`; `go run ./examples/hello`), `README.md`.
Tests: `context_test.go`, `response_test.go`, `router_test.go`, `sse_test.go`,
`websocket_test.go`, `yukino_test.go` (in-package), `takeover_test.go` (external package).

## Dependencies and cross-references

`libs/yukino_http/go.mod`: module `github.com/hangtiancheng/yukino.go/libs/yukino_http`,
`go 1.26.0`, NO `require` block at all (zero external dependencies; standard library
only); only `replace` directives pointing sibling modules (`yukino_cache`, `yukino_orm`,
`yukino_rpc`) at local directories; the `yukino_http` replace line is commented out.
Notable stdlib uses: `strings.SplitSeq` (Go 1.24+), `http.NewResponseController`,
`html/template`, `encoding/json`, `net/http` hijacking.

- `yukino-cache`: the only sibling library module importing `yukino_http`.
  `DashboardHandler()` returns a valid `Middleware` driving `ctx.Upgrade` + `WSConn`
  (`WriteJSON`, `ReadJSON`, `Heartbeat`, `Closed`, `Close`); `StartDashboard(addr)`
  builds its own `New()` app at `/dashboard/ws` — share a port via
  `app.Get("/dashboard/ws", cache.DashboardHandler())`. Load the `yukino-cache` skill.
- `yukino-orm` (MongoDB) and `yukino-rpc` (TCP RPC) do not import this module; load
  their skills for query-builder or RPC questions. Do not answer gRPC questions here.
