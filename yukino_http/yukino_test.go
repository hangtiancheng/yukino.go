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
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNestedRouter(t *testing.T) {
	r := New()
	v1 := r.Router("/v1")
	v2 := v1.Router("/v2")
	v3 := v2.Router("/v3")
	if v2.prefix != "/v1/v2" {
		t.Fatalf("v2 prefix = %q, want /v1/v2", v2.prefix)
	}
	if v3.prefix != "/v1/v2/v3" {
		t.Fatalf("v3 prefix = %q, want /v1/v2/v3", v3.prefix)
	}
}

func TestMiddlewareOrder(t *testing.T) {
	r := New()
	var order []string
	r.Use(func(ctx *Context, next func()) {
		order = append(order, "root-before")
		next()
		order = append(order, "root-after")
	})
	api := r.Router("/api")
	api.Use(func(ctx *Context, next func()) {
		order = append(order, "api-before")
		next()
		order = append(order, "api-after")
	})
	api.Get("/ping", func(ctx *Context, next func()) {
		order = append(order, "handler")
		ctx.Status = http.StatusOK
		ctx.String("pong")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
	want := []string{"root-before", "api-before", "handler", "api-after", "root-after"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestRouterMiddlewareUsesPathBoundary(t *testing.T) {
	r := New()
	var routerMiddlewareRan bool
	r.Router("/v1").Use(func(ctx *Context, next func()) {
		routerMiddlewareRan = true
		next()
	})
	r.Get("/v10/ping", func(ctx *Context, next func()) {
		ctx.Status = http.StatusOK
		ctx.String("ok")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v10/ping", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if routerMiddlewareRan {
		t.Fatal("/v1 middleware should not run for /v10")
	}
}

func TestDefaultRecovery(t *testing.T) {
	r := Default()
	r.Get("/panic", func(ctx *Context, next func()) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Internal Server Error") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestAllHTTPMethods(t *testing.T) {
	r := New()
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	for _, m := range methods {
		method := m
		switch method {
		case "GET":
			r.Get("/test", func(ctx *Context, next func()) { ctx.Status = http.StatusOK; ctx.String("%s", method) })
		case "POST":
			r.Post("/test", func(ctx *Context, next func()) { ctx.Status = http.StatusOK; ctx.String("%s", method) })
		case "PUT":
			r.Put("/test", func(ctx *Context, next func()) { ctx.Status = http.StatusOK; ctx.String("%s", method) })
		case "DELETE":
			r.Delete("/test", func(ctx *Context, next func()) { ctx.Status = http.StatusOK; ctx.String("%s", method) })
		case "PATCH":
			r.Patch("/test", func(ctx *Context, next func()) { ctx.Status = http.StatusOK; ctx.String("%s", method) })
		case "HEAD":
			r.Head("/test", func(ctx *Context, next func()) { ctx.Status = http.StatusOK })
		case "OPTIONS":
			r.Options("/test", func(ctx *Context, next func()) { ctx.Status = http.StatusOK; ctx.String("%s", method) })
		}
	}

	for _, m := range methods {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(m, "/test", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s /test status = %d", m, rec.Code)
		}
	}
}

func TestStaticFiles(t *testing.T) {
	dir, _ := os.MkdirTemp("", "yukino_http-static-*")
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644)

	r := New()
	r.Static("/assets", dir)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/hello.txt", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "hello" {
		t.Fatalf("static hit = %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/missing.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("static miss status = %d", rec.Code)
	}
}

func TestTemplates(t *testing.T) {
	dir, _ := os.MkdirTemp("", "yukino_http-templates-*")
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "index.htmx"), []byte("Hello {{.Name | upper}}"), 0644)

	r := New()
	r.SetFuncMap(template.FuncMap{"upper": strings.ToUpper})
	r.LoadHTMLGlob(filepath.Join(dir, "*.htmx"))
	r.Get("/", func(ctx *Context, next func()) {
		ctx.Status = http.StatusCreated
		ctx.HTML("index.htmx", H{"Name": "yukino"})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusCreated || rec.Body.String() != "Hello YUKINO" {
		t.Fatalf("template response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestListenReturnsListenError(t *testing.T) {
	err := New().Listen("bad address")
	if err == nil {
		t.Fatal("expected listen error")
	}
}

func TestMatchRouterPath(t *testing.T) {
	if !matchRouterPath("/", "") || !matchRouterPath("/anything", "/") {
		t.Fatal("root router should match all paths")
	}
	if !matchRouterPath("/v1", "/v1") || !matchRouterPath("/v1/users", "/v1") {
		t.Fatal("router should match exact path and child paths")
	}
	if matchRouterPath("/v10", "/v1") || matchRouterPath("/api", "/v1") {
		t.Fatal("router matched an unrelated path")
	}
}

func TestNextCalledTwiceBecomes500(t *testing.T) {
	r := Default()
	r.Use(func(ctx *Context, next func()) {
		next()
		next() // koa-compose forbids this
	})
	r.Get("/twice", func(ctx *Context, next func()) {
		ctx.String("ok")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/twice", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for double next()", rec.Code)
	}
}

func TestRouterPrefixNormalized(t *testing.T) {
	r := New()
	var ran bool
	v1 := r.Router("v1") // missing leading slash
	v1.Use(func(ctx *Context, next func()) {
		ran = true
		next()
	})
	v1.Get("/ping", func(ctx *Context, next func()) {
		ctx.String("pong")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ping", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
	if !ran {
		t.Fatal("normalized router middleware did not run")
	}
}

func TestStaticDirectoryListingDisabled(t *testing.T) {
	dir, _ := os.MkdirTemp("", "yukino_http-staticdir-*")
	defer os.RemoveAll(dir)
	os.MkdirAll(filepath.Join(dir, "bare"), 0755)
	os.WriteFile(filepath.Join(dir, "bare", "secret.txt"), []byte("secret"), 0644)
	os.MkdirAll(filepath.Join(dir, "site"), 0755)
	os.WriteFile(filepath.Join(dir, "site", "index.html"), []byte("<h1>home</h1>"), 0644)

	r := New()
	r.Static("/assets", dir)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/bare", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("directory without index.html should be 404, got %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/site/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "home") {
		t.Fatalf("directory with index.html should serve it, got %d %q", rec.Code, rec.Body.String())
	}
}

type trackedFile struct {
	http.File
	closes *int
}

func (f trackedFile) Close() error {
	*f.closes += 1
	return f.File.Close()
}

type trackedFS struct {
	fs     http.FileSystem
	opens  *int
	closes *int
}

func (t trackedFS) Open(name string) (http.File, error) {
	f, err := t.fs.Open(name)
	if err != nil {
		return nil, err
	}
	*t.opens += 1
	return trackedFile{File: f, closes: t.closes}, nil
}

func TestStaticProbeClosesFileHandles(t *testing.T) {
	dir, _ := os.MkdirTemp("", "yukino_http-staticfd-*")
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644)
	os.MkdirAll(filepath.Join(dir, "site"), 0755)
	os.WriteFile(filepath.Join(dir, "site", "index.html"), []byte("home"), 0644)

	opens, closes := 0, 0
	fs := trackedFS{fs: http.Dir(dir), opens: &opens, closes: &closes}

	staticFileExists(fs, "hello.txt")
	staticFileExists(fs, "site")
	staticFileExists(fs, "missing.txt")

	if opens == 0 {
		t.Fatal("probe never opened a file")
	}
	if opens != closes {
		t.Fatalf("fd leak: %d opened, %d closed", opens, closes)
	}
}
