package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// handleCommonPreCreate 是视觉设计师(4)/剪辑大师(5)的完整路径（最核心）。
//
// 核心模式是 needReturnMember + defer 集中回滚：任何错误分支只需设标志位，
// 函数返回时 defer 统一执行"退积分 + 置 fail + 同步 feed"，避免近十个错误出口各写一遍回滚代码。
func (a *PreCreateApp) handleCommonPreCreate(ctx context.Context, req *contracts.PreCreateReq) (*contracts.PreCreateResp, error) {
	// ⚠️ 关键模式：defer 集中回滚
	needReturnMember := false
	failReason := ""
	var project *domain.Project

	defer func() {
		if needReturnMember && project != nil && project.PointsOrderID != "" {
			// 集中回滚：退积分（幂等，仅 used 才退）+ 置 fail + 同步 feed。
			// MarkFail 内部处理"仅 used 才退积分"不变量，并忽略退积分错误
			// （与旧实现语义一致：退积分失败不阻断置 fail，数据层幂等链兜底）。
			changed, _ := project.MarkFail(failReason, func() error {
				return a.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
			})
			if changed {
				_ = a.projects.Update(ctx, project)
			}
			// 同步 feed 表（可选，展示用冗余）
			syncFeed(ctx, project.ProjectID, domain.ProjectStatusFail)
		}
	}()

	// 1. 查询已有 project（幂等判断 + 状态判断的前提）
	projects, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		project = nil
	} else {
		project = projects[0]
	}

	// 2. 如果已完成 → 直接返回 new_session 让上游开新会话
	if project != nil && project.Status == domain.ProjectStatusDone {
		return contracts.NewSessionResp(), nil
	}

	// 2b. 如果 project 处于终态之外的异常态（fail 且已退积分）—— 视为需新会话
	if project != nil && project.Status == domain.ProjectStatusFail &&
		project.PointsStatus == domain.PointStatusRefund {
		return contracts.NewSessionResp(), nil
	}

	// 2c. 重试路径：retry_type=1（失败重试）或积分未扣（异常态）→ 再扣一次积分
	if project != nil &&
		(req.RetryType == domain.RetryTypeFail || project.PointsStatus != domain.PointStatusUsed) {
		return a.handleRetry(ctx, req, project)
	}

	// 2d. 已处于 audit/running 且积分已扣的 project 重进 pre_create —— 直接继续组装入参。

	// 3. 反序列化用户输入（"翻译器"）
	projectInput, err := contracts.ParseInput(req.ProjectInput)
	if err != nil {
		// 解析失败 → defer 回滚（阶段 1 先验证 flag 置位）
		needReturnMember = true
		failReason = "解析用户输入失败: " + err.Error()
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}

	// 4. 提取待审核文本（阶段 3：runPreCreateAudit 已接通）
	textGroups := projectInput.AuditTexts()

	// 5. 输入风控（同步）：按 (role, type) 配置的风控策略路由送审
	//    决策 4 关键：风控请求失败（系统错误）与内容不通过（命中风控）走不同分支——
	//    前者 ErrTaskFail（退积分），后者 ErrInputRisk（输入风控卡）。首次 pre_create
	//    尚未扣积分（project==nil），defer 回滚自然跳过；重试路径已扣分则真正退积分。
	if len(textGroups) > 0 {
		subProduct := inputAuditSubproduct(req.RoleType, req.ProjectType)
		if subProduct != "" {
			passed, auditReason, auditErr := a.runPreCreateAudit(ctx, req, project, textGroups, subProduct)
			if auditErr != nil || !passed {
				needReturnMember = true
				if auditErr != nil {
					// 风控请求失败（系统错误）→ 退积分
					failReason = contracts.BuildFailReason("风控请求失败", auditErr.Error())
				} else {
					// 内容不通过 → 输入风控卡（status=2），输入阶段无积分不退
					failReason = contracts.BuildFailReason("输入风控拦截", auditReason)
				}
				return contracts.AuditFailResp(domain.CardTypeInputRisk), nil
			}
		}
	}

	// 6. 组装下游算子入参 → 返回给 agent
	resp, err := a.buildPreCreateSuccess(ctx, req, project, projectInput)
	if err != nil {
		needReturnMember = true
		failReason = err.Error()
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}
	return resp, nil
}

// buildPreCreateSuccess 组装下游算子入参并返回成功响应。
//
// 被 handleCommonPreCreate 和 handleRetry 共用（重试成功后同样组装入参返回）。
// 组装失败时返回 error，由调用方决定回滚策略。
func (a *PreCreateApp) buildPreCreateSuccess(ctx context.Context, req *contracts.PreCreateReq,
	project *domain.Project, projectInput contracts.Input) (*contracts.PreCreateResp, error) {

	assembled, err := projectInput.InputFill(project, req, req.LogID)
	if err != nil {
		return nil, fmt.Errorf("组装算子入参失败: %w", err)
	}

	projectInputJSON, err := json.Marshal(assembled)
	if err != nil {
		return nil, fmt.Errorf("序列化算子入参失败: %w", err)
	}

	// 记录本阶段的 task_step 到 project.ext（阶段间数据透传的载体）
	// 仅有 project 时才有意义（轻量路径的透传字段直接放 PassThroughData）
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

// handleScreenplayPreCreate 编剧(role=3)的轻量路径。
//
// 不查 project 表（第一次进来没有 project）、不扣积分；输入风控命中时写
// cg_task_callback_record（status=fail）但不退积分。
//
// 注意：编剧阶段没有 project，task_step_id/parent_task_step_id 这类透传字段
// 直接放 PassThroughData，回调时从 extend_params 里取。
func (a *PreCreateApp) handleScreenplayPreCreate(ctx context.Context, req *contracts.PreCreateReq) (*contracts.PreCreateResp, error) {
	// 1. 反序列化用户输入
	projectInput, err := contracts.ParseInput(req.ProjectInput)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}

	// 2. 提取待审文本组（阶段 3：runPreCreateAudit 已接通）
	textGroups := projectInput.AuditTexts()

	// 3. 输入风控（同步）—— 轻量路径不扣积分，命中直接返回输入风控卡（status=2）
	//    决策 4：内容不通过 → ErrInputRisk（无积分不退）；请求失败（系统错误）→ 失败卡。
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

	// 4. 组装算子入参返回（透传 task_step_id 等）
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

// handleBrainstormPreCreate 点子王(role=2)的轻量路径。
//
// 与编剧路径同构：不写 project 表、不扣积分，只做输入解析 + 入参组装。
func (a *PreCreateApp) handleBrainstormPreCreate(ctx context.Context, req *contracts.PreCreateReq) (*contracts.PreCreateResp, error) {
	projectInput, err := contracts.ParseInput(req.ProjectInput)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, inputParams(projectInput))), nil
	}

	// 输入风控（阶段 3 已接通）：轻量路径不扣积分，命中返回输入风控卡（status=2）
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
