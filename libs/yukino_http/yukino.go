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
