package service

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// PreCreateApp 是 pre_create 用例的应用服务：承载角色路由与各子 handler。
//
// 无状态：不持有流程上下文，所有状态落库（cg_creation_project / cg_points_flow /
// cg_audit_compensation / cg_risk_audit_record）。跨 context 依赖（送审 / 积分）
// 通过 RiskPort / PointsPort 接口注入，不 import risk/points 基础设施。
type PreCreateApp struct {
	projects domain.ProjectRepository
	risk     RiskPort
	points   PointsPort
}

// NewPreCreateApp 创建 pre_create 应用服务。
func NewPreCreateApp(projects domain.ProjectRepository, risk RiskPort, points PointsPort) *PreCreateApp {
	return &PreCreateApp{
		projects: projects,
		risk:     risk,
		points:   points,
	}
}

// HandlePreCreate 是 pre_create 的入口，按 role_type 分流。
//
// 只有 role_type=4（视觉设计师）和 role_type=5（剪辑大师）走完整路径
// （含风控 + 可退积分）；role_type=3（金牌编剧）和 role_type=2（点子王）走轻量路径
// （不写 project 表、不扣积分）。
func (a *PreCreateApp) HandlePreCreate(ctx context.Context, req *contracts.PreCreateReq) (*contracts.PreCreateResp, error) {
	switch req.RoleType {
	case domain.RoleTypeScreenplay: // 3 - 编剧
		return a.handleScreenplayPreCreate(ctx, req)
	case domain.RoleTypePointOfView: // 2 - 点子王
		return a.handleBrainstormPreCreate(ctx, req)
	case domain.RoleTypeVisualDesigner, domain.RoleTypeEditor: // 4, 5 - 有积分角色
		return a.handleCommonPreCreate(ctx, req)
	default:
		return nil, contracts.ErrUnsupportedRole
	}
}

// costScore 返回该阶段的预估积分消耗。
// 阶段 2 起参与扣分；projectInput 为解析后的用户输入（用于差异化计费）。
func costScore(projectType string, projectInput map[string]any) int {
	return domain.CalculateCost(projectType, projectInput)
}

// inputParams 从 Input 翻译器提取解析后的参数 map（用于计费）。
// CommonInput 暴露 Params；后续自定义 Input 可在接口上补充获取方式。
func inputParams(in contracts.Input) map[string]any {
	if c, ok := in.(*contracts.CommonInput); ok {
		return c.Params
	}
	return nil
}

// syncFeed 同步 feed 表（可选，展示用冗余）。阶段 1 先留空实现，保证骨架可编译。
func syncFeed(ctx context.Context, projectID string, status string) {
	// TODO(阶段 4+): 同步 feed_template_task（与应用层双写，不在同一事务，短暂不一致可接受）。
	_ = ctx
	_ = projectID
	_ = status
}

// runPreCreateAudit 输入风控（阶段 1 骨架留白，本阶段填充）。
//
// 核心（决策 3 + 决策 4）：
//   - 按文本组构建送审请求，每组一个 RiskSubmitRequest
//   - 用 SyncAuditRecorder 送审，把多次同步风控合并成一条 callback_record
//   - 返回语义区分两种失败：
//     err != nil      → 风控请求失败（系统错误）→ 调用方走 ErrTaskFail（退积分）
//     passed == false → 内容不通过（命中风控）→ 调用方走 ErrInputRisk（输入阶段无积分不退）
//
// 送审前先做黑名单检查（命中直接拦截，不再调风控）；textGroups 为空时直接通过（无内容可审）；
// subProduct 从 PreCreationInputMap 配置取。
//
// 注：阶段 5 的风控缓存（已审文本去重）尚未接入——rdb 在旧 Service 中为可选依赖，
// 此处先跳过缓存逻辑，行为等价于"缓存未启用"（照常送审）。
func (a *PreCreateApp) runPreCreateAudit(
	ctx context.Context,
	req *contracts.PreCreateReq,
	project *domain.Project,
	textGroups [][]string,
	subProduct string,
) (passed bool, reason string, err error) {
	if len(textGroups) == 0 {
		return true, "", nil
	}

	// 风控治理（阶段 6）：送审前黑名单检查——命中则直接拦截，不再调风控
	if subProduct != "" && a.risk.IsBlacklisted(ctx, req.UserID, subProduct) {
		return false, "用户已触发风控治理下线", nil
	}

	// project 可能为 nil（首次 pre_create 未建 project），用 session_id 作关联键
	// （契约 §3.1：project_id = session_id，全程复用）。
	projectID := req.SessionID

	// 按组构建审核请求，每组一个 RiskSubmitRequest
	var auditReqs []*riskmodel.RiskSubmitRequest
	for i, texts := range textGroups {
		ar, buildErr := buildSyncTextRequest(projectID, req.UserID, texts, i+1)
		if buildErr != nil {
			return false, "", buildErr
		}
		auditReqs = append(auditReqs, ar)
	}

	// 用 recorder 送审（合并审计记录）：有错优先报错，其次不通过，全过才过
	// task_type 形如 "risk:5:mix_video:precreate"，供审计对账按 (role,type) 检索
	recorder := a.risk.NewSyncAuditRecorder(ctx, req.UserID, projectID, req.TaskStepID,
		"risk:"+domain.TaskTypeKey(req.RoleType, req.ProjectType)+":precreate")
	for _, ar := range auditReqs {
		p, r, e := recorder.Run(ar, subProduct)
		if e != nil {
			recorder.Finish(false, "", e)
			return false, "", e
		}
		if !p {
			recorder.Finish(false, r, nil)
			return false, r, nil
		}
	}
	recorder.Finish(true, "", nil)
	return true, "", nil
}

// buildSyncTextRequest 按一组文本构建同步送审请求。
//
// bizID 用序号区分（"pre_create-1"、"pre_create-2"…），多段文本合并为一条送审，
// 分组粒度由调用方控制，每次送审在 SyncAuditRecorder 里都能对账到是哪一次。
func buildSyncTextRequest(thirdID string, userID int64, texts []string, seq int) (*riskmodel.RiskSubmitRequest, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("empty text group")
	}
	text := ""
	for i, t := range texts {
		if i > 0 {
			text += "\n"
		}
		text += t
	}
	req := riskmodel.NewRiskSubmitRequest(userID, thirdID, fmt.Sprintf("pre_create-%d", seq), text)
	req.Extra = map[string]string{"group_seq": fmt.Sprintf("%d", seq)}
	return req, nil
}
