package service

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// RiskPort is the audit-submission interface (implemented by risk.application's RiskAuditApp; a cross-context port).
//
// The creation application layer does not import risk.infrastructure; it relies on this interface only for:
//   - Input/output risk control (sync SyncAudit, async AsyncAudit)
//   - Pre-submission blacklist checks (IsBlacklisted)
//   - Merged audit records (NewSyncAuditRecorder; the interface is defined in risk.domain, the implementation injected)
//
// Note: the old Service's runPreCreateAudit orchestration (text-group building + recorder merging + blacklist checks)
// stays in creation.application (it belongs to the creation workflow); the underlying submission/recording capabilities come from the risk port.
type RiskPort interface {
	// SyncAudit is synchronous text/image risk control: blocks for the result and returns immediately.
	//   - passed=false + err=non-nil → the risk request failed (system error, points-refund branch)
	//   - passed=false + reason=non-empty → content rejected (risk hit, input/output risk card)
	//   - passed=true → passed
	SyncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error)
	// AsyncAudit is asynchronous video risk control: returns immediately after submission; the result closes the loop via /inner/callback/risk.
	// Returns the audit_record ID (passed through for callback reconciliation).
	AsyncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct, callbackURL string) (int64, error)
	// IsBlacklisted is the pre-submission blacklist check: a hit returns true (intercept).
	IsBlacklisted(ctx context.Context, userID int64, subProduct string) bool
	// NewSyncAuditRecorder creates the merged recorder of one sync risk-control round (multiple checks merge into one audit record).
	NewSyncAuditRecorder(ctx context.Context, userID int64, projectID string, taskStepID int64, taskType string) domain.SyncAuditRecorder
}

// PointsPort is the points interface (implemented by points.application's PointsApp; a cross-context port).
//
// The creation application layer does not import points.infrastructure; deductions/refunds and balance queries go through this interface.
type PointsPort interface {
	// GetBalance queries the user balance.
	GetBalance(ctx context.Context, userID int64) (int, error)
	// Deduct deducts points (idempotent: the order_id UNIQUE prevents double deduction).
	// Callers guarantee orderID uniqueness (e.g. {projectID}_{retryNum}).
	Deduct(ctx context.Context, userID int64, projectID, orderID, projectType string, amount int) (uint64, error)
	// Refund refunds points (idempotent: already-refunded returns nil without error).
	Refund(ctx context.Context, orderID string, userID int64, projectType string) error
}
