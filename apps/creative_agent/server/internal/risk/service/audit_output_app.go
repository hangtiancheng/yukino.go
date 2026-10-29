package service

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// AuditOutputApp is the risk-callback terminal-settling application service.
//
// Aligned with §4.5: risk callbacks no longer go through callback_record — the status=auditing
// record inserted by stage 3's AsyncAudit is settled to terminal here (optimistic lock preventing concurrent duplicate callbacks); after the terminal decision
// It notifies creation to settle the project through AuditResultSink (dependency inversion).
//
// Orchestration order:
//  1. Find the audit record by extra.callback_record_id; record already terminal → short-circuit return (idempotent)
//  2. Set terminal with the optimistic lock (auditing/running → done/fail only, preventing concurrent duplicate callbacks)
//  3. Notify creation to settle the project (passed → done; !passed → refund points + fail)
//
// All dependencies are interface-injected (domain.AuditRecordRepository / AuditResultSink);
// it knows nothing about creation / infrastructure and holds no gorm / redis.Client.
type AuditOutputApp struct {
	records  domain.AuditRecordRepository
	onResult AuditResultSink // implemented and injected by creation; dependency inversion
}

// NewAuditOutputApp creates an AuditOutputApp.
func NewAuditOutputApp(records domain.AuditRecordRepository, onResult AuditResultSink) *AuditOutputApp {
	return &AuditOutputApp{records: records, onResult: onResult}
}

// Handle processes one risk callback (the /inner/callback/risk entry).
//
// Idempotency semantics (preventing a sink-failure dead end): the record terminal short-circuit only blocks callbacks that "already settled successfully"; the terminal write
// happens after the sink succeeds — if the sink fails transiently, the audit record stays auditing, and the risk service's retry
// goes through the sink again instead of being swallowed by a terminal short-circuit. Sink-side idempotency is covered by creation's project
// terminal short-circuit + idempotent points refund (two layers of idempotency).
//
// Concurrency protection: two concurrent callbacks both call the sink (the sink is idempotent); MarkTerminal's CAS guarantees the audit record's
// terminal state is written once (the later writer gets RowsAffected=0, idempotently ignored).
//
// When Extra lacks callback_record_id (recordID=0), audit-record reconciliation is skipped and only the sink notification runs
// (legacy protocol compatibility; the final state is covered by the project terminal short-circuit).
func (a *AuditOutputApp) Handle(ctx context.Context, req *contracts.RiskCallbackReq) error {
	// 1. Find the audit record by extra.callback_record_id; already terminal → idempotent short-circuit (terminal means the sink
	//    already settled successfully; no redo needed — this is what distinguishes it from the sink-failure scenario).
	recordID := mustParseRecordID(req.Extra["callback_record_id"])
	if recordID > 0 {
		rec, err := a.records.Get(ctx, recordID)
		if err != nil {
			return err
		}
		if rec.IsTerminal() {
			return nil // terminal short-circuit: the sink already settled successfully
		}
	}

	// 2. Notify creation to settle the project first (sink). The sink is idempotent (project terminal short-circuit + idempotent refund);
	//    on failure it returns an error, and the risk service's retry comes back here (the audit record is not terminal yet).
	if err := a.onResult.Handle(ctx, req.ThirdID, req.Passed, req.Reason); err != nil {
		return err
	}

	// 3. After the sink succeeds, set the audit record terminal with the optimistic lock (auditing/running → done/fail only, preventing concurrent duplicates).
	//    MarkTerminal's bool (whether the CAS hit) can be ignored: in concurrent duplicate callbacks, the losing side's
	//    terminal state was already written correctly by the winner.
	status := domain.StatusDone
	if !req.Passed {
		status = domain.StatusFail
	}
	if recordID > 0 {
		if _, err := a.records.MarkTerminal(ctx, recordID, status, domain.StatusAuditing, domain.StatusRunning); err != nil {
			return err
		}
	}
	return nil
}
