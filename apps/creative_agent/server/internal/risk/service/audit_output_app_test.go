package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// --- 审核结果落定专用 stub ---

// errSinkProjectNotFound 模拟 creation 侧 sink 未找到作品时返回的错误
// （risk 域不感知 creation.domain.ErrProjectNotFound，测试里用本地哨兵等价模拟）。
var errSinkProjectNotFound = errors.New("sink: project not found")

// auditOutcomeRecords 内存仓储：记录 Get / MarkTerminal 调用，供行为断言。
type auditOutcomeRecords struct {
	records         map[uint64]*domain.AuditRecord
	markTerminalID  uint64
	markStatus      string
	markCalls       int
	markTerminalHit bool // MarkTerminal 返回的 bool；false 模拟并发 CAS 失败（乐观锁输给对端）
}

func newAuditOutcomeRecords() *auditOutcomeRecords {
	return &auditOutcomeRecords{records: map[uint64]*domain.AuditRecord{}, markTerminalHit: true}
}

func (s *auditOutcomeRecords) Insert(ctx context.Context, r *domain.AuditRecord) (uint64, error) {
	return 0, nil
}
func (s *auditOutcomeRecords) Update(ctx context.Context, id uint64, assigns map[string]any) error {
	return nil
}
func (s *auditOutcomeRecords) Get(ctx context.Context, id uint64) (*domain.AuditRecord, error) {
	rec, ok := s.records[id]
	if !ok {
		return nil, domain.ErrRecordNotFound
	}
	return rec, nil
}
func (s *auditOutcomeRecords) MarkTerminal(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error) {
	s.markTerminalID = id
	s.markStatus = status
	s.markCalls++
	return s.markTerminalHit, nil
}

// auditResultStub 记录 sink 调用的假 AuditResultSink。
type auditResultStub struct {
	projectID string
	passed    bool
	reason    string
	calls     int
	err       error // 非 nil 时 Handle 返回该错误（如未找到作品）
}

func (s *auditResultStub) Handle(ctx context.Context, projectID string, passed bool, reason string) error {
	s.projectID = projectID
	s.passed = passed
	s.reason = reason
	s.calls++
	return s.err
}

// auditResultApp 构造 AuditOutputApp + 可断言的 records/sink。
func auditResultApp(records map[uint64]*domain.AuditRecord, sinkErr error) (*service.AuditOutputApp, *auditOutcomeRecords, *auditResultStub) {
	recordsStub := newAuditOutcomeRecords()
	for id, rec := range records {
		recordsStub.records[id] = rec
	}
	sink := &auditResultStub{err: sinkErr}
	return service.NewAuditOutputApp(recordsStub, sink), recordsStub, sink
}

// --- 通过 / 不通过回调 ---

func TestHandleAuditOutputPassedMarksDoneAndNotifies(t *testing.T) {
	app, records, sink := auditResultApp(map[uint64]*domain.AuditRecord{
		7: {ID: 7, Status: domain.StatusAuditing},
	}, nil)

	req := &contracts.RiskCallbackReq{
		ThirdID: "p1",
		Passed:  true,
		Reason:  "",
		Extra:   map[string]string{"callback_record_id": "7"},
	}
	err := app.Handle(context.Background(), req)
	testx.NoError(t, err)

	// MarkTerminal(done) 被调用（乐观锁，auditing/running → done）
	testx.Equal(t, uint64(7), records.markTerminalID)
	testx.Equal(t, domain.StatusDone, records.markStatus)
	testx.Equal(t, 1, records.markCalls)

	// sink.Handle(projectID, true, reason) 被调用
	testx.Equal(t, 1, sink.calls)
	testx.Equal(t, "p1", sink.projectID)
	testx.True(t, sink.passed)
	testx.Equal(t, "", sink.reason)
}

func TestHandleAuditOutputNotPassedMarksFailAndNotifies(t *testing.T) {
	app, records, sink := auditResultApp(map[uint64]*domain.AuditRecord{
		9: {ID: 9, Status: domain.StatusRunning},
	}, nil)

	req := &contracts.RiskCallbackReq{
		ThirdID: "p2",
		Passed:  false,
		Reason:  "输出包含违规内容",
		Extra:   map[string]string{"callback_record_id": "9"},
	}
	err := app.Handle(context.Background(), req)
	testx.NoError(t, err)

	testx.Equal(t, uint64(9), records.markTerminalID)
	testx.Equal(t, domain.StatusFail, records.markStatus)
	testx.Equal(t, 1, records.markCalls)

	testx.Equal(t, 1, sink.calls)
	testx.Equal(t, "p2", sink.projectID)
	testx.False(t, sink.passed)
	testx.Equal(t, "输出包含违规内容", sink.reason)
}

// --- 终态短路 ---

func TestHandleAuditOutputTerminalShortCircuit(t *testing.T) {
	for _, terminal := range []string{domain.StatusDone, domain.StatusFail} {
		app, records, sink := auditResultApp(map[uint64]*domain.AuditRecord{
			5: {ID: 5, Status: terminal},
		}, nil)

		req := &contracts.RiskCallbackReq{
			ThirdID: "p3",
			Passed:  true, // 即使回调说通过，记录已终态也不重复落定
			Extra:   map[string]string{"callback_record_id": "5"},
		}
		err := app.Handle(context.Background(), req)
		testx.NoError(t, err, "status=%s", terminal)

		testx.Zero(t, records.markCalls, "status=%s 已终态：不应再 MarkTerminal", terminal)
		testx.Zero(t, sink.calls, "status=%s 已终态：不应再通知 sink", terminal)
	}
}

// --- CAS 丢失（并发重复回调，乐观锁输给对端） ---

func TestHandleAuditOutputMarkTerminalCASLostStillNotifies(t *testing.T) {
	app, records, sink := auditResultApp(map[uint64]*domain.AuditRecord{
		13: {ID: 13, Status: domain.StatusAuditing},
	}, nil)
	// MarkTerminal 返回 (false, nil)：并发下对端先落定终态，本回调 CAS 未命中
	records.markTerminalHit = false

	req := &contracts.RiskCallbackReq{
		ThirdID: "p6",
		Passed:  true,
		Reason:  "",
		Extra:   map[string]string{"callback_record_id": "13"},
	}
	err := app.Handle(context.Background(), req)
	testx.NoError(t, err)

	// MarkTerminal 仍被调用（发起了 CAS），但命中结果为 false
	testx.Equal(t, uint64(13), records.markTerminalID)
	testx.Equal(t, domain.StatusDone, records.markStatus)
	testx.Equal(t, 1, records.markCalls)

	// CAS 丢失不阻断通知：sink 仍被调用（终态由对端写对，creation 侧靠
	// project 终态短路 + 幂等退积分兜底）
	testx.Equal(t, 1, sink.calls)
	testx.Equal(t, "p6", sink.projectID)
	testx.True(t, sink.passed)
	testx.Equal(t, "", sink.reason)
}

// --- 缺失作品（sink 返回错误） ---

func TestHandleAuditOutputSinkErrorPropagates(t *testing.T) {
	app, _, sink := auditResultApp(map[uint64]*domain.AuditRecord{
		3: {ID: 3, Status: domain.StatusAuditing},
	}, errSinkProjectNotFound)

	req := &contracts.RiskCallbackReq{
		ThirdID: "missing_project",
		Passed:  true,
		Extra:   map[string]string{"callback_record_id": "3"},
	}
	err := app.Handle(context.Background(), req)

	// sink 的未找到作品错误向上传播（落定失败，调用方可重试/告警）
	testx.ErrorIs(t, err, errSinkProjectNotFound)
	testx.Equal(t, 1, sink.calls)
}

// TestHandleAuditOutputSinkErrorLeavesRecordAuditing 验证 sink 失败死胡同修复：
// sink 瞬时失败时审计记录必须保持 auditing（不置终态），风控服务重试时才能重新走 sink
// 而非被记录终态短路吞掉重试。这是"先 sink 后 CAS 置终态"的核心保证。
func TestHandleAuditOutputSinkErrorLeavesRecordAuditing(t *testing.T) {
	app, records, sink := auditResultApp(map[uint64]*domain.AuditRecord{
		15: {ID: 15, Status: domain.StatusAuditing},
	}, errSinkProjectNotFound)

	req := &contracts.RiskCallbackReq{
		ThirdID: "p15",
		Passed:  true,
		Reason:  "",
		Extra:   map[string]string{"callback_record_id": "15"},
	}
	// 第一次：sink 失败（如 creation DB 瞬时错误）
	err := app.Handle(context.Background(), req)
	testx.ErrorIs(t, err, errSinkProjectNotFound)

	// ⚠️ 关键断言：sink 失败时不得置审计记录终态（MarkTerminal 未调用），
	//    记录保持 auditing 供重试——否则重试会被终态短路吞掉，project 永久卡 audit。
	testx.Zero(t, records.markCalls, "sink 失败时不应置审计记录终态（保持 auditing 供重试）")
	testx.Equal(t, 1, sink.calls)

	// 重试：sink 恢复（nil 错误），应成功落定 + 置审计记录终态
	app2, records2, sink2 := auditResultApp(map[uint64]*domain.AuditRecord{
		15: {ID: 15, Status: domain.StatusAuditing},
	}, nil)
	err = app2.Handle(context.Background(), &contracts.RiskCallbackReq{
		ThirdID: "p15",
		Passed:  true,
		Reason:  "",
		Extra:   map[string]string{"callback_record_id": "15"},
	})
	testx.NoError(t, err)
	testx.Equal(t, uint64(15), records2.markTerminalID, "重试成功后应置审计记录终态")
	testx.Equal(t, domain.StatusDone, records2.markStatus)
	testx.Equal(t, 1, records2.markCalls)
	testx.Equal(t, 1, sink2.calls)
}

// --- 无 callback_record_id（旧协议兼容） ---

func TestHandleAuditOutputNoRecordIDSkipsAndStillNotifies(t *testing.T) {
	app, records, sink := auditResultApp(nil, nil)

	req := &contracts.RiskCallbackReq{
		ThirdID: "p4",
		Passed:  false,
		Reason:  "无记录直接拒绝",
		Extra:   map[string]string{}, // 无 callback_record_id
	}
	err := app.Handle(context.Background(), req)
	testx.NoError(t, err)

	// recordID=0 → 跳过 Get / MarkTerminal
	testx.Zero(t, records.markCalls)
	// 仍通知 creation 落定 project（终态由 project 侧短路兜底）
	testx.Equal(t, 1, sink.calls)
	testx.Equal(t, "p4", sink.projectID)
	testx.False(t, sink.passed)
}

// --- 记录不存在 ---

func TestHandleAuditOutputRecordNotFoundPropagates(t *testing.T) {
	app, _, sink := auditResultApp(nil, nil)

	req := &contracts.RiskCallbackReq{
		ThirdID: "p5",
		Passed:  true,
		Extra:   map[string]string{"callback_record_id": "404"},
	}
	err := app.Handle(context.Background(), req)

	// 找不到审计记录 → 上抛错误（不继续置终态/通知，等重放或告警）
	testx.ErrorIs(t, err, domain.ErrRecordNotFound)
	testx.Zero(t, sink.calls, "记录不存在：不应通知 creation")
}

// --- 用例：回调仅携带 projectID，无 recordID（与 legacy 行为对齐：ThirdID=projectID） ---

func TestHandleAuditOutputThirdIDIsProjectID(t *testing.T) {
	app, records, sink := auditResultApp(map[uint64]*domain.AuditRecord{
		11: {ID: 11, Status: domain.StatusAuditing},
	}, nil)

	req := &contracts.RiskCallbackReq{
		ThirdID: "proj_abc",
		Passed:  true,
		Extra:   map[string]string{"callback_record_id": "11"},
	}
	err := app.Handle(context.Background(), req)
	testx.NoError(t, err)

	testx.Equal(t, uint64(11), records.markTerminalID)
	testx.Equal(t, domain.StatusDone, records.markStatus)
	// sink 收到的是 ThirdID（= project_id），与 legacy HandleAuditOutput 的 req.ThirdID 一致
	testx.Equal(t, "proj_abc", sink.projectID)
}
