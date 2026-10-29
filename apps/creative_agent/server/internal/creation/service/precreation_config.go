package service

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// PreCreationInputConfig is config-driven: all three dimensions — input struct + risk policy + callback mode — are configuration.
//
// Each (role_type, project_type) combination has one entry in PreCreationInputMap.
// Adding a new creation type only needs one map entry, with no business-code changes.
type PreCreationInputConfig struct {
	// IsMainFlow marks the main flow (deciding the callback mode and whether points are refundable).
	IsMainFlow bool
	// CallbackMode is the callback mode ("only" / "multi" / "both", etc.).
	CallbackMode string
	// ProjectInputSubType is the sub-type passed down to operators.
	ProjectInputSubType string
	// PassThroughCallbackStep is "only" / "multi" single-step/multi-step callback.
	PassThroughCallbackStep string
	// AuditSubproduct is the risk policy this stage uses (filled in stage 3).
	AuditSubproduct string
	// NewInput is the factory method: this stage's input struct.
	NewInput func() contracts.Input
}

// PreCreationInputMap is the (role, type) → input-config registry.
// From stage 3 runPreCreateAudit uses it to route risk policies; this stage only builds the skeleton.
var PreCreationInputMap = map[string]PreCreationInputConfig{
	// Mix-cut (main flow; the final video deducts points)
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
	// Instant image (synchronous; the finished image deducts points)
	domain.TaskTypeKey(domain.RoleTypeVisualDesigner, domain.ProjectTypeMiaotuImage): {
		IsMainFlow: true, CallbackMode: "only",
		PassThroughCallbackStep: domain.CallbackStepOnly,
		AuditSubproduct:         "dujiaapp_miaotu_t2i_input",
		NewInput:                func() contracts.Input { return &contracts.CommonInput{} },
	},
	// Instant video (asynchronous; the final video deducts points)
	domain.TaskTypeKey(domain.RoleTypeEditor, domain.ProjectTypeMiaoyingVideo): {
		IsMainFlow: true, CallbackMode: "both",
		PassThroughCallbackStep: domain.CallbackStepMulti,
		AuditSubproduct:         "dujiaapp_miaoying5s_t2v_input",
		NewInput:                func() contracts.Input { return &contracts.CommonInput{} },
	},
	// Image-text article (no points at the scriptwriter stage / the finished image stage deducts points)
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
	// Brainstorm (idea king; no points deduction)
	domain.TaskTypeKey(domain.RoleTypePointOfView, ""): {
		IsMainFlow: false, AuditSubproduct: "dujiaapp_brainstormplan_t2t_input",
		NewInput: func() contracts.Input { return &contracts.CommonInput{} },
	},
}

// GetConfig looks up the config; returns nil with an ok flag when unconfigured, letting callers distinguish "missing config".
func GetConfig(roleType int, projectType string) (PreCreationInputConfig, bool) {
	cfg, ok := PreCreationInputMap[domain.TaskTypeKey(roleType, projectType)]
	return cfg, ok
}

// inputAuditSubproduct looks up the input risk policy of (role, type); returns an empty string when unconfigured (skip risk control).
func inputAuditSubproduct(roleType int, projectType string) string {
	cfg, ok := GetConfig(roleType, projectType)
	if !ok {
		return ""
	}
	return cfg.AuditSubproduct
}
