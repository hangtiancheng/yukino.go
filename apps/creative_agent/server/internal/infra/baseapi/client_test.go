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

// mockBaseServer 起一个 httptest server，记录收到的请求并按路径返回固定响应。
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

// TestClient_PreCreate 验证 PreCreate 请求构造与响应解析。
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

	// 验证请求体字段
	var req map[string]any
	testx.NoError(t, json.Unmarshal(received["/inner/creation/pre_create"], &req))
	testx.Equal(t, float64(1), req["user_id"])
	testx.Equal(t, "s1", req["session_id"])
	testx.Equal(t, "mix_video", req["project_type"])
	testx.Equal(t, float64(3), req["role_type"])
}

// TestClient_Create 验证 Create 请求与响应。
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

// TestClient_TraceHeader 验证出站请求携带 W3C traceparent 头（trace 透传）。
func TestClient_TraceHeader(t *testing.T) {
	srv, _ := mockBaseServer(t)
	defer srv.Close()

	// 用真实 span 才能注入 traceparent；全局 provider 默认 no-op，需临时替换并恢复。
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

	// 自建 http server 记录收到的 traceparent 头。
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

	// traceparent 头形如 00-<32位traceID>-<16位spanID>-01
	testx.Equal(t, "00-"+span.SpanContext().TraceID().String()+"-"+span.SpanContext().SpanID().String()+"-01", gotTrace,
		"应透传 W3C traceparent 头")
}

// TestClient_BusinessErrno 验证 HTTP 200 但业务 errno != 0 时返回错误（Data 不可用）。
func TestClient_BusinessErrno(t *testing.T) {
	// 自建 server 返回 HTTP 200 但 errno != 0（base-server 接口调用失败形态）。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errno":1001,"errmsg":"no permission"}`))
	}))
	defer ts.Close()

	c := baseapi.NewClient(baseapi.ClientConfig{BaseURL: ts.URL})
	_, err := c.PreCreate(context.Background(), &baseapi.PreCreateReq{UserID: 1})
	testx.Error(t, err)
	testx.Contains(t, err.Error(), "baseapi business errno 1001")
}

// TestClient_AsyncPostCreate 验证异步登记请求。
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
