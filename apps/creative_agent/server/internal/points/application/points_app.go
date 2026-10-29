package application

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/domain"
)

// PointsApp is the points application service: orchestrating deductions/refunds/balance/ledger queries.
type PointsApp struct {
	repo domain.PointsRepository
}

// NewPointsApp creates a PointsApp.
func NewPointsApp(repo domain.PointsRepository) *PointsApp {
	return &PointsApp{repo: repo}
}

// GetBalance queries the user balance.
func (a *PointsApp) GetBalance(ctx context.Context, userID int64) (int, error) {
	return a.repo.GetBalance(ctx, userID)
}

// Deduct deducts points (idempotent: the order_id UNIQUE prevents double deduction).
// Callers guarantee orderID uniqueness (e.g. {projectID}_{retryNum}).
func (a *PointsApp) Deduct(ctx context.Context, userID int64, projectID, orderID, projectType string, amount int) (uint64, error) {
	return a.repo.InsertConsume(ctx, &domain.PointsFlow{
		UserID: userID, ProjectID: projectID, OrderID: orderID,
		Amount: amount, SceneDesc: domain.ConsumeTitle(projectType),
	})
}

// Refund refunds points (idempotent: already-refunded returns nil without error).
func (a *PointsApp) Refund(ctx context.Context, orderID string, userID int64, projectType string) error {
	return a.repo.RefundByOrderID(ctx, orderID, userID, domain.BackTitle(projectType))
}

// ListByProject looks up a work's deduction/refund records (reconciliation).
func (a *PointsApp) ListByProject(ctx context.Context, projectID string) ([]*domain.PointsFlow, error) {
	return a.repo.ListByProject(ctx, projectID)
}
