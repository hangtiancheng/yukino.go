package yukino_http_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpapp "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

func TestTakeOverHTTPHandler(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		write  func(http.ResponseWriter)
	}{
		{"accepted", http.StatusAccepted, `{"ok":true}`, func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"ok":true}`)
		}},
		{"method_not_allowed", http.StatusMethodNotAllowed, "unsupported\n", func(w http.ResponseWriter) {
			w.Header().Set("Allow", "POST")
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}},
		{"implicit_ok", http.StatusOK, "ok", func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, "ok")
		}},
		{"empty", http.StatusOK, "", func(http.ResponseWriter) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := httpapp.New()
			app.Use(func(ctx *httpapp.Context, next func()) {
				ctx.Set("X-Request-Id", "takeover")
				ctx.String("pending body")
				next()
				ctx.SetStatus(http.StatusInternalServerError)
				ctx.String("deferred body must not be appended")
			})
			app.Get("/external", func(ctx *httpapp.Context, _ func()) {
				ctx.TakeOver()
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					tc.write(w)
				}).ServeHTTP(ctx.Writer, ctx.Request)
			})
			var logs bytes.Buffer
			server := httptest.NewUnstartedServer(app)
			server.Config.ErrorLog = log.New(&logs, "", 0)
			server.Start()
			defer server.Close()
			res, err := server.Client().Get(server.URL + "/external")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()
			server.Close()
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tc.status || string(body) != tc.body {
				t.Fatalf("response = %d %q, want %d %q", res.StatusCode, body, tc.status, tc.body)
			}
			if res.Header.Get("X-Request-Id") != "takeover" {
				t.Fatal("buffered middleware header was lost")
			}
			if logs.Len() != 0 {
				t.Fatalf("unexpected HTTP server log: %s", logs.String())
			}
		})
	}
}

func TestTakeOverDoesNotCommitOrReplaceWriter(t *testing.T) {
	app := httpapp.New()
	rec := httptest.NewRecorder()
	app.Get("/external", func(ctx *httpapp.Context, _ func()) {
		ctx.Set("X-Header", "before")
		ctx.TakeOver()
		if ctx.Writer != rec || rec.Flushed || rec.Body.Len() != 0 || ctx.Status != http.StatusNotFound {
			t.Fatal("TakeOver replaced the writer or committed a response")
		}
		ctx.Writer.Header().Set("X-Header", "handler")
		ctx.TakeOver()
		if ctx.Writer.Header().Get("X-Header") != "handler" {
			t.Fatal("repeated TakeOver overwrote handler headers")
		}
		ctx.Writer.WriteHeader(http.StatusNoContent)
	})
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/external", nil))
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestTakeOverStreaming(t *testing.T) {
	app := httpapp.New()
	app.Get("/events", func(ctx *httpapp.Context, _ func()) {
		ctx.TakeOver()
		ctx.Writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(ctx.Writer, "data: ready\n\n")
		if err := http.NewResponseController(ctx.Writer).Flush(); err != nil {
			t.Error(err)
			return
		}
		<-ctx.Request.Context().Done()
	})
	finished := make(chan struct{})
	var logs bytes.Buffer
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.ServeHTTP(w, r)
		close(finished)
	}))
	server.Config.ErrorLog = log.New(&logs, "", 0)
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	line, err := bufio.NewReader(res.Body).ReadString('\n')
	if err != nil || line != "data: ready\n" {
		t.Fatalf("stream did not flush while handler was running: %q, %v", line, err)
	}
	select {
	case <-finished:
		t.Fatal("stream handler returned before client disconnected")
	default:
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("stream handler did not observe cancellation")
	}
	server.Close()
	if logs.Len() != 0 {
		t.Fatalf("unexpected HTTP server log: %s", logs.String())
	}
}
