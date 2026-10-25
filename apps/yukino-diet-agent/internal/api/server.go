// Package api wires the diet services onto the yukino_http framework.
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"

	"github.com/hangtiancheng/yukino.go/apps/diet-agent/internal/diet"
)

// Services aggregates the diet services consumed by the HTTP handlers.
type Services struct {
	Orchestrator *diet.Orchestrator
	Sessions     *diet.SessionService
	Meals        *diet.MealService
	SlotOptions  *diet.SlotOptionService
	Evaluations  *diet.EvaluationService
	Feedback     *diet.FeedbackService
	Traces       *diet.TraceService
}

// NewApp builds the HTTP application with all diet routes under
// /api/v1/diet, a health probe, and the static frontend when its build
// output exists.
func NewApp(services *Services, staticDir string) *yukino.Application {
	app := yukino.New()
	app.Use(yukino.Logger(), yukino.Recovery(), corsMiddleware())

	api := app.Router("/api/v1/diet")
	// yukino's BindJSON has no built-in size limit; cap request bodies so a
	// malicious or buggy client cannot stream an unbounded payload.
	api.Use(bodyLimitMiddleware(maxRequestBodyBytes))
	registerRoutes(api, services)

	app.Get("/healthz", func(ctx *yukino.Context, next func()) {
		ctx.JSON(yukino.H{"status": "ok"})
	})

	if staticDir != "" {
		if info, err := os.Stat(filepath.Join(staticDir, "index.html")); err == nil && !info.IsDir() {
			registerStatic(app, staticDir)
		}
	}
	return app
}

func registerRoutes(api *yukino.Router, services *Services) {
	api.Post("/chat", adapt(services.handleChat))
	api.Post("/sessions", adapt(services.handleCreateSession))

	api.Get("/meals/personal", adapt(services.handleListPersonalMeals))
	api.Post("/meals/personal", adapt(services.handleCreatePersonalMeal))
	api.Put("/meals/personal/:mealId", adapt(services.handleUpdatePersonalMeal))
	api.Delete("/meals/personal/:mealId", adapt(services.handleDeletePersonalMeal))
	api.Get("/meals/public", adapt(services.handleListPublicMeals))

	api.Get("/slot-options", adapt(services.handleSlotOptions))
	api.Post("/evaluations", adapt(services.handleEvaluate))
	api.Post("/feedback", adapt(services.handleSaveFeedback))

	api.Get("/debug/traces", adapt(services.handleListTraces))
	api.Get("/debug/traces/:traceId", adapt(services.handleGetTrace))
	api.Get("/debug/sessions/:sessionId/traces", adapt(services.handleListSessionTraces))
	api.Put("/debug/traces/:traceId/label", adapt(services.handleLabelTrace))
}

// handlerFunc is the typed handler signature; a nil result renders 204.
type handlerFunc func(ctx *yukino.Context, userID int64) (any, error)

// adapt converts a typed handler into a yukino middleware with unified
// error rendering, replacing the Spring @RestControllerAdvice behavior.
func adapt(fn handlerFunc) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		userID, err := parseUserID(ctx.Get(diet.UserIDHeader))
		if err != nil {
			writeError(ctx, err)
			return
		}
		result, err := fn(ctx, userID)
		if err != nil {
			writeError(ctx, err)
			return
		}
		if result == nil {
			ctx.SetStatus(http.StatusNoContent)
			return
		}
		ctx.JSON(result)
	}
}

// writeError renders business errors as 400 and everything else as 500,
// both with a {"message": ...} body. Business errors may carry an
// explicit status, e.g. 404 for missing traces.
func writeError(ctx *yukino.Context, err error) {
	if dietError, ok := diet.AsError(err); ok {
		status := dietError.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		ctx.SetStatus(status)
		ctx.JSON(yukino.H{"message": dietError.Message})
		return
	}
	ctx.SetStatus(http.StatusInternalServerError)
	ctx.JSON(yukino.H{"message": err.Error()})
}

// parseUserID reads the X-User-Id header. A missing header defaults to 1
// like the Java controllers; a present but malformed value is rejected with
// a 400 instead of silently acting as user 1 (Spring rejected those with a
// type-mismatch 400 as well).
func parseUserID(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 1, nil
	}
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, diet.NewError("invalid %s header: %s", diet.UserIDHeader, raw)
	}
	return parsed, nil
}

// corsMiddleware allows browser clients on other origins (Vite dev server)
// to call the API and short-circuits preflight requests.
func corsMiddleware() yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		if origin := ctx.Get("Origin"); origin != "" {
			ctx.Set("Access-Control-Allow-Origin", origin)
			ctx.Set("Access-Control-Allow-Headers", "Content-Type, X-User-Id")
			ctx.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			ctx.Set("Access-Control-Max-Age", "600")
			ctx.Set("Vary", "Origin")
		}
		if ctx.Method == http.MethodOptions {
			ctx.SetStatus(http.StatusNoContent)
			return
		}
		next()
	}
}

// maxRequestBodyBytes caps JSON request bodies on the API routes.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// bodyLimitMiddleware wraps the request body so reads beyond the limit
// fail, which surfaces as a 400 from the handler's BindJSON error path.
func bodyLimitMiddleware(limit int64) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		if ctx.Request.Body != nil {
			ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, limit)
		}
		next()
	}
}

// registerStatic serves the built frontend: index.html at the root and the
// remaining assets through the static wildcard route. The wildcard needs a
// segment, so the bare mount path is registered separately.
func registerStatic(app *yukino.Application, staticDir string) {
	app.Get("/", func(ctx *yukino.Context, next func()) {
		// TakeOver disables deferred rendering, which is also what would
		// otherwise promote the implicit 404 to 200. Record the status
		// explicitly so the access log does not report every page load as a
		// 404; the asset route mirrors its real status through yukino's own
		// static recorder.
		ctx.SetStatus(http.StatusOK)
		ctx.TakeOver()
		http.ServeFile(ctx.Writer, ctx.Request, filepath.Join(staticDir, "index.html"))
	})
	app.Static("/", staticDir)
}
