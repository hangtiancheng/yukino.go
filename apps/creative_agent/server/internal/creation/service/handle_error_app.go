package service

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// HandleErrorApp is the application service of the handle_error use case: failure fallback rollback.
type HandleErrorApp struct {
	projects domain.ProjectRepository
	points   PointsPort
}

// NewHandleErrorApp creates the handle_error application service.
func NewHandleErrorApp(projects domain.ProjectRepository, points PointsPort) *HandleErrorApp {
	return &HandleErrorApp{
		projects: projects,
		points:   points,
	}
}

// HandleError is the entry of /inner/creation/handle_error: failure fallback rollback.
//
// Aligned with §4.7 (main doc §3.7):
//   - Role skip: data analyst(6) / idea king(2) / gold scriptwriter(3) are not processed
//   - project missing / status not running/audit → return the fallback retry card
//   - Otherwise: refund points only when points_status==used → status=fail → produce the failure retry card (retry_num>=3 produces the retry-limit card)
func (a *HandleErrorApp) HandleError(ctx context.Context, req *contracts.HandleErrorReq) (*contracts.HandleErrorResp, error) {
	// Fallback response: always returned (even when later branches skip)
	fallbackResp := contracts.HandleErrorFallbackResp(req)

	// 1. Role skip: data analyst(6) / idea king(2) / gold scriptwriter(3) are not processed
	if req.RoleType == domain.RoleTypeDataAnalyst || req.RoleType == domain.RoleTypePointOfView ||
		req.RoleType == domain.RoleTypeScreenplay {
		return fallbackResp, nil
	}

	// 2. Look up the work; no record → fallback
	projects, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UID)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return fallbackResp, nil
	}
	project := projects[0]

	// 3. Process only running/audit; other states → fallback
	if project.Status != domain.ProjectStatusRunning && project.Status != domain.ProjectStatusAudit {
		return fallbackResp, nil
	}

	// 4. Duplicate-refund guard + points refund (only used refunds); MarkFail internally handles the "refund only when used" invariant
	changed, _ := project.MarkFail(contracts.BuildFailReason("任务失败", "智能体失败回退"), func() error {
		return a.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
	})
	if changed {
		_ = a.projects.Update(ctx, project)
	}

	// 5. Produce the failure retry card (retry_num>=3 produces the retry-limit card)
	cardType := domain.CardTypeProjectFailed
	if project.RetryNum >= 3 {
		cardType = domain.CardTypeRetryLimit
	}
	return &contracts.HandleErrorResp{
		Errno: 0,
		Data: &contracts.HandleErrorData{
			SessionID: req.SessionID,
			UserID:    req.UID,
			ProjectID: project.ProjectID,
			Status:    domain.ProjectStatusFail,
			TaskType:  domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type":  cardType,
				"cost_score": project.PointsCost,
			},
		},
	}, nil
}
