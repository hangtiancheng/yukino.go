package dao

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// SyncAuditTaskInput is the task_input content persisted by SyncAuditRecorder.Finish.
type SyncAuditTaskInput struct {
	BizIDs   []string `json:"biz_ids"`
	SubTypes []string `json:"sub_types"`
}

// SyncAuditRecorder merges multiple sync risk-control calls within one business flow into a single risk-audit record.
//
// Design intent (design deep-dive §4): one final-video pre_create may audit several text groups; writing one record per group
// would flood the record table. The Recorder collects all (subProduct, bizID) pairs of this round in memory and Finish writes one
// summary record for audit reconciliation. Three execution modes:
//
//	Run         a single text/image, one risk call
//	RunByTick   multiple texts, ticker rate-limited dispatch (protecting downstream)
//	RunParallel multiple concurrent, aggregating with err > !pass > pass
//
// Usage:
//
//	recorder := NewSyncAuditRecorder(ctx, userID, projectID, taskStepID, "risk:5:mix_video:precreate", backend, records)
//	passed, reason, err := recorder.Run(req, subProduct) // called once or more
//	// ... other risk-control calls ...
//	recorder.Finish(passed, reason, err) // finally write one summary record
//
// Image dispatch/aggregation policies (rate-limit gate + one-vote veto) converge in domain.SyncAuditImages /
// domain.AggregateImageResults; this struct only collects audit metadata and persists it.
type SyncAuditRecorder struct {
	ctx        context.Context
	userID     int64
	projectID  string
	taskStepID int64
	taskType   string   // e.g. "risk:5:mix_video:precreate"
	subTypes   []string // all subProducts involved in this round
	bizIDs     []string // all bizIDs involved in this round
	backend    domain.AuditBackend
	records    domain.AuditRecordRepository

	mu sync.Mutex
}

// NewSyncAuditRecorder creates a recorder.
func NewSyncAuditRecorder(ctx context.Context, userID int64, projectID string, taskStepID int64, taskType string, backend domain.AuditBackend, records domain.AuditRecordRepository) *SyncAuditRecorder {
	return &SyncAuditRecorder{
		ctx:        ctx,
		userID:     userID,
		projectID:  projectID,
		taskStepID: taskStepID,
		taskType:   taskType,
		backend:    backend,
		records:    records,
	}
}

// Run executes one sync risk-control call and records the audit metadata (subProduct + bizID).
func (r *SyncAuditRecorder) Run(req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	r.mu.Lock()
	r.subTypes = append(r.subTypes, subProduct)
	r.mu.Unlock()

	passed, reason, err := r.syncAudit(r.ctx, req, subProduct)

	r.mu.Lock()
	r.bizIDs = append(r.bizIDs, req.BizID)
	r.mu.Unlock()

	return passed, reason, err
}

// RunByTick dispatches multiple texts with ticker rate limiting (protecting the risk service); aggregation semantics match RunParallel.
func (r *SyncAuditRecorder) RunByTick(reqs []*domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	if len(reqs) == 0 {
		return true, "", nil
	}
	r.mu.Lock()
	r.subTypes = append(r.subTypes, subProduct)
	r.mu.Unlock()

	// A rate-limit gate isomorphic to domain.SyncAuditImages: limits dispatch rate, not concurrency
	results := make(chan domain.ImageAuditResult, len(reqs))
	var wg sync.WaitGroup
	ticker := time.NewTicker(domain.DefaultImageTicker)
	defer ticker.Stop()

	for _, req := range reqs {
		select {
		case <-ticker.C:
		case <-r.ctx.Done():
			return false, "", r.ctx.Err()
		}
		wg.Add(1)
		go func(req *domain.RiskSubmitRequest) {
			defer wg.Done()
			passed, reason, err := r.syncAudit(r.ctx, req, subProduct)
			r.mu.Lock()
			r.bizIDs = append(r.bizIDs, req.BizID)
			r.mu.Unlock()
			results <- domain.ImageAuditResult{Passed: passed, Reason: reason, Err: err}
		}(req)
	}
	wg.Wait()
	close(results)
	return domain.AggregateImageResults(results)
}

// RunParallel executes multiple texts concurrently, aggregating with err > !pass > pass.
func (r *SyncAuditRecorder) RunParallel(reqs []*domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	if len(reqs) == 0 {
		return true, "", nil
	}
	r.mu.Lock()
	r.subTypes = append(r.subTypes, subProduct)
	r.mu.Unlock()

	results := make(chan domain.ImageAuditResult, len(reqs))
	var wg sync.WaitGroup
	for _, req := range reqs {
		wg.Add(1)
		go func(req *domain.RiskSubmitRequest) {
			defer wg.Done()
			passed, reason, err := r.syncAudit(r.ctx, req, subProduct)
			r.mu.Lock()
			r.bizIDs = append(r.bizIDs, req.BizID)
			r.mu.Unlock()
			results <- domain.ImageAuditResult{Passed: passed, Reason: reason, Err: err}
		}(req)
	}
	wg.Wait()
	close(results)
	return domain.AggregateImageResults(results)
}

// Finish is called once after all risk-control calls of the business logic complete, writing one summary risk-audit record.
//
// status: all passed → "done"; any rejection or error → "fail".
// task_input records all bizIDs + subProducts involved in this round (for audit reconciliation).
func (r *SyncAuditRecorder) Finish(passed bool, reason string, err error) {
	taskInput, _ := json.Marshal(SyncAuditTaskInput{
		BizIDs:   r.bizIDs,
		SubTypes: r.subTypes,
	})
	status := domain.StatusDone
	if !passed || err != nil {
		status = domain.StatusFail
	}

	_, _ = r.records.Insert(r.ctx, &domain.AuditRecord{
		UserID:     r.userID,
		ProjectID:  r.projectID,
		TaskStepID: r.taskStepID,
		TaskType:   r.taskType,
		TaskInput:  string(taskInput),
		Status:     status,
	})
}

// syncAudit is synchronous text/image risk control: blocks for the result and returns immediately.
//
// Returns:
//   - passed=false + err=non-nil → the risk request failed (system error)
//   - passed=false + reason=non-empty → content rejected (risk hit)
//   - passed=true → passed
//
// Image risk control is split into one image per call (domain.SyncAuditImages, rate-limited dispatch + one-vote veto);
// text risk control is a single request straight to backend.Submit.
func (r *SyncAuditRecorder) syncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	// Image risk control: split into one image per call, rate-limited dispatch + one-vote veto
	if len(req.Content.Image) > 0 {
		return domain.SyncAuditImages(ctx, r.backend, req, subProduct)
	}
	// Text risk control: a single request
	return r.backend.Submit(ctx, req, subProduct)
}
