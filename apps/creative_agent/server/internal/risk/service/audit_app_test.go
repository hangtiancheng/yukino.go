package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// --- Stub implementations ---

// stubBackend is a backend that always passes.
type stubBackend struct{}

func (stubBackend) Submit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	return true, "", nil
}

// scriptedBackend is a fake backend returning scripted outcomes in order (index guarded by a mutex for concurrency).
type auditOutcome struct {
	passed bool
	reason string
	err    error
}

type scriptedBackend struct {
	mu      sync.Mutex
	idx     int
	results []auditOutcome
}

func (b *scriptedBackend) Submit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.idx >= len(b.results) {
		return true, "", nil
	}
	r := b.results[b.idx]
	b.idx++
	return r.passed, r.reason, r.err
}

// stubRecords is the in-memory repository.
type stubRecords struct {
	records []*domain.AuditRecord
}

func (s *stubRecords) Insert(ctx context.Context, r *domain.AuditRecord) (uint64, error) {
	r.ID = uint64(len(s.records) + 1)
	s.records = append(s.records, r)
	return r.ID, nil
}
func (s *stubRecords) Get(ctx context.Context, id uint64) (*domain.AuditRecord, error) {
	for _, r := range s.records {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, domain.ErrRecordNotFound
}
func (s *stubRecords) Update(ctx context.Context, id uint64, assigns map[string]any) error {
	for _, r := range s.records {
		if r.ID == id {
			if v, ok := assigns["task_input"]; ok {
				r.TaskInput = v.(string)
			}
			return nil
		}
	}
	return domain.ErrRecordNotFound
}
func (s *stubRecords) MarkTerminal(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error) {
	return true, nil
}

// stubSink is a fake sink recording compensation-write calls.
type stubSink struct {
	mu          sync.Mutex
	calls       int
	lastReq     *domain.RiskSubmitRequest
	lastProduct string
	lastID      int64
}

func (s *stubSink) InsertAuditCompensation(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string, recordID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastReq = req
	s.lastProduct = subProduct
	s.lastID = recordID
	return nil
}

// stubBlacklist is a fake blacklist checker that always hits.
type stubBlacklist struct {
	hit bool
}

func (b stubBlacklist) IsBlacklisted(ctx context.Context, userID int64, subProduct string) bool {
	return b.hit
}

// --- Sync submission ---

func TestSyncAuditPasses(t *testing.T) {
	app := service.NewRiskAuditApp(stubBackend{}, &stubRecords{}, nil, nil)
	passed, reason, err := app.SyncAudit(context.Background(), &domain.RiskSubmitRequest{
		Content: domain.AuditContent{Text: "你好"},
	}, "test_sub")
	testx.NoError(t, err)
	testx.True(t, passed)
	testx.Equal(t, "", reason)
}

func TestSyncAuditBlocksOnSensitiveWord(t *testing.T) {
	backend := &scriptedBackend{results: []auditOutcome{
		{passed: false, reason: "内容包含敏感词[赌博]"},
	}}
	app := service.NewRiskAuditApp(backend, &stubRecords{}, nil, nil)
	passed, reason, err := app.SyncAudit(context.Background(), &domain.RiskSubmitRequest{
		Content: domain.AuditContent{Text: "这里有赌博"},
	}, "test_sub")
	testx.NoError(t, err)
	testx.False(t, passed)
	testx.Contains(t, reason, "赌博")
}

func TestSyncAuditImageOneVetoBlocksAll(t *testing.T) {
	// 3 images, the 2nd rejected → one-vote veto
	backend := &scriptedBackend{results: []auditOutcome{
		{passed: true},
		{passed: false, reason: "含违规图"},
		{passed: true},
	}}
	app := service.NewRiskAuditApp(backend, &stubRecords{}, nil, nil)
	req := &domain.RiskSubmitRequest{UserID: 1, ThirdID: "p1", Content: domain.AuditContent{
		Image: []domain.AuditImage{{URL: "a.png"}, {URL: "b.png"}, {URL: "c.png"}},
	}}
	passed, reason, err := app.SyncAudit(context.Background(), req, "img_sub")
	testx.NoError(t, err)
	testx.False(t, passed, "one-vote veto: any rejected image fails the whole round")
	testx.Equal(t, "含违规图", reason)
}

func TestSyncAuditImageErrorPriorityOverFail(t *testing.T) {
	// Aggregation semantics err > !pass > pass: an image error takes priority
	backend := &scriptedBackend{results: []auditOutcome{
		{passed: false, reason: "违规"},
		{passed: false, err: errors.New("backend down")},
	}}
	app := service.NewRiskAuditApp(backend, &stubRecords{}, nil, nil)
	req := &domain.RiskSubmitRequest{UserID: 1, ThirdID: "p1", Content: domain.AuditContent{
		Image: []domain.AuditImage{{URL: "a.png"}, {URL: "b.png"}},
	}}
	passed, reason, err := app.SyncAudit(context.Background(), req, "img_sub")
	testx.Error(t, err, "errors take priority")
	testx.False(t, passed)
	testx.Empty(t, reason)
}

// --- Async submission ---

func TestAsyncAuditInsertsAuditingRecord(t *testing.T) {
	records := &stubRecords{}
	app := service.NewRiskAuditApp(stubBackend{}, records, &stubSink{}, nil)

	req := &domain.RiskSubmitRequest{
		UserID:  7,
		ThirdID: "p1",
		BizID:   "b1",
		Content: domain.AuditContent{Text: "视频脚本"},
	}
	id, err := app.AsyncAudit(context.Background(), req, "dujiaapp_mixvideo60s_t2v_output", "http://cb/risk")
	testx.NoError(t, err)
	testx.Equal(t, int64(1), id)

	testx.Len(t, records.records, 1)
	rec := records.records[0]
	testx.Equal(t, int64(7), rec.UserID)
	testx.Equal(t, "p1", rec.ProjectID)
	testx.Equal(t, "risk:async:dujiaapp_mixvideo60s_t2v_output", rec.TaskType)
	testx.Equal(t, domain.StatusAuditing, rec.Status)

	// The callback bonds are backfilled into the request; task_input records the submission inputs
	testx.Equal(t, "http://cb/risk", req.CallbackURL)
	testx.Equal(t, "1", req.Extra["callback_record_id"])
	testx.Contains(t, rec.TaskInput, "dujiaapp_mixvideo60s_t2v_output")
	testx.Contains(t, rec.TaskInput, "http://cb/risk")
}

func TestAsyncAuditSubmitFailureWritesCompensation(t *testing.T) {
	// Empty content → submitAuditRequest validation fails → a compensation record is written, reusing the original recordID
	records := &stubRecords{}
	sink := &stubSink{}
	app := service.NewRiskAuditApp(stubBackend{}, records, sink, nil)

	req := &domain.RiskSubmitRequest{UserID: 7, ThirdID: "p1"}
	id, err := app.AsyncAudit(context.Background(), req, "sp", "")
	testx.Error(t, err)
	testx.Equal(t, int64(0), id)
	testx.Equal(t, 1, sink.calls, "a submission failure should write one compensation record")
	testx.Equal(t, "sp", sink.lastProduct)
	testx.Equal(t, int64(1), sink.lastID, "compensation reuses the original recordID")
	testx.Equal(t, req, sink.lastReq)
	// The original record stays auditing (for retry reuse)
	testx.Len(t, records.records, 1)
	testx.Equal(t, domain.StatusAuditing, records.records[0].Status)
}

func TestAsyncAuditSubmitFailureWithoutSinkSkips(t *testing.T) {
	// With no sink injected (nil), a submission failure does not panic; it only returns the error
	records := &stubRecords{}
	app := service.NewRiskAuditApp(stubBackend{}, records, nil, nil)

	req := &domain.RiskSubmitRequest{UserID: 7, ThirdID: "p1"}
	_, err := app.AsyncAudit(context.Background(), req, "sp", "")
	testx.Error(t, err)
	testx.Equal(t, 1, len(records.records))
}

// --- Async compensation retry ---

func TestRetryAsyncAuditRebuildsRequest(t *testing.T) {
	records := &stubRecords{}
	app := service.NewRiskAuditApp(stubBackend{}, records, nil, nil)

	// Pre-create an auditing record (record_id=42); the retry reuses the original record through submitAuditRequest
	_, err := records.Insert(context.Background(), &domain.AuditRecord{
		UserID:    7,
		ProjectID: "p1",
		TaskType:  "risk:async:sp",
		Status:    domain.StatusAuditing,
	})
	testx.NoError(t, err)

	payload := map[string]any{
		"sub_product":  "sp",
		"record_id":    int64(1),
		"user_id":      int64(7),
		"third_id":     "p1",
		"biz_id":       "b1",
		"callback_url": "http://cb/risk",
		"content_text": "重试的视频内容",
	}
	err = app.RetryAsyncAudit(context.Background(), payload)
	testx.NoError(t, err)

	// task_input records the reconciliation bonds: sub_product + callback_record_id
	rec := records.records[0]
	testx.Equal(t, uint64(1), rec.ID)
	testx.Contains(t, rec.TaskInput, `"sub_product":"sp"`)
	testx.Contains(t, rec.TaskInput, `"callback_record_id":"1"`)
}

func TestRetryAsyncAuditMissingFields(t *testing.T) {
	app := service.NewRiskAuditApp(stubBackend{}, &stubRecords{}, nil, nil)

	err := app.RetryAsyncAudit(context.Background(), map[string]any{})
	testx.Error(t, err)
	testx.Contains(t, err.Error(), "sub_product")

	err = app.RetryAsyncAudit(context.Background(), map[string]any{"sub_product": "sp"})
	testx.Error(t, err)
	testx.Contains(t, err.Error(), "record_id")

	err = app.RetryAsyncAudit(context.Background(), map[string]any{
		"sub_product": "sp", "record_id": int64(1),
	})
	testx.Error(t, err)
	testx.Contains(t, err.Error(), "content")
}

// --- Blacklist checks ---

func TestIsBlacklistedHit(t *testing.T) {
	app := service.NewRiskAuditApp(stubBackend{}, &stubRecords{}, nil, stubBlacklist{hit: true})
	testx.True(t, app.IsBlacklisted(context.Background(), 1, "sp"))
}

func TestIsBlacklistedMiss(t *testing.T) {
	app := service.NewRiskAuditApp(stubBackend{}, &stubRecords{}, nil, stubBlacklist{hit: false})
	testx.False(t, app.IsBlacklisted(context.Background(), 1, "sp"))
}

func TestIsBlacklistedNilCheckerOrEmptySubProduct(t *testing.T) {
	// No blacklist checker injected → pass
	app := service.NewRiskAuditApp(stubBackend{}, &stubRecords{}, nil, nil)
	testx.False(t, app.IsBlacklisted(context.Background(), 1, "sp"))
	// Checker present but subProduct empty → pass
	app = service.NewRiskAuditApp(stubBackend{}, &stubRecords{}, nil, stubBlacklist{hit: true})
	testx.False(t, app.IsBlacklisted(context.Background(), 1, ""))
}
