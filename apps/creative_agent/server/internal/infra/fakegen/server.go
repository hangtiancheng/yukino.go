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

// Server is the simulated generation service: it has no real generation logic; after receiving /submit it calls back callback_url with a random delay.
//
// Architectural position: it stands "downstream" of base-server, simulating the external generation service of the real system.
// base-server does not know it exists — it only sees "an upstream coming to account, and a downstream operator calling back".
type Server struct {
	httpServer *http.Server
	ln         net.Listener
	logger     *zap.Logger

	delayMin time.Duration
	delayMax time.Duration

	// nextTaskID increments the pipeline ID (atomic counter, used when the client does not pass task_id).
	nextTaskID atomic.Int64
}

// Config holds the Server's configuration.
type Config struct {
	Addr       string     // listen address, e.g. ":8091"; empty = random port
	DelayRange DelayRange // random delay range for callbacks
	Logger     *zap.Logger
}

// DelayRange is the callback delay range.
type DelayRange struct {
	Min time.Duration
	Max time.Duration
}

// SubmitReq is the /submit request body.
type SubmitReq struct {
	ProjectInput string `json:"project_input"` // operator inputs assembled by base-server
	CallbackURL  string `json:"callback_url"`  // go-learn callback address
	TaskID       int64  `json:"task_id"`       // pipeline ID (server increments when the client passes 0)
	SessionID    string `json:"session_id"`    // passed through to the callback body's project_id
	UserID       int64  `json:"user_id"`       // passed through to the callback body's user_id
}

// SubmitResp is the /submit response body.
type SubmitResp struct {
	Code int        `json:"code"`
	Data SubmitData `json:"data"`
}

// SubmitData is the data part of the /submit response.
type SubmitData struct {
	TaskID int64  `json:"task_id"`
	Status string `json:"status"`
}

// NewServer creates the simulated generation service.
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
		// net.Listen with an empty address picks a random port automatically in go1.26, but an explicit ":0" is clearer and more portable.
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

// Addr returns the actual listen address (the real address when tests use a random port).
func (s *Server) Addr() string { return "http://" + s.ln.Addr().String() }

// Start starts the HTTP service (blocking; should be called in a goroutine).
func (s *Server) Start(ctx context.Context) error {
	err := s.httpServer.Serve(s.ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown performs a graceful shutdown.
func (s *Server) Shutdown(ctx context.Context) error { return s.httpServer.Shutdown(ctx) }

// Close shuts down the HTTP service (for defer; equivalent to a graceful shutdown with a timeout).
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Shutdown(ctx)
}

// Submit synchronously submits a generation task to fakegen and returns the assigned pipeline task_id.
// It propagates traceparent (aligned with baseapi.do).
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

// handleSubmit handles /submit: register the task → callback asynchronously after a random delay.
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	// Parse the full body at once with io.ReadAll + json.Unmarshal: rejects trailing garbage (consistent with baseapi.do).
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

	// Restore the inbound traceparent: fakegen has no OTelTracing middleware and r.Context() carries no span,
	// so tracex.ContextFromCarrier must Extract the remote parent span from the HTTP headers before creating a real child span.
	// Aligned with tracex/seda.go (the con_reader entry restores the span context).
	reqCtx := tracex.ContextFromCarrier(r.Context(), map[string]string{
		tracex.TraceParentKey: r.Header.Get(tracex.TraceParentKey),
	})
	// Create a real span at the request boundary (aligned with the OTelTracing middleware): the parent span's ctx is the restored remote span;
	// stuffing it directly into the async callback's ctx would break the traceparent chain (a remote span cannot be passed directly to the next process).
	// After creating a real child span, the callback goroutine derives descendant spans from it.
	// Note: the span must be created with the live ctx before WithoutCancel — Start needs to find the parent span from ctx.
	ctx, span := s.tracer().Start(reqCtx, "fakegen /submit")
	defer span.End()
	// Detach request cancellation: the request ctx is canceled once the handler returns (net/http convention); the async callback must detach from it.
	ctx = context.WithoutCancel(ctx)

	// Async: callback after a random delay (simulating "generation complete").
	delay := s.randomDelay()
	go s.delayedCallback(ctx, req, delay)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SubmitResp{
		Code: 0,
		Data: SubmitData{TaskID: req.TaskID, Status: "submitted"},
	})
}

// tracer returns fakegen's own tracer (uses the global provider; no-op when unconfigured).
func (s *Server) tracer() trace.Tracer {
	return otel.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/fakegen")
}

// delayedCallback POSTs callback_url after a random delay, carrying a body conforming to the base-server CallbackReq contract.
func (s *Server) delayedCallback(ctx context.Context, req SubmitReq, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return
	}

	// Callback body: aligned with base-server CallbackReq (task_status=success means generation complete).
	// project_id/user_id pass through the /submit request identity fields so go-learn's callback side can locate the Waiting task.
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
	// Propagate traceparent (extracted from the /submit request's ctx).
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

// randomDelay picks a random delay within [min, max].
// UnixNano modulo approximates randomness, avoiding crypto/rand (good enough for non-security contexts).
func (s *Server) randomDelay() time.Duration {
	if s.delayMax <= s.delayMin {
		return s.delayMin
	}
	d := s.delayMax - s.delayMin
	fraction := float64(time.Now().UnixNano() % 100)
	return s.delayMin + time.Duration(float64(d)*fraction/100)
}
