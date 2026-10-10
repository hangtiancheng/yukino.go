package yukino_http

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

func (ctx *Context) promoteStatus() {
	if !ctx.statusSet && ctx.Status == http.StatusNotFound {
		ctx.Status = http.StatusOK
	}
}

func (ctx *Context) JSON(obj any) {
	ctx.Type = "application/json"
	ctx.Body = obj
	ctx.promoteStatus()
}

func (ctx *Context) String(format string, values ...any) {
	ctx.Type = "text/plain"
	ctx.Body = fmt.Sprintf(format, values...)
	ctx.promoteStatus()
}

func (ctx *Context) Data(data []byte) {
	ctx.Body = data
	ctx.promoteStatus()
}

func (ctx *Context) HTML(name string, data any) {
	ctx.Type = "text/html"
	ctx.Body = htmlPayload{name: name, data: data}
	ctx.promoteStatus()
}

func (ctx *Context) Redirect(url string) {
	switch ctx.Status {
	case http.StatusMultipleChoices, http.StatusMovedPermanently, http.StatusFound,
		http.StatusSeeOther, http.StatusUseProxy, http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
	default:
		ctx.Status = http.StatusFound
	}
	ctx.statusSet = true
	ctx.headers["Location"] = url
	if ctx.Body == nil {
		ctx.Type = "text/plain"
		ctx.Body = "Redirecting to " + url
	}
}

type htmlPayload struct {
	name string
	data any
}

func emptyStatus(code int) bool {
	return code == http.StatusNoContent ||
		code == http.StatusResetContent ||
		code == http.StatusNotModified
}

func (ctx *Context) respond() {
	if ctx.flushed {
		return
	}

	if ctx.Body != nil && !ctx.statusSet && ctx.Status == http.StatusNotFound {
		ctx.Status = http.StatusOK
	}

	if emptyStatus(ctx.Status) {
		ctx.Body = nil
		delete(ctx.headers, "Content-Type")
		delete(ctx.headers, "Content-Length")
		delete(ctx.headers, "Transfer-Encoding")
	}

	for k, v := range ctx.headers {
		ctx.Writer.Header().Set(k, v)
	}

	if ctx.Body == nil {
		ctx.Writer.WriteHeader(ctx.Status)
		return
	}

	switch body := ctx.Body.(type) {
	case htmlPayload:
		ctx.respondHTML(body)
	case []byte:
		ctx.respondBytes(body)
	case string:
		ctx.respondString(body)
	case io.Reader:
		ctx.respondReader(body)
	default:
		ctx.respondJSON(body)
	}
}

func (ctx *Context) setContentType(value string) {
	if value == "" {
		return
	}
	if ctx.Writer.Header().Get("Content-Type") == "" {
		ctx.Writer.Header().Set("Content-Type", value)
	}
}

func (ctx *Context) respondJSON(obj any) {
	data, err := json.Marshal(obj)
	if err != nil {
		log.Printf("yukino_http: json marshal failed: %v", err)
		ctx.Writer.Header().Set("Content-Type", "application/json")
		ctx.Writer.WriteHeader(http.StatusInternalServerError)
		_, _ = ctx.Writer.Write([]byte(`{"message":"Internal Server Error"}`))
		return
	}
	if ctx.Type == "" {
		ctx.Type = "application/json"
	}
	ctx.setContentType(ctx.Type)
	ctx.Writer.WriteHeader(ctx.Status)
	_, _ = ctx.Writer.Write(data)
}

func (ctx *Context) respondString(s string) {
	if ctx.Type == "" {
		ctx.Type = "text/plain"
	}
	ctx.setContentType(ctx.Type)
	ctx.Writer.WriteHeader(ctx.Status)
	_, _ = ctx.Writer.Write([]byte(s))
}

func (ctx *Context) respondBytes(data []byte) {
	ctx.setContentType(ctx.Type)
	ctx.Writer.WriteHeader(ctx.Status)
	_, _ = ctx.Writer.Write(data)
}

func (ctx *Context) respondReader(r io.Reader) {
	ctx.setContentType(ctx.Type)
	ctx.Writer.WriteHeader(ctx.Status)
	_, _ = io.Copy(ctx.Writer, r)
}

func (ctx *Context) respondHTML(payload htmlPayload) {
	if ctx.app == nil || ctx.app.htmlTemplates == nil {
		ctx.Writer.WriteHeader(http.StatusInternalServerError)
		_, _ = ctx.Writer.Write([]byte(`{"message":"HTML templates are not loaded"}`))
		return
	}
	ctx.setContentType("text/html")
	ctx.Writer.WriteHeader(ctx.Status)
	if err := ctx.app.htmlTemplates.ExecuteTemplate(ctx.Writer, payload.name, payload.data); err != nil {
		log.Printf("yukino_http: template %q execution failed: %v", payload.name, err)
	}
}
