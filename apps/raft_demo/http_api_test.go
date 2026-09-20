package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/raft_demo/raft"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func newTestService() (*service, chan string, chan raft.ConfChange) {
	proposeC := make(chan string, 8)
	confChangeC := make(chan raft.ConfChange, 8)
	commitC := make(chan *string, 8)
	kv := newKVStore(proposeC, commitC)
	return newService(kv, proposeC, confChangeC), proposeC, confChangeC
}

func TestServeHTTPPutProposesBody(t *testing.T) {
	s, proposeC, _ := newTestService()

	req := httptest.NewRequest(http.MethodPut, "/name", strings.NewReader("yukino"))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	select {
	case got := <-proposeC:
		want := `{"key":"/name","val":"yukino"}`
		if got != want {
			t.Fatalf("proposed %q, want %q", got, want)
		}
	default:
		t.Fatal("expected a proposal on proposeC")
	}
}

// A failing request body must yield a 5xx response, not a panic.
func TestServeHTTPPutBodyError(t *testing.T) {
	s, _, _ := newTestService()

	req := httptest.NewRequest(http.MethodPut, "/name", errReader{})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestServeHTTPPostProposesConfChange(t *testing.T) {
	s, _, confChangeC := newTestService()

	req := httptest.NewRequest(http.MethodPost, "/2", strings.NewReader("ctx"))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	select {
	case cc := <-confChangeC:
		if cc.NodeID != 2 || cc.Type != raft.ConfChangeAddNode {
			t.Fatalf("unexpected conf change %+v", cc)
		}
	default:
		t.Fatal("expected a conf change on confChangeC")
	}
}

// Malformed POST targets must be rejected with 400. The old code panicked on
// "/" (out-of-range slice) and on non-numeric ids (ParseUint error).
func TestServeHTTPPostRejectsBadNodeID(t *testing.T) {
	s, _, _ := newTestService()

	for _, target := range []string{"/", "/abc"} {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader("ctx"))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST %s: status = %d, want %d", target, rec.Code, http.StatusBadRequest)
		}
	}
}
