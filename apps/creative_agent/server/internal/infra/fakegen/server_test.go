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

// startServer 启动 fakegen Server 并返回其实例（随机端口 + 固定 50ms 延迟）。
func startServer(t *testing.T) *fakegen.Server {
	t.Helper()
	srv := fakegen.NewServer(fakegen.Config{
		DelayRange: fakegen.DelayRange{Min: 50 * time.Millisecond, Max: 50 * time.Millisecond},
	})
	t.Cleanup(func() { _ = srv.Close() })
	go srv.Start(context.Background())
	return srv
}

// newCallbackServer 起一个记录回调 body 与 traceparent 头的回调服务，返回通道与地址。
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

// waitCallback 最多等 2s 取一次回调结果。
func waitCallback[T any](t *testing.T, ch chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("回调未触发")
		panic("unreachable")
	}
}

// submit 向 fakegen /submit 提交一个 JSON body，返回响应。
func submit(t *testing.T, srv *fakegen.Server, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.Addr()+"/submit", "application/json", bytes.NewReader(body))
	testx.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestServer_SubmitAndCallback 验证 /submit 收任务，延迟后回调 callback_url。
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
	testx.Equal(t, "s1", cb["project_id"], "回调 body project_id 应透传 submit 的 session_id")
	testx.Equal(t, float64(7), cb["user_id"], "回调 body user_id 应透传 submit 的 user_id")
}

// TestServer_AutoIncrementTaskID 验证 task_id=0（或不传）时服务端自增。
// 断言 SubmitResp.Data.TaskID 与回调 body 的 task_id 都是同一自增值（>0）。
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
	testx.Greater(t, submitResp.Data.TaskID, int64(0), "响应里的 task_id 应自增 > 0")

	cb := waitCallback(t, gotBody)
	testx.Equal(t, float64(submitResp.Data.TaskID), cb["task_id"],
		"回调 body 的 task_id 应与响应一致")
}

// TestServer_RejectTrailingGarbage 验证一次请求只解一个 JSON 对象，尾随垃圾直接 400。
func TestServer_RejectTrailingGarbage(t *testing.T) {
	srv := startServer(t)

	// {"task_id":1}{"task_id":2} 两个 JSON 连一起：应被整体拒绝，而不是静默忽略第二个。
	resp := submit(t, srv, []byte(`{"task_id":1}{"task_id":2}`))
	testx.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestServer_RandomPortAddr 验证 Addr 为空时绑定随机端口，Addr() 返回可用的实际地址。
func TestServer_RandomPortAddr(t *testing.T) {
	srv := startServer(t)

	// 走真实 HTTP 请求：能收到 400 说明随机端口监听生效。
	resp := submit(t, srv, []byte(`{`))
	testx.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestServer_TraceparentPassthrough 验证 /submit 请求带真实 span 时，回调请求透传 traceparent 头。
func TestServer_TraceparentPassthrough(t *testing.T) {
	setupGlobal(t) // 设全局 W3C propagator + 采样 provider
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
	// 模拟 go-learn 调用方（baseapi 风格）：用 tracex.InjectCarrier 把当前 span 序列化为
	// traceparent 头。http.DefaultClient 不会自动注入，必须手动设头（与 baseapi.do 一致）。
	carrier := tracex.InjectCarrier(ctx)
	req.Header.Set(tracex.TraceParentKey, carrier[tracex.TraceParentKey])
	resp, err := http.DefaultClient.Do(req)
	testx.NoError(t, err)
	defer resp.Body.Close()
	testx.Equal(t, http.StatusOK, resp.StatusCode)

	cb := waitCallback(t, gotBody)
	testx.Equal(t, float64(7), cb["task_id"])

	tp := waitCallback(t, gotHeaders)
	testx.NotEmpty(t, tp["traceparent"], "回调请求应带 traceparent 头")
	// 子 span 是 submit-parent 的下游，traceparent 头应继承同一 trace id。
	testx.Equal(t, span.SpanContext().TraceID().String(),
		traceIDFromHeader(tp["traceparent"]), "回调 traceparent 应与父 span 同 trace id")
}
