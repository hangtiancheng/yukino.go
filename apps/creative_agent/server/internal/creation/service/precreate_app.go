package service

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// PreCreateApp is the application service of the pre_create use case: it carries role routing and the sub-handlers.
//
// Stateless: it holds no flow context; all state is persisted (cg_creation_project / cg_points_flow /
// cg_audit_compensation / cg_risk_audit_record). Cross-context dependencies (audit submission / points)
// are injected via the RiskPort / PointsPort interfaces, without importing risk/points infrastructure.
type PreCreateApp struct {
	projects domain.ProjectRepository
	risk     RiskPort
	points   PointsPort
}

// NewPreCreateApp creates the pre_create application service.
func NewPreCreateApp(projects domain.ProjectRepository, risk RiskPort, points PointsPort) *PreCreateApp {
	return &PreCreateApp{
		projects: projects,
		risk:     risk,
		points:   points,
	}
}

// HandlePreCreate is the pre_create entry, branching by role_type.
//
// Only role_type=4 (visual designer) and role_type=5 (edit master) take the full path
// (with risk control + refundable points); role_type=3 (gold scriptwriter) and role_type=2 (idea king) take the lightweight path
// (no project-table write, no points deduction).
func (a *PreCreateApp) HandlePreCreate(ctx context.Context, req *contracts.PreCreateReq) (*contracts.PreCreateResp, error) {
	switch req.RoleType {
	case domain.RoleTypeScreenplay: // 3 - scriptwriter
		return a.handleScreenplayPreCreate(ctx, req)
	case domain.RoleTypePointOfView: // 2 - idea king
		return a.handleBrainstormPreCreate(ctx, req)
	case domain.RoleTypeVisualDesigner, domain.RoleTypeEditor: // 4, 5 - points-bearing roles
		return a.handleCommonPreCreate(ctx, req)
	default:
		return nil, contracts.ErrUnsupportedRole
	}
}

// costScore returns the estimated points cost of this stage.
// Deductions start from stage 2; projectInput is the parsed user input (for differential billing).
func costScore(projectType string, projectInput map[string]any) int {
	return domain.CalculateCost(projectType, projectInput)
}

// inputParams extracts the parsed parameter map from the Input translator (for billing).
// CommonInput exposes Params; future custom Inputs can extend the interface with accessors.
func inputParams(in contracts.Input) map[string]any {
	if c, ok := in.(*contracts.CommonInput); ok {
		return c.Params
	}
	return nil
}

// syncFeed synchronizes the feed table (optional display redundancy). Stage 1 keeps an empty implementation so the skeleton compiles.
func syncFeed(ctx context.Context, projectID string, status string) {
	// TODO(stage 4+): synchronize feed_template_task (dual write with the application layer, not in one transaction; brief inconsistency is acceptable).
	_ = ctx
	_ = projectID
	_ = status
}

// runPreCreateAudit is input risk control (a stage-1 skeleton blank, filled in this stage).
//
// Core (decisions 3 + 4):
//   - Build audit requests per text group, one RiskSubmitRequest per group
//   - Submit via SyncAuditRecorder, merging multiple sync risk checks into one callback_record
//   - The return semantics distinguish two failures:
//     err != nil      → the risk request failed (system error) → callers take ErrTaskFail (points refund)
//     passed == false → content rejected (risk hit) → callers take ErrInputRisk (no points at the input stage, no refund)
//
// A blacklist check runs before submission (a hit intercepts directly without calling risk control); empty textGroups pass directly (nothing to audit);
// subProduct comes from the PreCreationInputMap config.
//
// Note: the stage-5 risk cache (dedup of audited texts) is not wired yet — rdb was an optional dependency in the old Service,
// so the cache logic is skipped here, equivalent to "cache disabled" (submit as usual).
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

	// Risk governance (stage 6): pre-submission blacklist check — a hit intercepts directly without calling risk control
	if subProduct != "" && a.risk.IsBlacklisted(ctx, req.UserID, subProduct) {
		return false, "用户已触发风控治理下线", nil
	}

	// project may be nil (the first pre_create created no project); session_id is the association key
	// (contract §3.1: project_id = session_id, reused throughout).
	projectID := req.SessionID

	// Build audit requests per group, one RiskSubmitRequest per group
	var auditReqs []*riskmodel.RiskSubmitRequest
	for i, texts := range textGroups {
		ar, buildErr := buildSyncTextRequest(projectID, req.UserID, texts, i+1)
		if buildErr != nil {
			return false, "", buildErr
		}
		auditReqs = append(auditReqs, ar)
	}

	// Submit via the recorder (merged audit records): errors take priority, then rejections; only all-pass passes
	// task_type looks like "risk:5:mix_video:precreate", letting audit reconciliation search by (role,type)
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

// buildSyncTextRequest builds a sync audit request from one text group.
//
// bizID is distinguished by sequence ("pre_create-1", "pre_create-2"…); multiple text segments merge into one submission,
// grouping granularity is controlled by callers, and every submission is reconcilable inside SyncAuditRecorder.
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
