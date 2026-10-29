package service

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// handleRetry handles retry deductions (retry_type=1 failure retry, or points_status != used).
//
// Original project semantics: a failure retry = regenerating once = consuming points once more. The core is incrementing order_id to
// retry_num+1 (the reconciliation basis preventing double deductions) and appending the new order ID into project.ext.order_ids.
//
// Flow: query balance → insufficient produces the insufficient-points card → deduct once more → update project (retry_num+1,
// points_order_id, points_status=used, ext.order_ids appended) → return the normal pre_create result.
func (a *PreCreateApp) handleRetry(ctx context.Context, req *contracts.PreCreateReq, project *domain.Project) (*contracts.PreCreateResp, error) {
	memberCost := project.PointsCost
	if memberCost == 0 {
		memberCost = 1
	}

	// 1. Query the balance
	currentScore, err := a.points.GetBalance(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if currentScore < memberCost {
		// Insufficient points → return the insufficient-points card (no deduction)
		return contracts.BuildInsufficientScoreResp(memberCost, currentScore), nil
	}

	// 2. Deduct once more, order_id = projectID_{retryNum+1}
	orderID := buildOrderID(project.ProjectID, project.RetryNum+1)
	_, err = a.points.Deduct(ctx, req.UserID, project.ProjectID, orderID, project.ProjectType, memberCost)
	if err != nil {
		return nil, err
	}

	// 3. Update project: retry_num+1, points_order_id=new orderID, points_status=used
	//    and append the new orderID into project.ext.order_ids (for reconciliation)
	project.RetryNum++
	newExt := domain.AppendOrderID(project.Ext, orderID)
	project.Ext = newExt // sync the in-memory value so the later buildPreCreateSuccess keeps order_ids when writing task_step
	project.PointsOrderID = orderID
	project.PointsStatus = domain.PointStatusUsed
	_ = a.projects.Update(ctx, project)

	// 4. Parse the input and assemble the operator inputs to return (after a successful retry the normal pre_create flow resumes)
	projectInput, err := contracts.ParseInput(req.ProjectInput)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, nil)), nil
	}
	return a.buildPreCreateSuccess(ctx, req, project, projectInput)
}

// buildOrderID generates the points-ledger idempotency key: {projectID}_{retryNum}.
func buildOrderID(projectID string, retryNum int) string {
	return fmt.Sprintf("%s_%d", projectID, retryNum)
}
