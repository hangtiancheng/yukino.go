package domain

import (
	"context"
	"errors"
)

// ErrAlreadyRefunded indicates the order's points were already refunded (idempotency interception).
var ErrAlreadyRefunded = errors.New("already refunded")

// PointsRepository is the points-ledger repository interface (defined in domain, implemented in infra).
type PointsRepository interface {
	// GetBalance computes the user's current balance.
	GetBalance(ctx context.Context, userID int64) (int, error)
	// InsertConsume writes a deduction entry. The order_id UNIQUE guarantees one order is never deducted twice.
	InsertConsume(ctx context.Context, flow *PointsFlow) (uint64, error)
	// InsertRefund writes a refund entry and marks the matching deduction entry rolled back.
	// Returns ErrAlreadyRefunded when already refunded.
	InsertRefund(ctx context.Context, flow *PointsFlow) (uint64, error)
	// RefundByOrderID refunds points idempotently by order ID: already-refunded returns nil.
	RefundByOrderID(ctx context.Context, orderID string, userID int64, title string) error
	// ListByProject looks up all deduction/refund records of a work (reconciliation).
	ListByProject(ctx context.Context, projectID string) ([]*PointsFlow, error)
}
