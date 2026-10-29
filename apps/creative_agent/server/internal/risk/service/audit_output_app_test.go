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

// --- Stubs dedicated to audit-result settling ---

// errSinkProjectNotFound simulates the error the creation-side sink returns when the work is not found
// (the risk domain knows nothing about creation.domain.ErrProjectNotFound; tests simulate it with a local sentinel).
var errSinkProjectNotFound = errors.New("sink: project not found")

// auditOutcomeRecords is the in-memory repository: it records Get / MarkTerminal calls for behavior assertions.
type auditOutcomeRecords struct {
	records         map[uint64]*domain.AuditRecord
	markTerminalID  uint64
	markStatus      string
	markCalls       int
	markTerminalHit bool // the bool returned by MarkTerminal; false simulates a concurrent CAS failure (losing the optimistic lock to the peer)
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

// auditResultStub is a fake AuditResultSink recording sink calls.
type auditResultStub struct {
	projectID string
	passed    bool
	reason    string
	calls     int
	err       error // when non-nil, Handle returns this error (e.g. work not found)
}

func (s *auditResultStub) Handle(ctx context.Context, projectID string, passed bool, reason string) error {
	s.projectID = projectID
	s.passed = passed
	s.reason = reason
	s.calls++
	return s.err
}

// auditResultApp builds an AuditOutputApp + assertable records/sink.
func auditResultApp(records map[uint64]*domain.AuditRecord, sinkErr error) (*service.AuditOutputApp, *auditOutcomeRecords, *auditResultStub) {
	recordsStub := newAuditOutcomeRecords()
	for id, rec := range records {
		recordsStub.records[id] = rec
	}
	sink := &auditResultStub{err: sinkErr}
	return service.NewAuditOutputApp(recordsStub, sink), recordsStub, sink
}

// --- Passed / rejected callbacks ---

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

	// MarkTerminal(done) was called (optimistic lock, auditing/running → done)
	testx.Equal(t, uint64(7), records.markTerminalID)
	testx.Equal(t, domain.StatusDone, records.markStatus)
	testx.Equal(t, 1, records.markCalls)

	// sink.Handle(projectID, true, reason) was called
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

// --- Terminal short-circuit ---

func TestHandleAuditOutputTerminalShortCircuit(t *testing.T) {
	for _, terminal := range []string{domain.StatusDone, domain.StatusFail} {
		app, records, sink := auditResultApp(map[uint64]*domain.AuditRecord{
			5: {ID: 5, Status: terminal},
		}, nil)

		req := &contracts.RiskCallbackReq{
			ThirdID: "p3",
			Passed:  true, // even when the callback says passed, an already-terminal record is not settled again
			Extra:   map[string]string{"callback_record_id": "5"},
		}
		err := app.Handle(context.Background(), req)
		testx.NoError(t, err, "status=%s", terminal)

		testx.Zero(t, records.markCalls, "status=%s is terminal: MarkTerminal must not run again", terminal)
		testx.Zero(t, sink.calls, "status=%s is terminal: the sink must not be notified again", terminal)
	}
}

// --- CAS loss (concurrent duplicate callbacks, losing the optimistic lock to the peer) ---

func TestHandleAuditOutputMarkTerminalCASLostStillNotifies(t *testing.T) {
	app, records, sink := auditResultApp(map[uint64]*domain.AuditRecord{
		13: {ID: 13, Status: domain.StatusAuditing},
	}, nil)
	// MarkTerminal returns (false, nil): under concurrency the peer settled terminal first; this callback's CAS missed
	records.markTerminalHit = false

	req := &contracts.RiskCallbackReq{
		ThirdID: "p6",
		Passed:  true,
		Reason:  "",
		Extra:   map[string]string{"callback_record_id": "13"},
	}
	err := app.Handle(context.Background(), req)
	testx.NoError(t, err)

	// MarkTerminal was still called (the CAS was attempted), but the hit result is false
	testx.Equal(t, uint64(13), records.markTerminalID)
	testx.Equal(t, domain.StatusDone, records.markStatus)
	testx.Equal(t, 1, records.markCalls)

	// A CAS loss does not block the notification: the sink is still called (the peer wrote the terminal state correctly; the creation side is covered by
	// the project terminal short-circuit + idempotent points refund)
	testx.Equal(t, 1, sink.calls)
	testx.Equal(t, "p6", sink.projectID)
	testx.True(t, sink.passed)
	testx.Equal(t, "", sink.reason)
}

// --- Missing work (the sink returns an error) ---

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

	// The sink's work-not-found error propagates up (settling failed; callers can retry/alert)
	testx.ErrorIs(t, err, errSinkProjectNotFound)
	testx.Equal(t, 1, sink.calls)
}

// TestHandleAuditOutputSinkErrorLeavesRecordAuditing verifies the sink-failure dead-end fix:
// on transient sink failure the audit record must stay auditing (no terminal state), so the risk service's retry goes through the sink again
// instead of being swallowed by the record's terminal short-circuit. This is the core guarantee of "sink first, then CAS to terminal".
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
	// First time: the sink fails (e.g. a transient creation DB error)
	err := app.Handle(context.Background(), req)
	testx.ErrorIs(t, err, errSinkProjectNotFound)

	// ⚠️ Key assertion: on sink failure the audit record must not be set terminal (MarkTerminal not called);
	//    the record stays auditing for retries — otherwise the retry is swallowed by the terminal short-circuit and the project is stuck in audit forever.
	testx.Zero(t, records.markCalls, "on sink failure the audit record must not be set terminal (stay auditing for retries)")
	testx.Equal(t, 1, sink.calls)

	// Retry: the sink recovers (nil error); settling should succeed + the audit record goes terminal
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
	testx.Equal(t, uint64(15), records2.markTerminalID, "after a successful retry the audit record should be set terminal")
	testx.Equal(t, domain.StatusDone, records2.markStatus)
	testx.Equal(t, 1, records2.markCalls)
	testx.Equal(t, 1, sink2.calls)
}

// --- No callback_record_id (legacy protocol compatibility) ---

func TestHandleAuditOutputNoRecordIDSkipsAndStillNotifies(t *testing.T) {
	app, records, sink := auditResultApp(nil, nil)

	req := &contracts.RiskCallbackReq{
		ThirdID: "p4",
		Passed:  false,
		Reason:  "无记录直接拒绝",
		Extra:   map[string]string{}, // no callback_record_id
	}
	err := app.Handle(context.Background(), req)
	testx.NoError(t, err)

	// recordID=0 → skip Get / MarkTerminal
	testx.Zero(t, records.markCalls)
	// creation is still notified to settle the project (the terminal state is covered by the project-side short-circuit)
	testx.Equal(t, 1, sink.calls)
	testx.Equal(t, "p4", sink.projectID)
	testx.False(t, sink.passed)
}

// --- Record not found ---

func TestHandleAuditOutputRecordNotFoundPropagates(t *testing.T) {
	app, _, sink := auditResultApp(nil, nil)

	req := &contracts.RiskCallbackReq{
		ThirdID: "p5",
		Passed:  true,
		Extra:   map[string]string{"callback_record_id": "404"},
	}
	err := app.Handle(context.Background(), req)

	// Audit record not found → propagate the error (no terminal settling/notification; wait for replay or alerting)
	testx.ErrorIs(t, err, domain.ErrRecordNotFound)
	testx.Zero(t, sink.calls, "record not found: creation must not be notified")
}

// --- Case: the callback carries only projectID, no recordID (aligned with legacy behavior: ThirdID=projectID) ---

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
	// The sink receives ThirdID (= project_id), consistent with legacy HandleAuditOutput's req.ThirdID
	testx.Equal(t, "proj_abc", sink.projectID)
}
