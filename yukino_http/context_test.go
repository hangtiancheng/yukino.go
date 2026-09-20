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

func TestContextRequestHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/users/yukino?debug=true", strings.NewReader("name=body"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Custom", "hello")
	rec := httptest.NewRecorder()
	ctx := newContext(rec, req)
	ctx.Params = map[string]string{"name": "param"}

	if ctx.Param("name") != "param" || ctx.Param("missing") != "" {
		t.Fatalf("unexpected Param results")
	}
	if ctx.Query("debug") != "true" {
		t.Fatalf("Query(debug) = %q", ctx.Query("debug"))
	}
	if ctx.PostForm("name") != "body" {
		t.Fatalf("PostForm(name) = %q", ctx.PostForm("name"))
	}
	if ctx.Get("X-Custom") != "hello" {
		t.Fatalf("Get(X-Custom) = %q", ctx.Get("X-Custom"))
	}
	if ctx.Path != "/users/yukino" {
		t.Fatalf("Path = %q", ctx.Path)
	}
	if ctx.Method != "POST" {
		t.Fatalf("Method = %q", ctx.Method)
	}
}

func TestContextBindJSON(t *testing.T) {
	body := `{"name":"yukino","age":1}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := newContext(httptest.NewRecorder(), req)

	var out struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	if err := ctx.BindJSON(&out); err != nil {
		t.Fatalf("BindJSON error: %v", err)
	}
	if out.Name != "yukino" || out.Age != 1 {
		t.Fatalf("BindJSON result = %+v", out)
	}
}

func TestContextState(t *testing.T) {
	ctx := newContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.State["user"] = "admin"
	if ctx.State["user"] != "admin" {
		t.Fatalf("State[user] = %v", ctx.State["user"])
	}
}

func TestContextThrow(t *testing.T) {
	ctx := newContext(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	ctx.Throw(http.StatusForbidden, "forbidden")
	if ctx.Status != http.StatusForbidden {
		t.Fatalf("Status = %d", ctx.Status)
	}
	body, ok := ctx.Body.(H)
	if !ok || body["message"] != "forbidden" {
		t.Fatalf("Body = %v", ctx.Body)
	}
}
