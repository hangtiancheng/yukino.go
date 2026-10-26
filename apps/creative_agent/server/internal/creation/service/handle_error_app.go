package service

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// HandleErrorApp 是 handle_error 用例的应用服务：失败兜底回退。
type HandleErrorApp struct {
	projects domain.ProjectRepository
	points   PointsPort
}

// NewHandleErrorApp 创建 handle_error 应用服务。
func NewHandleErrorApp(projects domain.ProjectRepository, points PointsPort) *HandleErrorApp {
	return &HandleErrorApp{
		projects: projects,
		points:   points,
	}
}

// HandleError 是 /inner/creation/handle_error 的入口：失败兜底回退。
//
// 对齐 §4.7（主文档 §3.7）：
//   - 角色跳过：数据分析师(6)/点子王(2)/金牌编剧(3) 不处理
//   - project 不存在 / 状态不是 running/audit → 返回兜底重试卡
//   - 否则：points_status==used 才退积分 → status=fail → 出失败重试卡（retry_num>=3 出重试限制卡）
func (a *HandleErrorApp) HandleError(ctx context.Context, req *contracts.HandleErrorReq) (*contracts.HandleErrorResp, error) {
	// 兜底响应：始终返回（即使后续分支跳过）
	fallbackResp := contracts.HandleErrorFallbackResp(req)

	// 1. 角色跳过：数据分析师(6)/点子王(2)/金牌编剧(3) 不处理
	if req.RoleType == domain.RoleTypeDataAnalyst || req.RoleType == domain.RoleTypePointOfView ||
		req.RoleType == domain.RoleTypeScreenplay {
		return fallbackResp, nil
	}

	// 2. 查作品，无记录 → 兜底
	projects, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UID)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return fallbackResp, nil
	}
	project := projects[0]

	// 3. 仅处理 running/audit，其它状态 → 兜底
	if project.Status != domain.ProjectStatusRunning && project.Status != domain.ProjectStatusAudit {
		return fallbackResp, nil
	}

	// 4. 防重退款 + 积分退还（used 才退），MarkFail 内部处理"仅 used 才退"不变量
	changed, _ := project.MarkFail(contracts.BuildFailReason("任务失败", "智能体失败回退"), func() error {
		return a.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
	})
	if changed {
		_ = a.projects.Update(ctx, project)
	}

	// 5. 出失败重试卡（retry_num>=3 出重试限制卡）
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
