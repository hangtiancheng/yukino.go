package baseapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/baseapi"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// mockBaseServer starts an httptest server recording received requests and returning fixed responses by path.
func mockBaseServer(t *testing.T) (*httptest.Server, map[string][]byte) {
	t.Helper()
	received := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		received[r.URL.Path] = body
		switch r.URL.Path {
		case "/inner/creation/pre_create":
			w.Write([]byte(`{"errno":0,"data":{"status":0,"task_type":"push_card","task_arguments":{},"project_input":"eyJpbnB1dCI6InRlc3QifQ=="}}`))
		case "/inner/creation/create":
			w.Write([]byte(`{"errno":0,"data":{"status":"success","task_type":"push_card","task_arguments":{},"member_cost":10,"remain_score":90}}`))
		case "/inner/creation/async_post_create":
			w.Write([]byte(`{"errno":0,"data":{"status":"success","task_type":"push_card","task_arguments":{}}}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"errno":1,"errmsg":"not found"}`))
		}
	}))
	return srv, received
}

// TestClient_PreCreate verifies PreCreate request construction and response parsing.
func TestClient_PreCreate(t *testing.T) {
	srv, received := mockBaseServer(t)
	defer srv.Close()

	c := baseapi.NewClient(baseapi.ClientConfig{BaseURL: srv.URL})
	resp, err := c.PreCreate(context.Background(), &baseapi.PreCreateReq{
		UserID:       1,
		SessionID:    "s1",
		ProjectType:  "mix_video",
		RoleType:     3,
		ProjectInput: `{"input":"test"}`,
	})
	testx.NoError(t, err)
	testx.NotNil(t, resp.Data)
	testx.Equal(t, 0, resp.Data.Status)
	testx.Equal(t, "push_card", resp.Data.TaskType)

	// Verify the request body fields
	var req map[string]any
	testx.NoError(t, json.Unmarshal(received["/inner/creation/pre_create"], &req))
	testx.Equal(t, float64(1), req["user_id"])
	testx.Equal(t, "s1", req["session_id"])
	testx.Equal(t, "mix_video", req["project_type"])
	testx.Equal(t, float64(3), req["role_type"])
}

// TestClient_Create verifies the Create request and response.
func TestClient_Create(t *testing.T) {
	srv, _ := mockBaseServer(t)
	defer srv.Close()

	c := baseapi.NewClient(baseapi.ClientConfig{BaseURL: srv.URL})
	resp, err := c.Create(context.Background(), &baseapi.CreateReq{
		UserID:       1,
		SessionID:    "s1",
		ProjectType:  "mix_video",
		ProjectInput: map[string]any{"input": "test"},
		Title:        "test",
	})
	testx.NoError(t, err)
	testx.NotNil(t, resp.Data)
	testx.Equal(t, "success", resp.Data.Status)
}

// TestClient_TraceHeader verifies that outbound requests carry the W3C traceparent header (trace propagation).
func TestClient_TraceHeader(t *testing.T) {
	srv, _ := mockBaseServer(t)
	defer srv.Close()

	// A real span is required to inject traceparent; the global provider defaults to no-op, so replace it temporarily and restore.
	tp, _ := otelx.NewMemoryExporter()
	oldTP := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(oldTP)
		otel.SetTextMapPropagator(oldProp)
		_ = tp.Shutdown(context.Background())
	})

	// A self-built http server records the received traceparent header.
	var gotTrace string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTrace = r.Header.Get(tracex.TraceParentKey)
		w.Write([]byte(`{"errno":0,"data":{"status":0,"task_type":"push_card"}}`))
	}))
	defer ts.Close()

	ctx, span := otel.Tracer("baseapi-test").Start(context.Background(), "pre_create")
	defer span.End()

	c := baseapi.NewClient(baseapi.ClientConfig{BaseURL: ts.URL})
	_, err := c.PreCreate(ctx, &baseapi.PreCreateReq{UserID: 1})
	testx.NoError(t, err)

	// The traceparent header looks like 00-<32-char traceID>-<16-char spanID>-01
	testx.Equal(t, "00-"+span.SpanContext().TraceID().String()+"-"+span.SpanContext().SpanID().String()+"-01", gotTrace,
		"the W3C traceparent header should be propagated")
}

// TestClient_BusinessErrno verifies that HTTP 200 with business errno != 0 returns an error (Data unusable).
func TestClient_BusinessErrno(t *testing.T) {
	// The self-built server returns HTTP 200 with errno != 0 (the base-server API-call failure shape).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errno":1001,"errmsg":"no permission"}`))
	}))
	defer ts.Close()

	c := baseapi.NewClient(baseapi.ClientConfig{BaseURL: ts.URL})
	_, err := c.PreCreate(context.Background(), &baseapi.PreCreateReq{UserID: 1})
	testx.Error(t, err)
	testx.Contains(t, err.Error(), "baseapi business errno 1001")
}

// TestClient_AsyncPostCreate verifies the async registration request.
func TestClient_AsyncPostCreate(t *testing.T) {
	srv, _ := mockBaseServer(t)
	defer srv.Close()

	c := baseapi.NewClient(baseapi.ClientConfig{BaseURL: srv.URL})
	resp, err := c.AsyncPostCreate(context.Background(), &baseapi.AsyncPostCreateReq{
		UserID:      1,
		SessionID:   "s1",
		TaskOutput:  `{"errno":0,"data":{"task_id":42,"task_status":"success"}}`,
		Status:      "success",
		RoleType:    5,
		ProjectType: "mix_video",
	})
	testx.NoError(t, err)
	testx.NotNil(t, resp.Data)
	testx.Equal(t, "success", resp.Data.Status)
}
