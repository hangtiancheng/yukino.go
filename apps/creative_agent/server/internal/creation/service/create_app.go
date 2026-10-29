package service

import (
	"context"
	"encoding/json"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// CreateApp is the application service of the create use case: deduct points + create the work (idempotent).
//
// ⚠️ Core design (difficulty 2: money must never be wrong): deliberately no database transaction.
// Deducting points (writing the local points ledger) and inserting the work are two independent writes, simulating the original project's
// cross-process scenario of "external points RPC + local DB". Application-level compensation replaces the transaction: write ledger → insert work → on failure write a refund ledger entry to "roll back".
type CreateApp struct {
	projects domain.ProjectRepository
	points   PointsPort
}

// NewCreateApp creates the create application service.
func NewCreateApp(projects domain.ProjectRepository, points PointsPort) *CreateApp {
	return &CreateApp{
		projects: projects,
		points:   points,
	}
}

// HandleCreate is the entry of /inner/creation/create: deduct points + create the work (idempotent).
//
// Three layers of idempotency:
//  1. Entry layer: an existing project under the session returns success directly (repeated calls never deduct twice)
//  2. Data layer: the order_id UNIQUE index prevents duplicate deductions for one order
//  3. Compensation layer: work-insert failure → write a points-refund ledger entry (money is never lost)
func (a *CreateApp) HandleCreate(ctx context.Context, req *contracts.CreateReq) (*contracts.CreateResp, error) {
	// 0. Idempotency check: an existing project under the session → return success directly
	existing, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return contracts.CreateSuccessResp(existing[0].PointsCost, 0), nil
	}

	// 1. Compute the cost
	cost := domain.CalculateCost(req.ProjectType, req.ProjectInput)

	// 2. Query the balance (local points ledger: sum(deductions) - sum(refunds))
	balance, err := a.points.GetBalance(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if balance < cost {
		return contracts.CreateInsufficientResp(cost, balance), nil
	}

	// 3. Write the points ledger: deduct (orderID as the idempotency key; the UNIQUE constraint prevents double deduction)
	orderID := buildOrderID(req.SessionID, 1) // {projectID}_{retryNum}; the first deduction uses retryNum=1
	_, err = a.points.Deduct(ctx, req.UserID, req.SessionID, orderID, req.ProjectType, cost)
	if err != nil {
		return nil, err
	}

	// 4. Insert the work (local DB, NOT in the same transaction as step 3!)
	project := &domain.Project{
		UserID:        req.UserID,
		SessionID:     req.SessionID,
		ProjectID:     req.SessionID,
		ProjectType:   req.ProjectType,
		ProjectInput:  marshalProjectInput(req.ProjectInput),
		Title:         req.Title,
		Status:        domain.ProjectStatusRunning,
		PointsStatus:  domain.PointStatusUsed, // deducted
		PointsCost:    cost,
		PointsOrderID: orderID, // {session}_1
		RetryNum:      1,       // the first deduction is consumed (orderID used slot 1); the first retry starts at _2
	}
	err = a.projects.Insert(ctx, project)
	if err != nil {
		// Insert failed → refund points (manual compensation "rollback"); money must never be lost
		_ = a.points.Refund(ctx, orderID, req.UserID, req.ProjectType)
		return nil, err
	}

	// 5. Success: return the post-deduction balance
	return contracts.CreateSuccessResp(cost, balance-cost), nil
}

// marshalProjectInput serializes project_input into a JSON string (stored in cg_creation_project.project_input).
func marshalProjectInput(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	b, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return string(b)
}
