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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRespondJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.Status = http.StatusCreated
	ctx.JSON(H{"name": "yukino"})
	ctx.respond()

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), `"name":"yukino"`) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestRespondString(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.Status = http.StatusOK
	ctx.String("hello %s", "world")
	ctx.respond()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestRespondData(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.Status = http.StatusOK
	ctx.Data([]byte("raw bytes"))
	ctx.respond()

	if rec.Body.String() != "raw bytes" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestRespondNilBody(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.Status = http.StatusNoContent
	ctx.respond()

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body should be empty, got %q", rec.Body.String())
	}
}

func TestRespondSkipsWhenFlushed(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.flushed = true
	ctx.Status = http.StatusOK
	ctx.Body = "should not appear"
	ctx.respond()

	if rec.Body.Len() != 0 {
		t.Fatalf("flushed context should not write, got %q", rec.Body.String())
	}
}

func TestRespondWithHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.Status = http.StatusOK
	ctx.Set("X-Request-Id", "abc123")
	ctx.String("ok")
	ctx.respond()

	if rec.Header().Get("X-Request-Id") != "abc123" {
		t.Fatalf("custom header missing")
	}
}

func TestAutoStatusOKWhenBodySet(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.JSON(H{"ok": true})
	ctx.respond()

	if rec.Code != http.StatusOK {
		t.Fatalf("auto status = %d, want 200", rec.Code)
	}
}

func TestAutoStatusPreservesExplicitStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.Status = http.StatusCreated
	ctx.JSON(H{"id": 1})
	ctx.respond()

	if rec.Code != http.StatusCreated {
		t.Fatalf("explicit status = %d, want 201", rec.Code)
	}
}

func TestDeferredResponseCanBeModifiedByMiddleware(t *testing.T) {
	app := New()
	app.Use(func(ctx *Context, next func()) {
		next()
		if ctx.Status == http.StatusOK {
			ctx.Set("X-Modified", "true")
		}
	})
	app.Get("/test", func(ctx *Context, next func()) {
		ctx.Status = http.StatusOK
		ctx.JSON(H{"ok": true})
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))

	if rec.Header().Get("X-Modified") != "true" {
		t.Fatalf("middleware could not modify response after handler")
	}
}

func TestPromotedStatusVisibleToMiddlewareAfterNext(t *testing.T) {
	app := New()
	var observed int
	app.Use(func(ctx *Context, next func()) {
		next()
		observed = ctx.Status
	})
	app.Get("/auto", func(ctx *Context, next func()) {
		ctx.JSON(H{"ok": true}) // no explicit status
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auto", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if observed != http.StatusOK {
		t.Fatalf("middleware observed status %d after next(), want 200", observed)
	}
}

func TestSetStatusPreservesExplicitNotFoundWithBody(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.SetStatus(http.StatusNotFound)
	ctx.JSON(H{"message": "no such user"})
	ctx.respond()

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want explicit 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no such user") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestEmptyStatusStripsBody(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.SetStatus(http.StatusNoContent)
	ctx.JSON(H{"ignored": true})
	ctx.respond()

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("204 must not carry a body, got %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "" {
		t.Fatalf("204 must not carry Content-Type, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestRedirectDefaultsAndKeepsRedirectStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.Redirect("/login")
	ctx.respond()

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if rec.Header().Get("Location") != "/login" {
		t.Fatalf("location = %q", rec.Header().Get("Location"))
	}
	if !strings.Contains(rec.Body.String(), "Redirecting to /login") {
		t.Fatalf("body = %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	ctx = newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.SetStatus(http.StatusMovedPermanently)
	ctx.Redirect("/new-home")
	ctx.respond()

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want explicit 301", rec.Code)
	}
}

func TestUserContentTypeHeaderWins(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.Set("content-type", "application/xml")
	ctx.String("<ok/>")
	ctx.respond()

	if got := rec.Header().Get("Content-Type"); got != "application/xml" {
		t.Fatalf("content-type = %q, want application/xml", got)
	}
}
