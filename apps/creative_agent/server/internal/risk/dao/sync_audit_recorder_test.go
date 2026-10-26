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

// --- stub 实现 ---

// stubAuditBackend 固定返回通过，或按脚本返回预置结果的假 backend（并发安全）。
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

// stubAuditRecords 内存版 AuditRecordRepository。
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

// --- 工具 ---

// runReq 构造一个带 bizID 的单条文本请求。
func runReq(bizID string) *domain.RiskSubmitRequest {
	return &domain.RiskSubmitRequest{
		UserID:  1,
		ThirdID: "p1",
		BizID:   bizID,
		Content: domain.AuditContent{Text: "待审内容"},
	}
}

// finishRecord 读取 Finish 落下的唯一一条记录。
func finishRecord(t *testing.T, records *stubAuditRecords) *domain.AuditRecord {
	t.Helper()
	testx.Len(t, records.records, 1)
	return records.records[0]
}

// parseTaskInput 解析记录里的 task_input。
func parseTaskInput(t *testing.T, rec *domain.AuditRecord) dao.SyncAuditTaskInput {
	t.Helper()
	var in dao.SyncAuditTaskInput
	testx.NoError(t, json.Unmarshal([]byte(rec.TaskInput), &in))
	return in
}

// --- 测试用例 ---

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
	// 图片请求走 domain.SyncAuditImages：3 张图，第 2 张不通过 → 一票否决
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
	testx.False(t, passed, "一票否决：任一图片不通过则整体失败")
	testx.Equal(t, "含违规图", reason)

	recorder.Finish(passed, reason, err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusFail, rec.Status)
}

func TestRecorderRunParallelMixedPriority(t *testing.T) {
	// RunParallel 多条并发，聚合 err > !pass > pass：
	// 一条不通过 + 两条通过 → 整体 fail
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
	testx.False(t, passed, "任一不通过则整体失败")
	testx.Equal(t, "命中风险", reason)

	recorder.Finish(passed, reason, err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusFail, rec.Status)
	in := parseTaskInput(t, rec)
	testx.ElementsMatch(t, []string{"b1", "b2", "b3"}, in.BizIDs)
	testx.Equal(t, []string{"sp_text_input"}, in.SubTypes)
}

func TestRecorderRunParallelErrorPriorityOverFail(t *testing.T) {
	// 聚合 err > !pass > pass：既有不通过又有报错 → 优先报错
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
	testx.Error(t, err, "有错优先报错")

	recorder.Finish(false, "", err)

	rec := finishRecord(t, records)
	testx.Equal(t, domain.StatusFail, rec.Status)
}

func TestRecorderRunByTickAccumulatesOneRecord(t *testing.T) {
	// 多条文本 ticker 限速派发：多次 Run 的 bizID/subProduct 合并进同一条记录
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
	// 多次 Run 调用：subTypes/bizIDs 跨调用累加进同一条记录
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

// --- 小工具 ---

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
