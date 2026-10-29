package dao

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
	riskservice "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
)

// OnAuditResultImpl implements the audit-result notification called by the risk domain (AuditResultSink).
//
// Dependency inversion: the interface is defined in risk.application, implemented and injected by creation.infrastructure,
// keeping dependencies one-way (creation → risk; risk knows nothing about creation).
//
// Aligned with spec §5.2: passed → project set done; !passed → refund points + set fail.
// Idempotent: project terminal short-circuit (MarkDone/MarkFail never change terminal states) + idempotent points refund (only used points are refunded).
type OnAuditResultImpl struct {
	projects domain.ProjectRepository
	points   service.PointsPort
}

// NewOnAuditResultImpl creates the implementation.
func NewOnAuditResultImpl(projects domain.ProjectRepository, points service.PointsPort) *OnAuditResultImpl {
	return &OnAuditResultImpl{projects: projects, points: points}
}

// Compile-time assertion: the implementation satisfies the risk-side AuditResultSink interface.
var _ riskservice.AuditResultSink = (*OnAuditResultImpl)(nil)

// Handle processes one risk-audit result: passed → project done; !passed → refund points + fail.
//
// Idempotent: project terminal short-circuit + idempotent points refund (only used points are refunded).
// ⚠️ CAS settling (preventing the audit→done lost-update race): the final-video callback handler also writes project in a goroutine
// (setting audit + ext.async_audit_record_id). With an unconditional full-field Update here, the handler's stale snapshot
// could overwrite an already-settled done back to audit. So UpdateIfStatus(project, audit) provides a DB-level CAS —
// the terminal state is written only while the DB status is still audit; a CAS failure means the state already advanced (e.g. a duplicate callback settled it), an idempotent short-circuit.
func (h *OnAuditResultImpl) Handle(ctx context.Context, projectID string, passed bool, reason string) error {
	project, err := h.projects.GetByProjectID(ctx, projectID)
	if err != nil {
		return err
	}
	if project.IsTerminal() {
		return nil // terminal short-circuit
	}
	var ok bool
	if passed {
		if !project.MarkDone() {
			return nil
		}
		// CAS expects audit or running: on the normal chain the handler already set audit; with the legacy protocol (no recordID
		// reconciliation) the project may still be running. Both are allowed to settle to done, eliminating the overwrite race.
		ok, err = h.projects.UpdateIfStatus(ctx, project, domain.ProjectStatusAudit, domain.ProjectStatusRunning)
	} else {
		var changed bool
		changed, err = project.MarkFail(contracts.BuildFailReason("输出风控拦截", reason), func() error {
			return h.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
		})
		if err != nil || !changed {
			return err
		}
		ok, err = h.projects.UpdateIfStatus(ctx, project, domain.ProjectStatusAudit, domain.ProjectStatusRunning)
	}
	if err != nil {
		return err
	}
	if !ok {
		// CAS failed: the state was advanced concurrently. If this done/fail settling was preempted by the handler, just give up;
		// if the handler's running→audit has not been persisted yet (handler concurrent with the risk callback), the audit-written
		// snapshot is not overwritten — leave it to the next callback or the timeout fallback. Return nil idempotently.
		return nil
	}
	return nil
}
