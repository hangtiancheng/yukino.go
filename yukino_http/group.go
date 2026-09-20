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
	"log"
	"net/http"
	"path"
	"strings"
)

type Router struct {
	prefix      string
	middlewares []Middleware
	parent      *Router
	app         *Application
}

func (r *Router) Router(prefix string) *Router {
	newRouter := &Router{
		prefix: r.prefix + normalizePrefix(prefix),
		parent: r,
		app:    r.app,
	}
	r.app.routers = append(r.app.routers, newRouter)
	return newRouter
}

// normalizePrefix keeps trie registration and matchRouterPath consistent:
// without it, Router("v1") registers routes reachable at /v1/... (parsePattern
// collapses slashes) while its middleware silently never matches.
func normalizePrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	for len(prefix) > 1 && strings.HasSuffix(prefix, "/") {
		prefix = prefix[:len(prefix)-1]
	}
	if prefix == "/" {
		return ""
	}
	return prefix
}

func (r *Router) Use(middlewares ...Middleware) {
	r.middlewares = append(r.middlewares, middlewares...)
}

func (r *Router) addRoute(method string, comp string, handler Middleware) {
	pattern := r.prefix + comp
	log.Printf("Route %4s - %s", method, pattern)
	r.app.router.addRoute(method, pattern, handler)
}

func (r *Router) Get(pattern string, handler Middleware) {
	r.addRoute("GET", pattern, handler)
}

func (r *Router) Post(pattern string, handler Middleware) {
	r.addRoute("POST", pattern, handler)
}

func (r *Router) Put(pattern string, handler Middleware) {
	r.addRoute("PUT", pattern, handler)
}

func (r *Router) Delete(pattern string, handler Middleware) {
	r.addRoute("DELETE", pattern, handler)
}

func (r *Router) Patch(pattern string, handler Middleware) {
	r.addRoute("PATCH", pattern, handler)
}

func (r *Router) Head(pattern string, handler Middleware) {
	r.addRoute("HEAD", pattern, handler)
}

func (r *Router) Options(pattern string, handler Middleware) {
	r.addRoute("OPTIONS", pattern, handler)
}

func (r *Router) All(pattern string, handler Middleware) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	for _, m := range methods {
		r.addRoute(m, pattern, handler)
	}
}

func (r *Router) Static(relativePath string, root string) {
	handler := r.createStaticHandler(relativePath, http.Dir(root))
	urlPattern := path.Join(relativePath, "/*filepath")
	r.Get(urlPattern, handler)
}

func (r *Router) createStaticHandler(relativePath string, fs http.FileSystem) Middleware {
	absolutePath := path.Join(r.prefix, relativePath)
	fileServer := http.StripPrefix(absolutePath, http.FileServer(fs))
	return func(ctx *Context, next func()) {
		file := ctx.Param("filepath")
		if !staticFileExists(fs, file) {
			ctx.Status = http.StatusNotFound
			ctx.statusSet = true
			return
		}
		// flush deferred headers (e.g. CORS) before handing off to FileServer,
		// otherwise headers set via ctx.Set in upstream middlewares are lost.
		header := ctx.Writer.Header()
		for k, v := range ctx.headers {
			header.Set(k, v)
		}
		ctx.flushed = true
		fileServer.ServeHTTP(&statusRecorder{ResponseWriter: ctx.Writer, status: &ctx.Status}, ctx.Request)
	}
}

// staticFileExists probes the target and closes the handle immediately.
// Directories are only served when they contain an index.html, matching
// koa-static; bare directory listings are never exposed.
func staticFileExists(fs http.FileSystem, name string) bool {
	f, err := fs.Open(name)
	if err != nil {
		return false
	}
	stat, err := f.Stat()
	_ = f.Close()
	if err != nil {
		return false
	}
	if !stat.IsDir() {
		return true
	}
	index, err := fs.Open(path.Join(name, "index.html"))
	if err != nil {
		return false
	}
	_ = index.Close()
	return true
}

// statusRecorder mirrors the status FileServer writes into ctx.Status so
// observers like Logger see the real code instead of the pre-flush default.
type statusRecorder struct {
	http.ResponseWriter
	status *int
	wrote  bool
}

func (w *statusRecorder) WriteHeader(code int) {
	if !w.wrote {
		*w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if !w.wrote {
		*w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

func matchRouterPath(requestPath, routerPrefix string) bool {
	if routerPrefix == "" || routerPrefix == "/" {
		return true
	}
	if !strings.HasPrefix(requestPath, routerPrefix) {
		return false
	}
	return len(requestPath) == len(routerPrefix) || requestPath[len(routerPrefix)] == '/'
}
