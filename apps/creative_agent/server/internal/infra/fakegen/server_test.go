package fakegen_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/fakegen"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"
)

// startServer starts the fakegen Server and returns the instance (random port + fixed 50ms delay).
func startServer(t *testing.T) *fakegen.Server {
	t.Helper()
	srv := fakegen.NewServer(fakegen.Config{
		DelayRange: fakegen.DelayRange{Min: 50 * time.Millisecond, Max: 50 * time.Millisecond},
	})
	t.Cleanup(func() { _ = srv.Close() })
	go srv.Start(context.Background())
	return srv
}

// newCallbackServer starts a callback service recording the callback body and traceparent header, returning the channel and address.
func newCallbackServer(t *testing.T) (chan map[string]any, chan map[string]string, string) {
	t.Helper()
	gotBody := make(chan map[string]any, 1)
	gotHeaders := make(chan map[string]string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotBody <- body
		gotHeaders <- map[string]string{
			"traceparent": r.Header.Get("traceparent"),
		}
		w.Write([]byte(`{"errno":0}`))
	}))
	t.Cleanup(srv.Close)
	return gotBody, gotHeaders, srv.URL + "/cb"
}

// waitCallback waits up to 2s for one callback result.
func waitCallback[T any](t *testing.T, ch chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("callback was not triggered")
		panic("unreachable")
	}
}

// submit posts a JSON body to fakegen /submit and returns the response.
func submit(t *testing.T, srv *fakegen.Server, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.Addr()+"/submit", "application/json", bytes.NewReader(body))
	testx.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestServer_SubmitAndCallback verifies that /submit accepts the task and callback_url is called after the delay.
func TestServer_SubmitAndCallback(t *testing.T) {
	gotBody, _, callbackURL := newCallbackServer(t)
	srv := startServer(t)

	body := map[string]any{
		"project_input": `{"feat_id":"f1"}`,
		"callback_url":  callbackURL,
		"task_id":       42,
		"session_id":    "s1",
		"user_id":       float64(7),
	}
	raw, _ := json.Marshal(body)
	resp := submit(t, srv, raw)
	testx.Equal(t, http.StatusOK, resp.StatusCode)

	cb := waitCallback(t, gotBody)
	testx.Equal(t, float64(42), cb["task_id"])
	testx.Equal(t, "success", cb["task_status"])
	testx.Equal(t, "s1", cb["project_id"], "the callback body project_id should pass through the submit session_id")
	testx.Equal(t, float64(7), cb["user_id"], "the callback body user_id should pass through the submit user_id")
}

// TestServer_AutoIncrementTaskID verifies server-side auto-increment when task_id=0 (or absent).
// Asserts that SubmitResp.Data.TaskID and the callback body's task_id are the same auto-incremented value (>0).
func TestServer_AutoIncrementTaskID(t *testing.T) {
	gotBody, _, callbackURL := newCallbackServer(t)
	srv := startServer(t)

	raw, _ := json.Marshal(map[string]any{
		"project_input": `{"feat_id":"f1"}`,
		"callback_url":  callbackURL,
		"task_id":       0,
	})
	resp := submit(t, srv, raw)
	testx.Equal(t, http.StatusOK, resp.StatusCode)

	var submitResp fakegen.SubmitResp
	testx.NoError(t, json.NewDecoder(resp.Body).Decode(&submitResp))
	testx.Greater(t, submitResp.Data.TaskID, int64(0), "the response task_id should auto-increment > 0")

	cb := waitCallback(t, gotBody)
	testx.Equal(t, float64(submitResp.Data.TaskID), cb["task_id"],
		"the callback body task_id should match the response")
}

// TestServer_RejectTrailingGarbage verifies that one request parses exactly one JSON object; trailing garbage yields 400.
func TestServer_RejectTrailingGarbage(t *testing.T) {
	srv := startServer(t)

	// {"task_id":1}{"task_id":2} two concatenated JSON objects: the whole request should be rejected, not silently ignoring the second.
	resp := submit(t, srv, []byte(`{"task_id":1}{"task_id":2}`))
	testx.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestServer_RandomPortAddr verifies that an empty Addr binds a random port and Addr() returns the usable actual address.
func TestServer_RandomPortAddr(t *testing.T) {
	srv := startServer(t)

	// Issue a real HTTP request: receiving 400 proves the random-port listener works.
	resp := submit(t, srv, []byte(`{`))
	testx.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestServer_TraceparentPassthrough verifies that when the /submit request carries a real span, the callback request propagates the traceparent header.
func TestServer_TraceparentPassthrough(t *testing.T) {
	setupGlobal(t) // set the global W3C propagator + sampling provider
	gotBody, gotHeaders, callbackURL := newCallbackServer(t)
	srv := startServer(t)

	ctx, span := globalTracer().Start(context.Background(), "submit-parent")
	defer span.End()

	raw, _ := json.Marshal(map[string]any{
		"project_input": `{"feat_id":"f1"}`,
		"callback_url":  callbackURL,
		"task_id":       7,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.Addr()+"/submit", bytes.NewReader(raw))
	testx.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	// Simulate the go-learn caller (baseapi style): serialize the current span into the traceparent header
	// with tracex.InjectCarrier. http.DefaultClient does not inject automatically; the header must be set manually (consistent with baseapi.do).
	carrier := tracex.InjectCarrier(ctx)
	req.Header.Set(tracex.TraceParentKey, carrier[tracex.TraceParentKey])
	resp, err := http.DefaultClient.Do(req)
	testx.NoError(t, err)
	defer resp.Body.Close()
	testx.Equal(t, http.StatusOK, resp.StatusCode)

	cb := waitCallback(t, gotBody)
	testx.Equal(t, float64(7), cb["task_id"])

	tp := waitCallback(t, gotHeaders)
	testx.NotEmpty(t, tp["traceparent"], "the callback request should carry the traceparent header")
	// The child span is downstream of submit-parent; the traceparent header should inherit the same trace id.
	testx.Equal(t, span.SpanContext().TraceID().String(),
		traceIDFromHeader(tp["traceparent"]), "the callback traceparent should share the parent span's trace id")
}
