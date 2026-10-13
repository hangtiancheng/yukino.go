package yukino_http

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
)

type H map[string]any

type Middleware func(ctx *Context, next func())

type Context struct {
	Request *http.Request
	Writer  http.ResponseWriter

	Path   string
	Method string

	Status int
	Body   any
	Type   string

	State  map[string]any
	Params map[string]string

	headers map[string]string

	app       *Application
	flushed   bool
	statusSet bool
}

func newContext(w http.ResponseWriter, req *http.Request) *Context {
	return &Context{
		Request: req,
		Writer:  w,
		Path:    req.URL.Path,
		Method:  req.Method,
		Status:  http.StatusNotFound,
		State:   make(map[string]any),
		headers: make(map[string]string),
	}
}

func (ctx *Context) Throw(status int, msg string) {
	ctx.Status = status
	ctx.statusSet = true
	ctx.Body = H{"message": msg, "data": nil}
}

func (ctx *Context) SetStatus(status int) {
	ctx.Status = status
	ctx.statusSet = true
}

// TakeOver disables deferred response rendering for this request. Call it before
// writing directly to Writer or passing Writer to an external http.Handler.
// Buffered headers are copied to Writer, but no status or body is written and
// Writer is not wrapped, preserving optional interfaces such as http.Flusher.
// Subsequent calls have no effect. The caller owns the response until its handler
// returns; deferred body, status and header changes are no longer rendered.
// SetStatus can still be used to record the status for logging middleware.
func (ctx *Context) TakeOver() {
	if ctx.flushed {
		return
	}
	ctx.flushed = true
	for k, v := range ctx.headers {
		ctx.Writer.Header().Set(k, v)
	}
}

func (ctx *Context) Get(header string) string {
	return ctx.Request.Header.Get(header)
}

func (ctx *Context) Set(header string, value string) {
	ctx.headers[http.CanonicalHeaderKey(header)] = value
}

func (ctx *Context) Query(key string) string {
	return ctx.Request.URL.Query().Get(key)
}

func (ctx *Context) Param(key string) string {
	value := ctx.Params[key]
	return value
}

func (ctx *Context) PostForm(key string) string {
	return ctx.Request.FormValue(key)
}

func (ctx *Context) BindJSON(out any) error {
	defer ctx.Request.Body.Close()
	return json.NewDecoder(ctx.Request.Body).Decode(out)
}

func (ctx *Context) FormFile(key string) (multipart.File, *multipart.FileHeader, error) {
	return ctx.Request.FormFile(key)
}
