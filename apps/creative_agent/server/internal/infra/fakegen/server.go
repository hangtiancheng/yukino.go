package fakegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Server 是模拟生成服务：无真实生成逻辑，收到 /submit 后随机延迟回调 callback_url。
//
// 架构位置：站在 base-server 的"下游"，模拟真实系统的外部生成服务。
// base-server 不知道它存在——只看到"有个上游来记账，有个下游算子来回调"。
type Server struct {
	httpServer *http.Server
	ln         net.Listener
	logger     *zap.Logger

	delayMin time.Duration
	delayMax time.Duration

	// nextTaskID 自增产线 ID（原子计数，客户端未传 task_id 时使用）。
	nextTaskID atomic.Int64
}

// Config 持有 Server 的配置。
type Config struct {
	Addr       string     // 监听地址，如 ":8091"；空=随机端口
	DelayRange DelayRange // 回调随机延迟范围
	Logger     *zap.Logger
}

// DelayRange 是回调延迟范围。
type DelayRange struct {
	Min time.Duration
	Max time.Duration
}

// SubmitReq 是 /submit 请求体。
type SubmitReq struct {
	ProjectInput string `json:"project_input"` // base-server 组好的算子入参
	CallbackURL  string `json:"callback_url"`  // go-learn 回调地址
	TaskID       int64  `json:"task_id"`       // 产线 ID（客户端传 0 时服务端自增）
	SessionID    string `json:"session_id"`    // 透传给回调 body 的 project_id
	UserID       int64  `json:"user_id"`       // 透传给回调 body 的 user_id
}

// SubmitResp 是 /submit 响应体。
type SubmitResp struct {
	Code int        `json:"code"`
	Data SubmitData `json:"data"`
}

// SubmitData 是 /submit 响应 data 部分。
type SubmitData struct {
	TaskID int64  `json:"task_id"`
	Status string `json:"status"`
}

// NewServer 创建模拟生成服务。
func NewServer(cfg Config) *Server {
	if cfg.DelayRange.Min <= 0 {
		cfg.DelayRange.Min = time.Second
	}
	if cfg.DelayRange.Max <= 0 {
		cfg.DelayRange.Max = 5 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.Addr == "" {
		// net.Listen 空地址在 go1.26 会自动选随机端口，但显式 ":0" 更明确、可移植。
		cfg.Addr = ":0"
	}
	s := &Server{
		logger:   cfg.Logger,
		delayMin: cfg.DelayRange.Min,
		delayMax: cfg.DelayRange.Max,
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		panic("fakegen listen: " + err.Error())
	}
	s.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", s.handleSubmit)
	s.httpServer = &http.Server{Handler: mux}
	return s
}

// Addr 返回实际监听地址（测试用随机端口时取真实地址）。
func (s *Server) Addr() string { return "http://" + s.ln.Addr().String() }

// Start 启动 HTTP 服务（阻塞，应在 goroutine 中调用）。
func (s *Server) Start(ctx context.Context) error {
	err := s.httpServer.Serve(s.ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown 优雅关闭。
func (s *Server) Shutdown(ctx context.Context) error { return s.httpServer.Shutdown(ctx) }

// Close 关闭 HTTP 服务（供 defer 调用，等效带超时的优雅关闭）。
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Shutdown(ctx)
}

// Submit 同步向 fakegen 提交生成任务，返回分配的产线 task_id。
// 带 traceparent 透传（对齐 baseapi.do）。
func (s *Server) Submit(ctx context.Context, req *SubmitReq) (int64, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Addr()+"/submit", bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	carrier := tracex.InjectCarrier(ctx)
	if tp, ok := carrier[tracex.TraceParentKey]; ok {
		hreq.Header.Set(tracex.TraceParentKey, tp)
	}
	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("fakegen http %d", resp.StatusCode)
	}
	var out SubmitResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Data.TaskID, nil
}

// handleSubmit 处理 /submit：登记任务 → 异步随机延迟回调。
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	// 用 io.ReadAll + json.Unmarshal 一次解完整 body：拒绝尾随垃圾（与 baseapi.do 一致）。
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var req SubmitReq
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.TaskID == 0 {
		req.TaskID = s.nextTaskID.Add(1)
	}

	// 恢复入站 traceparent：fakegen 没有 OTelTracing 中间件，r.Context() 里没有 span，
	// 必须用 tracex.ContextFromCarrier 从 HTTP 头 Extract 回远端父 span，再建真实子 span。
	// 对齐 tracex/seda.go 的做法（con_reader 入口恢复 span context）。
	reqCtx := tracex.ContextFromCarrier(r.Context(), map[string]string{
		tracex.TraceParentKey: r.Header.Get(tracex.TraceParentKey),
	})
	// 在请求边界建真实 span（对齐 OTelTracing 中间件做法）：父 span 的 ctx 是恢复出的远端 span，
	// 直接把它塞进异步回调的 ctx 会让 traceparent 链断掉（远端 span 无法直接传给下一个进程）。
	// 建一个真实子 span 后，回调 goroutine 用它派生子孙 span。
	// 注意：必须在 WithoutCancel 之前用 live ctx 建 span——Start 需要能从 ctx 找到父 span。
	ctx, span := s.tracer().Start(reqCtx, "fakegen /submit")
	defer span.End()
	// 去掉请求取消：handler 返回后请求 ctx 即被取消（net/http 约定），异步回调必须脱离它。
	ctx = context.WithoutCancel(ctx)

	// 异步：随机延迟后回调（模拟"生成完成"）。
	delay := s.randomDelay()
	go s.delayedCallback(ctx, req, delay)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SubmitResp{
		Code: 0,
		Data: SubmitData{TaskID: req.TaskID, Status: "submitted"},
	})
}

// tracer 返回 fakegen 自己的 tracer（用全局 provider，未配置时是 no-op）。
func (s *Server) tracer() trace.Tracer {
	return otel.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/fakegen")
}

// delayedCallback 随机延迟后 POST callback_url，携带符合 base-server CallbackReq 契约的 body。
func (s *Server) delayedCallback(ctx context.Context, req SubmitReq, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return
	}

	// 回调 body：对齐 base-server CallbackReq（task_status=success 表示生成完成）。
	// project_id/user_id 透传 /submit 请求身份字段，供 go-learn 回调侧定位 Waiting 任务。
	cb := map[string]any{
		"task_id":      req.TaskID,
		"project_id":   req.SessionID,
		"user_id":      req.UserID,
		"task_status":  "success",
		"special_info": "eyJpbWFnZXMiOlsiY2Y6Ly9mYWtlL2ltZy5wbmciXX0=", // base64({"images":["cf://fake/img.png"]})
	}
	raw, _ := json.Marshal(cb)

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.CallbackURL, bytes.NewReader(raw))
	if err != nil {
		s.logger.Error("fakegen build callback failed", zap.Error(err))
		return
	}
	hreq.Header.Set("Content-Type", "application/json")
	// 透传 traceparent（从 /submit 请求的 ctx 提取）。
	carrier := tracex.InjectCarrier(ctx)
	if tp, ok := carrier["traceparent"]; ok {
		hreq.Header.Set(tracex.TraceParentKey, tp)
	}

	hresp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		s.logger.Error("fakegen callback do failed", zap.Error(err))
		return
	}
	_ = hresp.Body.Close()
}

// randomDelay 在 [min, max] 内随机取延迟。
// 用 UnixNano 取模近似随机，避免引入 crypto/rand（非安全场景够用）。
func (s *Server) randomDelay() time.Duration {
	if s.delayMax <= s.delayMin {
		return s.delayMin
	}
	d := s.delayMax - s.delayMin
	fraction := float64(time.Now().UnixNano() % 100)
	return s.delayMin + time.Duration(float64(d)*fraction/100)
}
