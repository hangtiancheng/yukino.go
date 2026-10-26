package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"

	"github.com/hangtiancheng/yukino.go/apps/takeout-agent/internal/takeout"
)

func TestParseUserID(t *testing.T) {
	if got, err := parseUserID(""); err != nil || got != 1 {
		t.Fatalf("parseUserID(empty) = %d, %v; want 1, nil", got, err)
	}
	if got, err := parseUserID("  "); err != nil || got != 1 {
		t.Fatalf("parseUserID(blank) = %d, %v; want 1, nil", got, err)
	}
	if got, err := parseUserID("42"); err != nil || got != 42 {
		t.Fatalf("parseUserID(42) = %d, %v; want 42, nil", got, err)
	}
	if _, err := parseUserID("abc"); err == nil {
		t.Fatal("parseUserID(abc) should fail")
	}
	if _, err := parseUserID("0"); err == nil {
		t.Fatal("parseUserID(0) should fail")
	}
	if _, err := parseUserID("-3"); err == nil {
		t.Fatal("parseUserID(-3) should fail")
	}
}

func TestBodyLimitMiddlewareRejectsOversizedBody(t *testing.T) {
	app := yukino.New()
	api := app.Router("/api")
	api.Use(bodyLimitMiddleware(16))
	api.Post("/echo", func(ctx *yukino.Context, next func()) {
		var payload map[string]any
		if err := ctx.BindJSON(&payload); err != nil {
			ctx.SetStatus(http.StatusBadRequest)
			ctx.JSON(yukino.H{"message": "invalid JSON body"})
			return
		}
		ctx.JSON(payload)
	})

	small := httptest.NewRecorder()
	app.ServeHTTP(small, httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(`{"a":1}`)))
	if small.Code != http.StatusOK {
		t.Fatalf("small body status = %d, want 200", small.Code)
	}

	large := httptest.NewRecorder()
	app.ServeHTTP(large, httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(`{"a":"`+strings.Repeat("x", 64)+`"}`)))
	if large.Code != http.StatusBadRequest {
		t.Fatalf("large body status = %d, want 400", large.Code)
	}
}

func TestWriteErrorRendersBusinessStatus(t *testing.T) {
	app := yukino.New()
	app.Get("/missing", func(ctx *yukino.Context, next func()) {
		writeError(ctx, takeout.NewNotFoundError("trace not found: %s", "trace_x"))
	})
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "message") {
		t.Fatalf("body = %s, want a message field", recorder.Body.String())
	}
}
