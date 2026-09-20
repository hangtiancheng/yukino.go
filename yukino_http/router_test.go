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
	"reflect"
	"testing"
)

func newTestRouter() *router {
	r := newRouter()
	r.addRoute("GET", "/", func(ctx *Context, next func()) {})
	r.addRoute("GET", "/hello/:name", func(ctx *Context, next func()) {})
	r.addRoute("GET", "/hello/b/c", func(ctx *Context, next func()) {})
	r.addRoute("GET", "/hi/:name", func(ctx *Context, next func()) {})
	r.addRoute("GET", "/assets/*filepath", func(ctx *Context, next func()) {})
	return r
}

func TestParsePattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    []string
	}{
		{pattern: "/", want: []string{}},
		{pattern: "/p/:name", want: []string{"p", ":name"}},
		{pattern: "/p/*", want: []string{"p", "*"}},
		{pattern: "/p/*name/*", want: []string{"p", "*name"}},
		{pattern: "//p//:name//", want: []string{"p", ":name"}},
	}
	for _, tt := range tests {
		if got := parsePattern(tt.pattern); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("parsePattern(%q) = %#v, want %#v", tt.pattern, got, tt.want)
		}
	}
}

func TestGetRoute(t *testing.T) {
	r := newTestRouter()
	n, params := r.getRoute("GET", "/hello/yukino")
	if n == nil || n.pattern != "/hello/:name" {
		t.Fatalf("matched route = %v, want /hello/:name", n)
	}
	if params["name"] != "yukino" {
		t.Fatalf("name param = %q, want yukino", params["name"])
	}

	n, params = r.getRoute("GET", "/assets/css/test.css")
	if n == nil || n.pattern != "/assets/*filepath" || params["filepath"] != "css/test.css" {
		t.Fatalf("wildcard route = %v params=%v", n, params)
	}

	if n, params = r.getRoute("POST", "/hello/yukino"); n != nil || params != nil {
		t.Fatalf("unexpected POST match: %v %v", n, params)
	}
	if n, params = r.getRoute("GET", "/missing"); n != nil || params != nil {
		t.Fatalf("unexpected missing route match: %v %v", n, params)
	}
}

func TestGetRoutes(t *testing.T) {
	r := newTestRouter()
	if nodes := r.getRoutes("GET"); len(nodes) != 5 {
		t.Fatalf("GET route count = %d, want 5", len(nodes))
	}
	if nodes := r.getRoutes("POST"); nodes != nil {
		t.Fatalf("POST routes = %v, want nil", nodes)
	}
}

func TestStaticRouteTakesPriorityOverWildcard(t *testing.T) {
	r := newRouter()
	var matched string
	r.addRoute("GET", "/files/*filepath", func(ctx *Context, next func()) { matched = "wildcard" })
	r.addRoute("GET", "/files/exact", func(ctx *Context, next func()) { matched = "exact" })

	ctx := newContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/files/exact", nil))
	r.handle(ctx, nil)
	if matched != "exact" {
		t.Fatalf("matched %q, want exact", matched)
	}
}

func TestRouterHandleNotFound(t *testing.T) {
	r := newRouter()
	rec := httptest.NewRecorder()
	ctx := newContext(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	r.handle(ctx, nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestNodeStringAndTravel(t *testing.T) {
	r := newTestRouter()
	nodes := r.getRoutes("GET")
	if len(nodes) == 0 {
		t.Fatal("expected routes")
	}
	if got := nodes[0].String(); got == "" {
		t.Fatal("node String returned empty value")
	}
}
