package dao_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// --- Stub implementations ---

// stubAuditBackend is a fake backend that always passes or returns scripted outcomes (concurrency-safe).
type stubAuditBackend struct {
	mu      sync.Mutex
	idx     int
	results []auditOutcome
}

type auditOutcome struct {
	passed bool
	reason string
	err    error
}

func newStubAuditBackend(results ...auditOutcome) *stubAuditBackend {
	return &stubAuditBackend{results: results}
}

func (b *stubAuditBackend) Submit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.idx >= len(b.results) {
		return true, "", nil
	}
	r := b.results[b.idx]
	b.idx++
	return r.passed, r.reason, r.err
}

// stubAuditRecords is the in-memory AuditRecordRepository.
type stubAuditRecords struct {
	records []*domain.AuditRecord
}

func (s *stubAuditRecords) Insert(ctx context.Context, r *domain.AuditRecord) (uint64, error) {
	r.ID = uint64(len(s.records) + 1)
	s.records = append(s.records, r)
	return r.ID, nil
}
func (s *stubAuditRecords) Get(ctx context.Context, id uint64) (*domain.AuditRecord, error) {
	for _, r := range s.records {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, domain.ErrRecordNotFound
}
func (s *stubAuditRecords) Update(ctx context.Context, id uint64, assigns map[string]any) error {
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
func (s *stubAuditRecords) MarkTerminal(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error) {
	return true, nil
}

// --- Helpers ---

// runReq builds a single-text request with a bizID.
func runReq(bizID string) *domain.RiskSubmitRequest {
	return &domain.RiskSubmitRequest{
		UserID:  1,
		ThirdID: "p1",
		BizID:   bizID,
		Content: domain.AuditContent{Text: "待审内容"},
	}
}

// finishRecord reads the single record written by Finish.
func finishRecord(t *testing.T, records *stubAuditRecords) *domain.AuditRecord {
	t.Helper()
	testx.Len(t, records.records, 1)
	return records.records[0]
}

// parseTaskInput parses the record's task_input.
func parseTaskInput(t *testing.T, rec *domain.AuditRecord) dao.SyncAuditTaskInput {
	t.Helper()
	var in dao.SyncAuditTaskInput
	testx.NoError(t, json.Unmarshal([]byte(rec.TaskInput), &in))
	return in
}

// --- Test cases ---

func TestRecorderRunPassingWritesDoneRecord(t *testing.T) {
	records := &stubAuditRecords{}
	recorder := dao.NewSyncAuditRecorder(
		context.Background(), 7, "p1", 99, "risk:5:mix_video:precreate",
		newStubAuditBackend(), records,
	)

	req := runReq("b1")
	passed, reason, err := recorder.Run(req, "sp_text_input")
	testx.NoError(t, err)
	testx.True(t, passed)
	testx.Equal(t, "", reason)

	recorder.Finish(passed, reason, err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusDone, rec.Status)
	testx.Equal(t, int64(7), rec.UserID)
	testx.Equal(t, "p1", rec.ProjectID)
	testx.Equal(t, int64(99), rec.TaskStepID)
	testx.Equal(t, "risk:5:mix_video:precreate", rec.TaskType)

	in := parseTaskInput(t, rec)
	testx.Equal(t, []string{"b1"}, in.BizIDs)
	testx.Equal(t, []string{"sp_text_input"}, in.SubTypes)
}

func TestRecorderRunBlockingWritesFailRecord(t *testing.T) {
	records := &stubAuditRecords{}
	backend := newStubAuditBackend(auditOutcome{passed: false, reason: "内容包含敏感词[赌博]"})
	recorder := dao.NewSyncAuditRecorder(
		context.Background(), 7, "p1", 99, "risk:5:mix_video:precreate",
		backend, records,
	)

	passed, reason, err := recorder.Run(runReq("b1"), "sp_text_input")
	testx.NoError(t, err)
	testx.False(t, passed)
	testx.Contains(t, reason, "赌博")

	recorder.Finish(passed, reason, err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusFail, rec.Status)
}

func TestRecorderRunBackendErrorWritesFailRecord(t *testing.T) {
	records := &stubAuditRecords{}
	backend := newStubAuditBackend(auditOutcome{passed: false, err: errors.New("backend down")})
	recorder := dao.NewSyncAuditRecorder(
		context.Background(), 7, "p1", 99, "risk:5:mix_video:precreate",
		backend, records,
	)

	_, _, err := recorder.Run(runReq("b1"), "sp_text_input")
	testx.Error(t, err)

	recorder.Finish(false, "", err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusFail, rec.Status)
}

func TestRecorderRunImageUsesVetoDispatch(t *testing.T) {
	// Image requests go through domain.SyncAuditImages: 3 images, the 2nd rejected → one-vote veto
	records := &stubAuditRecords{}
	backend := newStubAuditBackend(
		auditOutcome{passed: true},
		auditOutcome{passed: false, reason: "含违规图"},
		auditOutcome{passed: true},
	)
	recorder := dao.NewSyncAuditRecorder(
		context.Background(), 7, "p1", 99, "risk:5:mix_video:precreate",
		backend, records,
	)

	req := &domain.RiskSubmitRequest{
		UserID: 1, ThirdID: "p1", BizID: "b1",
		Content: domain.AuditContent{Image: []domain.AuditImage{{URL: "a.png"}, {URL: "b.png"}, {URL: "c.png"}}},
	}
	passed, reason, err := recorder.Run(req, "sp_img_input")
	testx.NoError(t, err)
	testx.False(t, passed, "one-vote veto: any rejected image fails the whole round")
	testx.Equal(t, "含违规图", reason)

	recorder.Finish(passed, reason, err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusFail, rec.Status)
}

func TestRecorderRunParallelMixedPriority(t *testing.T) {
	// RunParallel runs multiple texts concurrently, aggregating err > !pass > pass:
	// one rejection + two passes → overall fail
	records := &stubAuditRecords{}
	backend := newStubAuditBackend(
		auditOutcome{passed: true},
		auditOutcome{passed: false, reason: "命中风险"},
		auditOutcome{passed: true},
	)
	recorder := dao.NewSyncAuditRecorder(
		context.Background(), 7, "p1", 99, "risk:5:mix_video:precreate",
		backend, records,
	)

	passed, reason, err := recorder.RunParallel([]*domain.RiskSubmitRequest{
		runReq("b1"), runReq("b2"), runReq("b3"),
	}, "sp_text_input")
	testx.NoError(t, err)
	testx.False(t, passed, "any rejection fails the whole round")
	testx.Equal(t, "命中风险", reason)

	recorder.Finish(passed, reason, err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusFail, rec.Status)
	in := parseTaskInput(t, rec)
	testx.ElementsMatch(t, []string{"b1", "b2", "b3"}, in.BizIDs)
	testx.Equal(t, []string{"sp_text_input"}, in.SubTypes)
}

func TestRecorderRunParallelErrorPriorityOverFail(t *testing.T) {
	// Aggregation err > !pass > pass: both a rejection and an error → the error wins
	records := &stubAuditRecords{}
	backend := newStubAuditBackend(
		auditOutcome{passed: false, reason: "违规"},
		auditOutcome{passed: false, err: errors.New("backend down")},
	)
	recorder := dao.NewSyncAuditRecorder(
		context.Background(), 7, "p1", 99, "risk:5:mix_video:precreate",
		backend, records,
	)

	_, _, err := recorder.RunParallel([]*domain.RiskSubmitRequest{
		runReq("b1"), runReq("b2"),
	}, "sp_text_input")
	testx.Error(t, err, "errors take priority")

	recorder.Finish(false, "", err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusFail, rec.Status)
}

func TestRecorderRunByTickAccumulatesOneRecord(t *testing.T) {
	// Multiple texts with ticker rate-limited dispatch: bizIDs/subProducts of several Runs merge into one record
	records := &stubAuditRecords{}
	recorder := dao.NewSyncAuditRecorder(
		context.Background(), 7, "p1", 99, "risk:5:mix_video:precreate",
		newStubAuditBackend(), records,
	)

	passed, reason, err := recorder.RunByTick([]*domain.RiskSubmitRequest{
		runReq("b1"), runReq("b2"), runReq("b3"),
	}, "sp_tick")
	testx.NoError(t, err)
	testx.True(t, passed)

	recorder.Finish(passed, reason, err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusDone, rec.Status)
	in := parseTaskInput(t, rec)
	testx.ElementsMatch(t, []string{"b1", "b2", "b3"}, in.BizIDs)
	testx.Equal(t, []string{"sp_tick"}, in.SubTypes)
}

func TestRecorderFinishAccumulatesAcrossRuns(t *testing.T) {
	// Multiple Run calls: subTypes/bizIDs accumulate across calls into one record
	records := &stubAuditRecords{}
	recorder := dao.NewSyncAuditRecorder(
		context.Background(), 7, "p1", 99, "risk:5:mix_video:precreate",
		newStubAuditBackend(), records,
	)

	p1, r1, err1 := recorder.Run(runReq("b1"), "sp_input")
	testx.NoError(t, err1)
	p2, r2, err2 := recorder.Run(runReq("b2"), "sp_output")
	testx.NoError(t, err2)
	p3, r3, err3 := recorder.Run(runReq("b3"), "sp_input")
	testx.NoError(t, err3)

	recorder.Finish(p1 && p2 && p3, firstNonEmpty(r1, r2, r3), firstNonNil(err1, err2, err3))

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusDone, rec.Status)
	in := parseTaskInput(t, rec)
	testx.ElementsMatch(t, []string{"b1", "b2", "b3"}, in.BizIDs)
	testx.ElementsMatch(t, []string{"sp_input", "sp_output", "sp_input"}, in.SubTypes)
}

// --- Small helpers ---

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func firstNonNil(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
