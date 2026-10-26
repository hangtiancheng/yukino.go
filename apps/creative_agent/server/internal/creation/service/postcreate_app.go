package service

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// PostCreateApp 是 post_create 用例的应用服务：同步/异步登记产线 + 产出风控 + 落终态。
type PostCreateApp struct {
	projects domain.ProjectRepository
	records  domain.CallbackRecordRepository
	risk     RiskPort
	points   PointsPort
}

// NewPostCreateApp 创建 post_create 应用服务。
func NewPostCreateApp(projects domain.ProjectRepository, records domain.CallbackRecordRepository, risk RiskPort, points PointsPort) *PostCreateApp {
	return &PostCreateApp{
		projects: projects,
		records:  records,
		risk:     risk,
		points:   points,
	}
}

// HandleSyncPostCreate 是 /inner/creation/sync_post_create 的入口（妙图：同步拿结果，一步到终态）。
//
// 主流程：解析算子输出 → 失败退积分 + fail → 成功加水印 → 图片/文本风控 → 直接 done。
// 对齐 §3.4：妙图是同步类型，create 后直接 sync_post_create，不经回调。
func (a *PostCreateApp) HandleSyncPostCreate(ctx context.Context, req *contracts.SyncPostCreateReq) (*contracts.PostCreateResp, error) {
	// 查作品（project_id = session_id）
	projects, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, domain.ErrProjectNotFound
	}
	project := projects[0]

	// ⚠️ 终态短路：done/fail 不重复处理（防重复上报）
	if project.Status == domain.ProjectStatusDone || project.Status == domain.ProjectStatusFail {
		return contracts.PostCreatePushCardResp(domain.CardTypeProjectProcessing), nil
	}

	// 解析算子输出
	skillOutput, err := contracts.ParseSkillTaskOutput(req.TaskOutput)
	if err != nil {
		return a.failPostCreate(ctx, project, "解析算子输出失败: "+err.Error()), nil
	}

	// 失败 → 退积分 + fail + 失败卡
	if skillOutput.Errno != 0 || req.Status == domain.CallbackTaskStatusFailed {
		_ = a.failProject(ctx, project, contracts.BuildFailReason("任务失败", skillOutput.Data.FailReason))
		return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed), nil
	}

	// 成功 → 加水印 → 图片风控(同步) → 文本风控(同步) → done
	specialInfo := skillOutput.Data.SpecialInfo

	// 1. 产出图片风控（同步）：妙图成图输出用 MiaotuImageOutputSubproduct
	imagePassed, imageReason, imageErr := a.auditOutputImage(ctx, project, specialInfo)
	if imageErr != nil || !imagePassed {
		_ = a.failProject(ctx, project, contracts.BuildFailReason("产出风控拦截", imageReason))
		return contracts.PostCreateFailCardResp(domain.CardTypeOutputAudit), nil
	}

	// 2. 产出文本风控（同步）：成图标题/描述用 MiaotuImageTextInputSubproduct（产出侧）
	textPassed, textReason, textErr := a.auditOutputText(ctx, project, specialInfo)
	if textErr != nil || !textPassed {
		_ = a.failProject(ctx, project, contracts.BuildFailReason("产出风控拦截", textReason))
		return contracts.PostCreateFailCardResp(domain.CardTypeOutputAudit), nil
	}

	// 3. 全部通过 → done + 图片交付卡
	if project.MarkDone() {
		_ = a.projects.Update(ctx, project)
	}
	return contracts.PostCreatePushCardResp(domain.CardTypeImageDelivery), nil
}

// HandleAsyncPostCreate 是 /inner/creation/async_post_create 的入口（混剪/妙影/图文笔记：异步登记产线 ID）。
//
// role_type=3/2 走 callback_record（轻量路径），4/5 走 project 表。
// 成功 → 记产线 task_id 到 project / callback_record，返回作品生产中卡，等待算子异步回调。
func (a *PostCreateApp) HandleAsyncPostCreate(ctx context.Context, req *contracts.AsyncPostCreateReq) (*contracts.PostCreateResp, error) {
	// 解析算子输出，拿产线 task_id
	skillOutput, err := contracts.ParseSkillTaskOutput(req.TaskOutput)
	if err != nil {
		return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed), nil
	}
	productionLineID := skillOutput.Data.TaskID

	// role_type=3/2 → 轻量路径（callback_record）
	if req.RoleType == domain.RoleTypeScreenplay || req.RoleType == domain.RoleTypePointOfView {
		return a.handleLightweightAsyncPostCreate(ctx, req, skillOutput, productionLineID)
	}

	// role_type=4/5 → 查作品
	projects, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, domain.ErrProjectNotFound
	}
	project := projects[0]

	// 终态短路
	if project.Status == domain.ProjectStatusDone || project.Status == domain.ProjectStatusFail {
		return contracts.PostCreatePushCardResp(domain.CardTypeProjectProcessing), nil
	}

	// 失败 → 退积分 + fail
	if skillOutput.Errno != 0 || req.Status == domain.CallbackTaskStatusFailed {
		_ = a.failProject(ctx, project, contracts.BuildFailReason("任务失败", skillOutput.Data.FailReason))
		return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed), nil
	}

	// 成功 → 记产线 ID 到 project（登记后等算子异步回调）
	project.ProductionLineID = fmt.Sprintf("%d", productionLineID)
	_ = a.projects.Update(ctx, project)
	return contracts.PostCreatePushCardResp(domain.CardTypeProjectProcessing), nil
}

// handleLightweightAsyncPostCreate 编剧(3)/点子王(2)的异步登记：写 callback_record，不扣积分。
func (a *PostCreateApp) handleLightweightAsyncPostCreate(ctx context.Context, req *contracts.AsyncPostCreateReq,
	skillOutput *contracts.SkillTaskOutput, productionLineID int64) (*contracts.PostCreateResp, error) {

	taskType := domain.TaskTypeKey(req.RoleType, req.ProjectType)

	// 失败 → record=fail（轻量路径不退积分）
	if skillOutput.Errno != 0 || req.Status == domain.CallbackTaskStatusFailed {
		record, err := a.records.GetByProjectAndType(ctx, req.SessionID, taskType)
		if err == nil {
			_ = a.records.Update(ctx, record.ID, map[string]any{
				"status": domain.ProjectStatusFail,
			})
		}
		return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed), nil
	}

	// 成功 → 登记产线 ID 到 callback_record（task_type = "3:mix_video"）
	record, err := a.records.GetByProjectAndType(ctx, req.SessionID, taskType)
	if err != nil {
		// 无既有记录则新建（编剧第一次 pre_create 不落记录，这里补）
		_, err = a.records.Insert(ctx, &domain.CallbackRecord{
			UserID:           req.UserID,
			ProjectID:        req.SessionID,
			TaskType:         taskType,
			Status:           domain.ProjectStatusRunning,
			ProductionLineID: fmt.Sprintf("%d", productionLineID),
		})
		if err != nil {
			return nil, err
		}
		return contracts.PostCreatePushCardResp(domain.CardTypeProjectProcessing), nil
	}

	_ = a.records.Update(ctx, record.ID, map[string]any{
		"production_line_id": fmt.Sprintf("%d", productionLineID),
		"status":             domain.ProjectStatusRunning,
	})
	return contracts.PostCreatePushCardResp(domain.CardTypeProjectProcessing), nil
}

// failPostCreate 统一处理 post_create 失败：退积分 + 置 fail + 返回失败卡。
func (a *PostCreateApp) failPostCreate(ctx context.Context, project *domain.Project, reason string) *contracts.PostCreateResp {
	_ = a.failProject(ctx, project, reason)
	return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed)
}

// failProject 按聚合根状态机置 fail：幂等退积分（仅 used 才退）+ 置 fail + 落库。
func (a *PostCreateApp) failProject(ctx context.Context, project *domain.Project, reason string) error {
	// 按 points_status 幂等退积分（只有 used 才退），退积分失败不阻断置 fail
	// （与旧实现语义一致：数据层幂等链兜底）。
	changed, _ := project.MarkFail(reason, func() error {
		return a.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
	})
	if changed {
		return a.projects.Update(ctx, project)
	}
	return nil
}

// auditOutputImage 产出图片风控（同步）。specialInfo 是 base64 编码的算子结果，
// 这里解析出图片 URL 列表后逐张送审。
func (a *PostCreateApp) auditOutputImage(ctx context.Context, project *domain.Project, specialInfo string) (bool, string, error) {
	images := contracts.ParseOutputImages(specialInfo)
	if len(images) == 0 {
		// 无图可审（妙图应产图，这里兜底通过，避免链路卡死）
		return true, "", nil
	}
	req := &riskmodel.RiskSubmitRequest{
		UserID:  project.UserID,
		ThirdID: project.ProjectID,
		BizID:   "output_image",
		Content: riskmodel.AuditContent{Image: images},
	}
	return a.risk.SyncAudit(ctx, req, riskmodel.MiaotuImageOutputSubproduct)
}

// auditOutputText 产出文本风控（同步）。specialInfo 里可能有标题/描述文本。
func (a *PostCreateApp) auditOutputText(ctx context.Context, project *domain.Project, specialInfo string) (bool, string, error) {
	text := contracts.ExtractOutputText(specialInfo)
	if text == "" {
		return true, "", nil
	}
	req := riskmodel.NewRiskSubmitRequest(project.UserID, project.ProjectID, "output_text", text)
	return a.risk.SyncAudit(ctx, req, riskmodel.MiaotuImageTextInputSubproduct)
}

// RefundAndFailTimeout 超时兜底（阶段 5 超时 job 用）：退积分 + 置 fail。
//
// 幂等：只有 running 才处理（终态短路），退积分走 MarkFail（仅 used 才退）。
// 返回 nil 表示已处理；项目不在 running 时直接返回 nil 不操作。
func (a *PostCreateApp) RefundAndFailTimeout(ctx context.Context, project *domain.Project) error {
	if project == nil || project.Status != domain.ProjectStatusRunning {
		return nil
	}
	changed, err := project.MarkTimeout(contracts.BuildFailReason("超时兜底", "作品生成超时"), func() error {
		return a.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
	})
	if err != nil || !changed {
		return err
	}
	if err := a.projects.Update(ctx, project); err != nil {
		return err
	}
	syncFeed(ctx, project.ProjectID, domain.ProjectStatusFail)
	return nil
}
