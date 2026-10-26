package service

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// PreCreationInputConfig 配置驱动：把「输入结构体 + 风控策略 + 回调方式」三个维度全部配置化。
//
// 每个 (role_type, project_type) 组合在 PreCreationInputMap 里配一条。
// 加一个新创作类型，只需在 map 里配一条，不用改业务代码。
type PreCreationInputConfig struct {
	// IsMainFlow 是否主流程（决定回调方式与是否可退积分）。
	IsMainFlow bool
	// CallbackMode 回调方式（"only" / "multi" / "both" 等）。
	CallbackMode string
	// ProjectInputSubType 下发算子的子类型。
	ProjectInputSubType string
	// PassThroughCallbackStep "only" / "multi" 单步/多步回调。
	PassThroughCallbackStep string
	// AuditSubproduct 这个阶段用什么风控策略（阶段 3 填）。
	AuditSubproduct string
	// NewInput 工厂方法：这个阶段的输入结构体。
	NewInput func() contracts.Input
}

// PreCreationInputMap 是 (role, type) → 输入配置的注册表。
// 阶段 3 起在 runPreCreateAudit 中用它路由风控策略，本阶段先搭骨架。
var PreCreationInputMap = map[string]PreCreationInputConfig{
	// 混剪（主流程，成片扣积分）
	domain.TaskTypeKey(domain.RoleTypeScreenplay, domain.ProjectTypeMixVideo): {
		IsMainFlow: false, AuditSubproduct: "dujiaapp_mixvideo_t2t_input",
		NewInput: func() contracts.Input { return &contracts.CommonInput{} },
	},
	domain.TaskTypeKey(domain.RoleTypeEditor, domain.ProjectTypeMixVideo): {
		IsMainFlow: true, CallbackMode: "both",
		PassThroughCallbackStep: domain.CallbackStepMulti,
		AuditSubproduct:         "dujiaapp_mixvideoscript_t2t_input",
		NewInput:                func() contracts.Input { return &contracts.CommonInput{} },
	},
	// 妙图（同步，成图扣积分）
	domain.TaskTypeKey(domain.RoleTypeVisualDesigner, domain.ProjectTypeMiaotuImage): {
		IsMainFlow: true, CallbackMode: "only",
		PassThroughCallbackStep: domain.CallbackStepOnly,
		AuditSubproduct:         "dujiaapp_miaotu_t2i_input",
		NewInput:                func() contracts.Input { return &contracts.CommonInput{} },
	},
	// 妙影（异步，成片扣积分）
	domain.TaskTypeKey(domain.RoleTypeEditor, domain.ProjectTypeMiaoyingVideo): {
		IsMainFlow: true, CallbackMode: "both",
		PassThroughCallbackStep: domain.CallbackStepMulti,
		AuditSubproduct:         "dujiaapp_miaoying5s_t2v_input",
		NewInput:                func() contracts.Input { return &contracts.CommonInput{} },
	},
	// 图文笔记（编剧阶段无积分 / 成图阶段扣积分）
	domain.TaskTypeKey(domain.RoleTypeScreenplay, domain.ProjectTypeGraphicNotes): {
		IsMainFlow: false, AuditSubproduct: "dujiaapp_graphicnotes_t2i_input",
		NewInput: func() contracts.Input { return &contracts.CommonInput{} },
	},
	domain.TaskTypeKey(domain.RoleTypeVisualDesigner, domain.ProjectTypeGraphicNotes): {
		IsMainFlow: true, CallbackMode: "both",
		PassThroughCallbackStep: domain.CallbackStepMulti,
		AuditSubproduct:         "dujiaapp_graphicnotesscript_t2i_input",
		NewInput:                func() contracts.Input { return &contracts.CommonInput{} },
	},
	// 脑洞（点子王，不扣积分）
	domain.TaskTypeKey(domain.RoleTypePointOfView, ""): {
		IsMainFlow: false, AuditSubproduct: "dujiaapp_brainstormplan_t2t_input",
		NewInput: func() contracts.Input { return &contracts.CommonInput{} },
	},
}

// GetConfig 查配置；未配置时返回 nil 并带 ok 标记，便于调用方区分"配置缺失"。
func GetConfig(roleType int, projectType string) (PreCreationInputConfig, bool) {
	cfg, ok := PreCreationInputMap[domain.TaskTypeKey(roleType, projectType)]
	return cfg, ok
}

// inputAuditSubproduct 查 (role, type) 对应的输入风控策略；未配置时返回空串（跳过风控）。
func inputAuditSubproduct(roleType int, projectType string) string {
	cfg, ok := GetConfig(roleType, projectType)
	if !ok {
		return ""
	}
	return cfg.AuditSubproduct
}
