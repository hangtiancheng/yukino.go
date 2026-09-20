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
	"context"
	"html/template"
	"net/http"
)

type Application struct {
	root          *Router
	router        *router
	routers       []*Router
	htmlTemplates *template.Template
	funcMap       template.FuncMap
	server        *http.Server
}

func New() *Application {
	app := &Application{router: newRouter()}
	app.root = &Router{app: app}
	app.routers = []*Router{app.root}
	// Constructed here (not in Listen) so a concurrent Shutdown never races
	// with the server field assignment or silently no-ops before Listen runs.
	app.server = &http.Server{Handler: app}
	return app
}

func Default() *Application {
	app := New()
	app.Use(Logger(), Recovery())
	return app
}

func (app *Application) SetFuncMap(funcMap template.FuncMap) {
	app.funcMap = funcMap
}

func (app *Application) LoadHTMLGlob(pattern string) {
	app.htmlTemplates = template.Must(
		template.New("").Funcs(app.funcMap).ParseGlob(pattern),
	)
}

func (app *Application) Listen(addr string) error {
	app.server.Addr = addr
	return app.server.ListenAndServe()
}

func (app *Application) Shutdown(ctx context.Context) error {
	if app.server == nil {
		return nil
	}
	return app.server.Shutdown(ctx)
}

func (app *Application) Use(middlewares ...Middleware) {
	app.root.Use(middlewares...)
}

func (app *Application) Router(prefix string) *Router {
	return app.root.Router(prefix)
}

func (app *Application) Get(pattern string, handler Middleware) {
	app.root.Get(pattern, handler)
}

func (app *Application) Post(pattern string, handler Middleware) {
	app.root.Post(pattern, handler)
}

func (app *Application) Put(pattern string, handler Middleware) {
	app.root.Put(pattern, handler)
}

func (app *Application) Delete(pattern string, handler Middleware) {
	app.root.Delete(pattern, handler)
}

func (app *Application) Patch(pattern string, handler Middleware) {
	app.root.Patch(pattern, handler)
}

func (app *Application) Head(pattern string, handler Middleware) {
	app.root.Head(pattern, handler)
}

func (app *Application) Options(pattern string, handler Middleware) {
	app.root.Options(pattern, handler)
}

func (app *Application) All(pattern string, handler Middleware) {
	app.root.All(pattern, handler)
}

func (app *Application) Static(relativePath string, root string) {
	app.root.Static(relativePath, root)
}

func (app *Application) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var middlewares []Middleware
	for _, r := range app.routers {
		if matchRouterPath(req.URL.Path, r.prefix) {
			middlewares = append(middlewares, r.middlewares...)
		}
	}
	ctx := newContext(w, req)
	ctx.app = app
	app.router.handle(ctx, middlewares)
}

func compose(middlewares []Middleware, final Middleware) func(ctx *Context) {
	return func(ctx *Context) {
		index := -1
		var dispatch func(i int)
		dispatch = func(i int) {
			if i <= index {
				// koa-compose rejects with "next() called multiple times";
				// the panic surfaces as a 500 through Recovery.
				panic("yukino_http: next() called multiple times")
			}
			index = i
			if i >= len(middlewares) {
				if final != nil {
					final(ctx, func() {})
				}
				return
			}
			middlewares[i](ctx, func() { dispatch(i + 1) })
		}
		dispatch(0)
	}
}
