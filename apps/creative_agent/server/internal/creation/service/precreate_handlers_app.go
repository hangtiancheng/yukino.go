package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// handleCommonPreCreate is the full path of the visual designer(4) / edit master(5) (the core).
//
// The core pattern is needReturnMember + centralized defer rollback: any error branch only sets the flag;
// on return the defer uniformly performs "refund points + set fail + sync feed", avoiding writing rollback code at nearly ten error exits.
func (a *PreCreateApp) handleCommonPreCreate(ctx context.Context, req *contracts.PreCreateReq) (*contracts.PreCreateResp, error) {
	// ⚠️ Key pattern: centralized defer rollback
	needReturnMember := false
	failReason := ""
	var project *domain.Project

	defer func() {
		if needReturnMember && project != nil && project.PointsOrderID != "" {
			// Centralized rollback: refund points (idempotent, only used refunds) + set fail + sync feed.
			// MarkFail internally handles the "refund only when used" invariant and ignores refund errors
			// (same semantics as the legacy implementation: a refund failure does not block setting fail; the data-layer idempotency chain covers it).
			changed, _ := project.MarkFail(failReason, func() error {
				return a.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
			})
			if changed {
				_ = a.projects.Update(ctx, project)
			}
			// Sync the feed table (optional display redundancy)
			syncFeed(ctx, project.ProjectID, domain.ProjectStatusFail)
		}
	}()

	// 1. Query the existing project (the premise of idempotency + state checks)
	projects, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		project = nil
	} else {
		project = projects[0]
	}

	// 2. Already done → return new_session directly so the upstream opens a new session
	if project != nil && project.Status == domain.ProjectStatusDone {
		return contracts.NewSessionResp(), nil
	}

	// 2b. If the project is in an abnormal state beyond terminal (fail with points already refunded) — treat it as needing a new session
	if project != nil && project.Status == domain.ProjectStatusFail &&
		project.PointsStatus == domain.PointStatusRefund {
		return contracts.NewSessionResp(), nil
	}

	// 2c. Retry path: retry_type=1 (failure retry) or points not deducted (abnormal state) → deduct once more
	if project != nil &&
		(req.RetryType == domain.RetryTypeFail || project.PointsStatus != domain.PointStatusUsed) {
		return a.handleRetry(ctx, req, project)
	}

	// 2d. A project already in audit/running with points deducted re-entering pre_create — continue assembling inputs directly.

	// 3. Deserialize the user input (the "translator")
	projectInput, err := contracts.ParseInput(req.ProjectInput)
	if err != nil {
		// Parse failure → defer rollback (stage 1 verifies the flag is set first)
		needReturnMember = true
		failReason = "解析用户输入失败: " + err.Error()
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}

	// 4. Extract the texts to audit (stage 3: runPreCreateAudit is wired)
	textGroups := projectInput.AuditTexts()

	// 5. Input risk control (synchronous): route to audit by the risk policy configured per (role, type)
	//    Decision 4 key point: a failed risk request (system error) and rejected content (risk hit) take different branches —
	//    the former ErrTaskFail (points refund), the latter ErrInputRisk (input risk card). The first pre_create
	//    has not deducted points yet (project==nil), so the defer rollback naturally skips; the retry path already deducted and really refunds.
	if len(textGroups) > 0 {
		subProduct := inputAuditSubproduct(req.RoleType, req.ProjectType)
		if subProduct != "" {
			passed, auditReason, auditErr := a.runPreCreateAudit(ctx, req, project, textGroups, subProduct)
			if auditErr != nil || !passed {
				needReturnMember = true
				if auditErr != nil {
					// Risk request failed (system error) → refund points
					failReason = contracts.BuildFailReason("风控请求失败", auditErr.Error())
				} else {
					// Content rejected → input risk card (status=2); no points at the input stage, no refund
					failReason = contracts.BuildFailReason("输入风控拦截", auditReason)
				}
				return contracts.AuditFailResp(domain.CardTypeInputRisk), nil
			}
		}
	}

	// 6. Assemble the downstream operator inputs → return to the agent
	resp, err := a.buildPreCreateSuccess(ctx, req, project, projectInput)
	if err != nil {
		needReturnMember = true
		failReason = err.Error()
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}
	return resp, nil
}

// buildPreCreateSuccess assembles the downstream operator inputs and returns the success response.
//
// Shared by handleCommonPreCreate and handleRetry (after a successful retry the inputs are assembled the same way).
// Assembly failures return an error; callers decide the rollback strategy.
func (a *PreCreateApp) buildPreCreateSuccess(ctx context.Context, req *contracts.PreCreateReq,
	project *domain.Project, projectInput contracts.Input) (*contracts.PreCreateResp, error) {

	assembled, err := projectInput.InputFill(project, req, req.LogID)
	if err != nil {
		return nil, fmt.Errorf("assemble operator inputs: %w", err)
	}

	projectInputJSON, err := json.Marshal(assembled)
	if err != nil {
		return nil, fmt.Errorf("marshal operator inputs: %w", err)
	}

	// Record this stage's task_step into project.ext (the carrier of cross-stage data pass-through)
	// meaningful only when a project exists (lightweight paths put pass-through fields directly in PassThroughData)
	if project != nil {
		ext, _ := domain.ParseExt(project.Ext)
		if ext.TaskStep == nil {
			ext.TaskStep = map[string]domain.TaskStepInfo{}
		}
		ext.TaskStep[domain.TaskTypeKey(req.RoleType, req.ProjectType)] = domain.TaskStepInfo{
			TaskStepID:       req.TaskStepID,
			ParentTaskStepID: req.ParentStepID,
		}
		extJSON, _ := ext.ToJSON()
		if extJSON != project.Ext {
			project.Ext = extJSON
			_ = a.projects.Update(ctx, project)
		}
	}

	return contracts.SuccessResp(domain.CardTypeProjectProcessing,
		costScore(req.ProjectType, inputParams(projectInput)),
		contracts.Base64Encode(projectInputJSON)), nil
}

// handleScreenplayPreCreate is the lightweight path of the scriptwriter(role=3).
//
// It does not query the project table (there is no project on first entry) and deducts no points; an input risk hit writes
// cg_task_callback_record (status=fail) but refunds nothing.
//
// Note: the scriptwriter stage has no project; pass-through fields like task_step_id/parent_task_step_id
// go directly into PassThroughData and are fetched from extend_params on callback.
func (a *PreCreateApp) handleScreenplayPreCreate(ctx context.Context, req *contracts.PreCreateReq) (*contracts.PreCreateResp, error) {
	// 1. Deserialize the user input
	projectInput, err := contracts.ParseInput(req.ProjectInput)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}

	// 2. Extract the text groups to audit (stage 3: runPreCreateAudit is wired)
	textGroups := projectInput.AuditTexts()

	// 3. Input risk control (synchronous) — the lightweight path deducts no points; a hit returns the input risk card directly (status=2)
	//    Decision 4: content rejected → ErrInputRisk (no points, no refund); request failure (system error) → failure card.
	if len(textGroups) > 0 {
		subProduct := inputAuditSubproduct(req.RoleType, req.ProjectType)
		if subProduct != "" {
			passed, _, auditErr := a.runPreCreateAudit(ctx, req, nil, textGroups, subProduct)
			if auditErr != nil {
				return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
			}
			if !passed {
				return contracts.AuditFailResp(domain.CardTypeInputRisk), nil
			}
		}
	}

	// 4. Assemble the operator inputs to return (passing through task_step_id, etc.)
	assembled, err := projectInput.InputFill(nil, req, req.LogID)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}
	projectInputJSON, err := json.Marshal(assembled)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}

	return contracts.SuccessResp(domain.CardTypeProjectProcessing, 0, contracts.Base64Encode(projectInputJSON)), nil
}

// handleBrainstormPreCreate is the lightweight path of the idea king(role=2).
//
// Isomorphic to the scriptwriter path: no project-table write, no points deduction; only input parsing + input assembly.
func (a *PreCreateApp) handleBrainstormPreCreate(ctx context.Context, req *contracts.PreCreateReq) (*contracts.PreCreateResp, error) {
	projectInput, err := contracts.ParseInput(req.ProjectInput)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}

	// Input risk control (wired in stage 3): the lightweight path deducts no points; a hit returns the input risk card (status=2)
	textGroups := projectInput.AuditTexts()
	if len(textGroups) > 0 {
		subProduct := inputAuditSubproduct(req.RoleType, req.ProjectType)
		if subProduct != "" {
			passed, _, auditErr := a.runPreCreateAudit(ctx, req, nil, textGroups, subProduct)
			if auditErr != nil {
				return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
			}
			if !passed {
				return contracts.AuditFailResp(domain.CardTypeInputRisk), nil
			}
		}
	}

	assembled, err := projectInput.InputFill(nil, req, req.LogID)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}
	projectInputJSON, err := json.Marshal(assembled)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}

	return contracts.SuccessResp(domain.CardTypeProjectProcessing, 0, contracts.Base64Encode(projectInputJSON)), nil
}
