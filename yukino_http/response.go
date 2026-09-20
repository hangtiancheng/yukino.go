// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package yukino_http

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// promoteStatus mirrors Koa's body setter: assigning a body upgrades the
// default 404 to 200 immediately, so middleware running after next() observes
// the final status instead of the pre-render default.
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
		// keep the redirect status chosen by the caller (Koa behavior)
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

// emptyStatus reports whether the status code forbids a response body
// (Koa strips the body for these codes in respond()).
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

// setContentType applies the inferred Content-Type unless the user already
// buffered an explicit one via ctx.Set (which was flushed by respond()).
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
