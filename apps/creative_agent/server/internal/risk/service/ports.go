package service

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// CompensationSink writes compensation for failed async risk submissions (implemented and injected by the creation domain; dependency inversion).
type CompensationSink interface {
	// InsertAuditCompensation writes one async-risk compensation record (biz_type=1). Idempotent: the same recordID is never written twice.
	InsertAuditCompensation(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string, recordID int64) error
}

// AuditResultSink is the risk-audit result settling notification (implemented and injected by the creation domain; dependency inversion).
//
// After the risk side settles the audit_record terminal state, it notifies creation through this interface to settle the project:
//   - passed=true → project set done (the terminal short-circuit covers duplicate notifications)
//   - passed=false → refund points + set fail (points_status refunds idempotently, only used refunds)
//
// The interface is defined on the risk side; the implementation is provided by creation.infrastructure (OnAuditResultImpl),
// keeping dependencies one-way (creation → risk; risk knows nothing about creation), injected at assembly time.
type AuditResultSink interface {
	Handle(ctx context.Context, projectID string, passed bool, reason string) error
}
