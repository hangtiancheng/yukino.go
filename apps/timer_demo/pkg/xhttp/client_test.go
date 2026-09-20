package xhttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestJSONClientGetWithParams(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("k")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	client := NewJSONClient()
	var resp map[string]any
	// Reserved characters must stay percent-encoded so the server parses
	// them as a single param value.
	params := map[string]string{"k": "a b&c=d"}
	if err := client.Get(context.Background(), srv.URL, nil, params, &resp); err != nil {
		t.Fatalf("Get returned err: %v", err)
	}
	if gotQuery != "a b&c=d" {
		t.Fatalf("server got k = %q, want %q", gotQuery, "a b&c=d")
	}
	if resp["ok"] != true {
		t.Fatalf("resp = %v, want ok=true", resp)
	}
}

func TestJSONClientGetWithoutParams(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	client := NewJSONClient()
	var resp map[string]any
	if err := client.Get(context.Background(), srv.URL+"/path", nil, nil, &resp); err != nil {
		t.Fatalf("Get returned err: %v", err)
	}
	if gotPath != "/path" {
		t.Fatalf("server got path = %q, want %q", gotPath, "/path")
	}
}

func TestJSONClientPostBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = io.WriteString(w, `{"done":true}`)
	}))
	defer srv.Close()

	client := NewJSONClient()
	var resp map[string]any
	if err := client.Post(context.Background(), srv.URL, nil, map[string]string{"name": "timer"}, &resp); err != nil {
		t.Fatalf("Post returned err: %v", err)
	}
	want := `{"name":"timer"}`
	if gotBody != want {
		t.Fatalf("server got body = %q, want %q", gotBody, want)
	}
	if resp["done"] != true {
		t.Fatalf("resp = %v, want done=true", resp)
	}
}

func TestJSONClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	client := NewJSONClient(WithTimeout(10 * time.Millisecond))
	var resp map[string]any
	if err := client.Get(context.Background(), srv.URL, nil, nil, &resp); err == nil {
		t.Fatal("Get with a 10ms timeout against a 200ms server returned nil error, want timeout error")
	}
}

func TestJSONClientInvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `not-json`)
	}))
	defer srv.Close()

	client := NewJSONClient()
	var resp map[string]any
	if err := client.Get(context.Background(), srv.URL, nil, nil, &resp); err == nil {
		t.Fatal("Get against a non-JSON response returned nil error, want unmarshal error")
	}
}
