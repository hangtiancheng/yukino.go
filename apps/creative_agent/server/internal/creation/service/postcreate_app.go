package service

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// PostCreateApp is the application service of the post_create use case: sync/async pipeline registration + output risk control + settling terminal states.
type PostCreateApp struct {
	projects domain.ProjectRepository
	records  domain.CallbackRecordRepository
	risk     RiskPort
	points   PointsPort
}

// NewPostCreateApp creates the post_create application service.
func NewPostCreateApp(projects domain.ProjectRepository, records domain.CallbackRecordRepository, risk RiskPort, points PointsPort) *PostCreateApp {
	return &PostCreateApp{
		projects: projects,
		records:  records,
		risk:     risk,
		points:   points,
	}
}

// HandleSyncPostCreate is the entry of /inner/creation/sync_post_create (instant image: get the result synchronously, one step to terminal).
//
// Main flow: parse the operator output → failure refunds points + fail → success adds the watermark → image/text risk control → done directly.
// Aligned with §3.4: instant image is a synchronous type; sync_post_create follows create directly, with no callback.
func (a *PostCreateApp) HandleSyncPostCreate(ctx context.Context, req *contracts.SyncPostCreateReq) (*contracts.PostCreateResp, error) {
	// Look up the work (project_id = session_id)
	projects, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, domain.ErrProjectNotFound
	}
	project := projects[0]

	// ⚠️ Terminal short-circuit: done/fail are not processed again (preventing duplicate reports)
	if project.Status == domain.ProjectStatusDone || project.Status == domain.ProjectStatusFail {
		return contracts.PostCreatePushCardResp(domain.CardTypeProjectProcessing), nil
	}

	// Parse the operator output
	skillOutput, err := contracts.ParseSkillTaskOutput(req.TaskOutput)
	if err != nil {
		return a.failPostCreate(ctx, project, "failed to parse operator output: "+err.Error()), nil
	}

	// Failure → refund points + fail + failure card
	if skillOutput.Errno != 0 || req.Status == domain.CallbackTaskStatusFailed {
		_ = a.failProject(ctx, project, contracts.BuildFailReason("任务失败", skillOutput.Data.FailReason))
		return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed), nil
	}

	// Success → add watermark → image risk control (sync) → text risk control (sync) → done
	specialInfo := skillOutput.Data.SpecialInfo

	// 1. Output image risk control (synchronous): instant-image outputs use MiaotuImageOutputSubproduct
	imagePassed, imageReason, imageErr := a.auditOutputImage(ctx, project, specialInfo)
	if imageErr != nil || !imagePassed {
		_ = a.failProject(ctx, project, contracts.BuildFailReason("产出风控拦截", imageReason))
		return contracts.PostCreateFailCardResp(domain.CardTypeOutputAudit), nil
	}

	// 2. Output text risk control (synchronous): image titles/descriptions use MiaotuImageTextInputSubproduct (output side)
	textPassed, textReason, textErr := a.auditOutputText(ctx, project, specialInfo)
	if textErr != nil || !textPassed {
		_ = a.failProject(ctx, project, contracts.BuildFailReason("产出风控拦截", textReason))
		return contracts.PostCreateFailCardResp(domain.CardTypeOutputAudit), nil
	}

	// 3. All passed → done + image delivery card
	if project.MarkDone() {
		_ = a.projects.Update(ctx, project)
	}
	return contracts.PostCreatePushCardResp(domain.CardTypeImageDelivery), nil
}

// HandleAsyncPostCreate is the entry of /inner/creation/async_post_create (mix-cut/instant-video/image-text article: register the pipeline ID asynchronously).
//
// role_type=3/2 goes through callback_record (lightweight path); 4/5 goes through the project table.
// Success → record the pipeline task_id into project / callback_record, return the work-in-production card, and wait for the operator's async callback.
func (a *PostCreateApp) HandleAsyncPostCreate(ctx context.Context, req *contracts.AsyncPostCreateReq) (*contracts.PostCreateResp, error) {
	// Parse the operator output to get the pipeline task_id
	skillOutput, err := contracts.ParseSkillTaskOutput(req.TaskOutput)
	if err != nil {
		return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed), nil
	}
	productionLineID := skillOutput.Data.TaskID

	// role_type=3/2 → lightweight path (callback_record)
	if req.RoleType == domain.RoleTypeScreenplay || req.RoleType == domain.RoleTypePointOfView {
		return a.handleLightweightAsyncPostCreate(ctx, req, skillOutput, productionLineID)
	}

	// role_type=4/5 → look up the work
	projects, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, domain.ErrProjectNotFound
	}
	project := projects[0]

	// Terminal short-circuit
	if project.Status == domain.ProjectStatusDone || project.Status == domain.ProjectStatusFail {
		return contracts.PostCreatePushCardResp(domain.CardTypeProjectProcessing), nil
	}

	// Failure → refund points + fail
	if skillOutput.Errno != 0 || req.Status == domain.CallbackTaskStatusFailed {
		_ = a.failProject(ctx, project, contracts.BuildFailReason("任务失败", skillOutput.Data.FailReason))
		return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed), nil
	}

	// Success → record the pipeline ID into project (after registration, wait for the operator's async callback)
	project.ProductionLineID = fmt.Sprintf("%d", productionLineID)
	_ = a.projects.Update(ctx, project)
	return contracts.PostCreatePushCardResp(domain.CardTypeProjectProcessing), nil
}

// handleLightweightAsyncPostCreate is the async registration of the scriptwriter(3)/idea king(2): write callback_record, no points deduction.
func (a *PostCreateApp) handleLightweightAsyncPostCreate(ctx context.Context, req *contracts.AsyncPostCreateReq,
	skillOutput *contracts.SkillTaskOutput, productionLineID int64) (*contracts.PostCreateResp, error) {

	taskType := domain.TaskTypeKey(req.RoleType, req.ProjectType)

	// Failure → record=fail (the lightweight path refunds nothing)
	if skillOutput.Errno != 0 || req.Status == domain.CallbackTaskStatusFailed {
		record, err := a.records.GetByProjectAndType(ctx, req.SessionID, taskType)
		if err == nil {
			_ = a.records.Update(ctx, record.ID, map[string]any{
				"status": domain.ProjectStatusFail,
			})
		}
		return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed), nil
	}

	// Success → register the pipeline ID into callback_record (task_type = "3:mix_video")
	record, err := a.records.GetByProjectAndType(ctx, req.SessionID, taskType)
	if err != nil {
		// Create one when no record exists (the scriptwriter's first pre_create writes no record; this fills it in)
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

// failPostCreate uniformly handles post_create failures: refund points + set fail + return the failure card.
func (a *PostCreateApp) failPostCreate(ctx context.Context, project *domain.Project, reason string) *contracts.PostCreateResp {
	_ = a.failProject(ctx, project, reason)
	return contracts.PostCreateFailCardResp(domain.CardTypeProjectFailed)
}

// failProject sets fail per the aggregate-root state machine: idempotent points refund (only used refunds) + set fail + persist.
func (a *PostCreateApp) failProject(ctx context.Context, project *domain.Project, reason string) error {
	// Refund points idempotently by points_status (only used refunds); a refund failure does not block setting fail
	// (same semantics as the legacy implementation: the data-layer idempotency chain covers it).
	changed, _ := project.MarkFail(reason, func() error {
		return a.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
	})
	if changed {
		return a.projects.Update(ctx, project)
	}
	return nil
}

// auditOutputImage runs output image risk control (synchronous). specialInfo is the base64-encoded operator result;
// the image URL list is parsed here and each image is submitted for audit.
func (a *PostCreateApp) auditOutputImage(ctx context.Context, project *domain.Project, specialInfo string) (bool, string, error) {
	images := contracts.ParseOutputImages(specialInfo)
	if len(images) == 0 {
		// No images to audit (instant image should produce images; pass as a fallback here to avoid stalling the chain)
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

// auditOutputText runs output text risk control (synchronous). specialInfo may contain title/description text.
func (a *PostCreateApp) auditOutputText(ctx context.Context, project *domain.Project, specialInfo string) (bool, string, error) {
	text := contracts.ExtractOutputText(specialInfo)
	if text == "" {
		return true, "", nil
	}
	req := riskmodel.NewRiskSubmitRequest(project.UserID, project.ProjectID, "output_text", text)
	return a.risk.SyncAudit(ctx, req, riskmodel.MiaotuImageTextInputSubproduct)
}

// RefundAndFailTimeout is the timeout fallback (used by the stage-5 timeout job): refund points + set fail.
//
// Idempotent: only running is processed (terminal short-circuit); the refund goes through MarkFail (only used refunds).
// Returning nil means handled; projects not in running return nil directly without action.
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
